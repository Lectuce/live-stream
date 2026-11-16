package service

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"live-stream/internal/dao"
	"live-stream/internal/model"
)

type discoveryRedis struct{ values map[string]string }

func (h discoveryRedis) DialHook(next redis.DialHook) redis.DialHook {
	return func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("unexpected dial") }
}
func (h discoveryRedis) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, c redis.Cmder) error {
		if c.Name() == "get" {
			cmd := c.(*redis.StringCmd)
			v, ok := h.values[c.Args()[1].(string)]
			if !ok {
				cmd.SetErr(redis.Nil)
				return redis.Nil
			}
			cmd.SetVal(v)
			return nil
		}
		if c.Name() == "set" {
			c.(*redis.StatusCmd).SetVal("OK")
			return nil
		}
		return errors.New("unexpected Redis command")
	}
}
func (h discoveryRedis) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func discoveryDB(t *testing.T, values map[string]string) sqlmock.Sqlmock {
	t.Helper()
	db, m, e := sqlmock.New()
	if e != nil {
		t.Fatal(e)
	}
	g, e := gorm.Open(mysql.New(mysql.Config{Conn: db, SkipInitializeWithVersion: true}), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	old := dao.SC
	dao.SC = &model.StorageContainer{Db: g}
	if values != nil {
		client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
		client.AddHook(discoveryRedis{values})
		dao.SC.Redis = client
		t.Cleanup(func() { client.Close() })
	}
	t.Cleanup(func() {
		dao.SC = old
		if e := m.ExpectationsWereMet(); e != nil {
			t.Error(e)
		}
		db.Close()
	})
	return m
}

func TestDiscoverySnapshotFiltersAndAdvancesWithoutSkipping(t *testing.T) {
	id := "bf08be4c-bcd7-47de-9519-2fc448578143"
	ids := make([]uint64, 65)
	for i := range ids {
		ids[i] = uint64(i + 1)
	}
	b, _ := json.Marshal(dao.VideoSnapshot{Mode: "hot", IDs: ids})
	m := discoveryDB(t, map[string]string{"feed:video:current:hot:": id, "feed:video:snapshot:" + id: string(b)})
	cursor := ""
	seen := map[uint64]bool{}
	offset := 0
	total := 0
	for pageIndex := 0; pageIndex < 4; pageIndex++ {
		rows := sqlmock.NewRows([]string{"video_id", "owner_id", "title", "category", "published_at"})
		for n := offset + 1; n <= 65; n++ {
			if n >= 21 && n <= 24 {
				continue
			}
			rows.AddRow(n, 1, "public", "general", time.Now())
		}
		m.ExpectQuery("SELECT .* FROM videos v JOIN users u .*v.deleted_at IS NULL.*v.publish_status='published'.*v.audit_status='approved'.*v.video_id IN").WillReturnRows(rows)
		p, e := GetVideoDiscovery(context.Background(), "hot", "", cursor, 20)
		if e != nil {
			t.Fatal(e)
		}
		for _, v := range p.Items {
			if seen[v.VideoID] || (v.VideoID >= 21 && v.VideoID <= 24) {
				t.Fatal("duplicate or hidden", v.VideoID)
			}
			seen[v.VideoID] = true
			total++
		}
		if pageIndex == 1 && (len(p.Items) != 20 || p.Items[0].VideoID != 25 || p.Items[19].VideoID != 44) {
			t.Fatal(p.Items)
		}
		if !p.HasMore {
			if p.NextCursor != "" || total != 61 {
				t.Fatal(p, total)
			}
			break
		}
		cursor = p.NextCursor
		c, e := decodeVideoCursor(cursor, "hot", "")
		if e != nil {
			t.Fatal(e)
		}
		offset = c.Offset
	}
	if total != 61 {
		t.Fatal(total)
	}
}
func TestDiscoveryRedisFailureFallsBackOnlyOnFirstPage(t *testing.T) {
	m := discoveryDB(t, nil)
	rows := sqlmock.NewRows([]string{"video_id", "published_at"}).AddRow(7, time.Now()).AddRow(6, time.Now().Add(-time.Second))
	m.ExpectQuery("SELECT .*ORDER BY v.published_at DESC,v.video_id DESC LIMIT").WithArgs(2).WillReturnRows(rows)
	p, e := GetVideoDiscovery(context.Background(), "recommended", "", "", 1)
	if e != nil || !p.Degraded || p.Mode != "latest" || !p.HasMore {
		t.Fatal(p, e)
	}
	if _, e := decodeVideoCursor(p.NextCursor, "latest", ""); e != nil {
		t.Fatal(e)
	}
	old := encodeVideoCursor(videoFeedCursor{V: 1, Mode: "hot", Snapshot: "bf08be4c-bcd7-47de-9519-2fc448578143", Offset: 20})
	if _, e := GetVideoDiscovery(context.Background(), "hot", "", old, 20); !errors.Is(e, dao.ErrVideoDiscoveryUnavailable) {
		t.Fatal(e)
	}
}
func TestDiscoveryCursorAndExpiredSnapshot(t *testing.T) {
	discoveryDB(t, map[string]string{})
	c := videoFeedCursor{V: 1, Mode: "hot", Category: "games", Snapshot: "bf08be4c-bcd7-47de-9519-2fc448578143", Offset: 20}
	raw := encodeVideoCursor(c)
	if _, e := decodeVideoCursor(raw, "recommended", "games"); e == nil {
		t.Fatal("mode mismatch accepted")
	}
	if _, e := decodeVideoCursor(raw, "hot", "music"); e == nil {
		t.Fatal("category mismatch accepted")
	}
	if _, e := decodeVideoCursor(strings.Repeat("x", 1025), "hot", "games"); e == nil {
		t.Fatal("oversized cursor")
	}
	if _, e := GetVideoDiscovery(context.Background(), "hot", "games", raw, 20); !errors.Is(e, dao.ErrVideoSnapshotExpired) {
		t.Fatal(e)
	}
}
func TestDiscoveryRankingColdStartDecayAndDiversity(t *testing.T) {
	now := time.Now()
	items := []model.VideoCard{}
	signals := map[uint64]model.VideoSignals{}
	for n := 1; n <= 40; n++ {
		v := model.VideoCard{VideoID: uint64(n), OwnerID: uint64(n % 10), Category: []string{"games", "music", "life", "knowledge"}[n%4], PublishedAt: now.Add(-time.Duration(n) * time.Hour)}
		items = append(items, v)
		signals[v.VideoID] = model.VideoSignals{Plays: float64(100 - n)}
	}
	ids := rankVideoCandidates(items, signals, "recommended", now)
	if len(ids) != 40 || ids[0] != 1 {
		t.Fatal(ids)
	}
	seen := map[uint64]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Fatal("duplicate")
		}
		seen[id] = true
	}
	if ids := rankVideoCandidates(items, nil, "recommended", now); len(ids) != 40 {
		t.Fatal("cold start empty")
	}
	a := model.VideoCard{VideoID: 1, PublishedAt: now}
	b := model.VideoCard{VideoID: 2, PublishedAt: now.Add(-30 * 24 * time.Hour)}
	ids = rankVideoCandidates([]model.VideoCard{b, a}, nil, "hot", now)
	if ids[0] != 1 {
		t.Fatal("age decay not applied")
	}
}
