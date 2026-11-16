package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"live-stream/internal/dao"
	"live-stream/internal/model"
	"live-stream/internal/repo"
	"math"
	"os"
	"sort"
	"strconv"
	"time"
	"unicode/utf8"
)

type videoFeedCursor struct {
	V                        int
	Mode, Category, Snapshot string
	Offset                   int
	Time                     time.Time
	ID                       uint64
}

func encodeVideoCursor(c videoFeedCursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}
func decodeVideoCursor(raw, mode, category string) (videoFeedCursor, error) {
	c := videoFeedCursor{V: 1, Mode: mode, Category: category}
	if raw == "" {
		return c, nil
	}
	if len(raw) > 1024 {
		return c, ErrInvalidFeedCursor
	}
	b, e := base64.RawURLEncoding.DecodeString(raw)
	if e != nil || json.Unmarshal(b, &c) != nil || c.V != 1 || c.Mode != mode || c.Category != category {
		return c, ErrInvalidFeedCursor
	}
	if mode == "latest" {
		if c.ID == 0 || c.Time.IsZero() || c.Snapshot != "" {
			return c, ErrInvalidFeedCursor
		}
	} else {
		if _, err := uuid.Parse(c.Snapshot); err != nil || c.Offset < 0 || c.Offset > 500 {
			return c, ErrInvalidFeedCursor
		}
	}
	return c, nil
}
func videoWeight(name string, fallback float64) float64 {
	n, e := strconv.ParseFloat(os.Getenv("VIDEO_RANK_"+name), 64)
	if e != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 100 {
		return fallback
	}
	return n
}
func rankVideoCandidates(items []model.VideoCard, signals map[uint64]model.VideoSignals, mode string, now time.Time) []uint64 {
	ranked := append([]model.VideoCard(nil), items...)
	play, click, like, fav, comment := videoWeight("PLAY", 3), videoWeight("CLICK", 0.25), videoWeight("LIKE", 2), videoWeight("FAVORITE", 4), videoWeight("COMMENT", 2)
	score := func(v model.VideoCard) float64 {
		s := signals[v.VideoID]
		age := math.Max(0, now.Sub(v.PublishedAt).Hours())
		return (1 + play*math.Log1p(s.Plays) + click*math.Log1p(s.Clicks) + like*math.Log1p(s.Likes) + fav*math.Log1p(s.Favorites) + comment*math.Log1p(s.Comments)) / math.Pow(1+age/24, 0.6)
	}
	sort.Slice(ranked, func(i, j int) bool {
		a, b := score(ranked[i]), score(ranked[j])
		if a == b {
			return ranked[i].VideoID > ranked[j].VideoID
		}
		return a > b
	})
	if mode == "hot" {
		ids := make([]uint64, 0, len(ranked))
		for _, v := range ranked {
			ids = append(ids, v.VideoID)
		}
		return ids
	}
	fresh := append([]model.VideoCard(nil), items...)
	sort.Slice(fresh, func(i, j int) bool {
		if fresh[i].PublishedAt.Equal(fresh[j].PublishedAt) {
			return fresh[i].VideoID > fresh[j].VideoID
		}
		return fresh[i].PublishedAt.After(fresh[j].PublishedAt)
	})
	used := map[uint64]bool{}
	ordered := make([]model.VideoCard, 0, len(items))
	ids := make([]uint64, 0, len(items))
	for len(ordered) < len(items) {
		pool := ranked
		if len(ordered)%5 == 0 {
			pool = fresh
		}
		choose := -1
		for pass := 0; pass < 3 && choose < 0; pass++ {
			for i, v := range pool {
				if used[v.VideoID] {
					continue
				}
				author, category := 0, 0
				for _, last := range ordered[max(0, len(ordered)-9):] {
					if last.OwnerID == v.OwnerID {
						author++
					}
					if last.Category == v.Category {
						category++
					}
				}
				if pass == 2 || (author < 2 && (category < 4 || pass == 1)) {
					choose = i
					break
				}
			}
		}
		if choose < 0 {
			break
		}
		v := pool[choose]
		used[v.VideoID] = true
		ordered = append(ordered, v)
		ids = append(ids, v.VideoID)
	}
	return ids
}
func videoCandidates(ctx context.Context, mode, category string) ([]uint64, error) {
	latest, e := repo.LatestVideoCards(ctx, category, time.Time{}, 0, 250)
	if e != nil {
		return nil, e
	}
	hot, e := repo.HotVideoCandidates(ctx, category)
	if e != nil {
		return nil, e
	}
	set := map[uint64]bool{}
	ids := make([]uint64, 0, 500)
	for _, v := range latest {
		set[v.VideoID] = true
		ids = append(ids, v.VideoID)
	}
	for _, id := range hot {
		if !set[id] {
			ids = append(ids, id)
			set[id] = true
		}
	}
	cards, e := repo.VideoCards(ctx, ids, category)
	if e != nil {
		return nil, e
	}
	signals, e := repo.VideoRankingSignals(ctx, ids)
	if e != nil {
		return nil, e
	}
	maintenance, stop := context.WithTimeout(ctx, 200*time.Millisecond)
	defer stop()
	if dao.SC.Redis.SetNX(maintenance, "feed:video:maintenance", "1", time.Minute).Val() {
		repo.PruneVideoDiscovery(maintenance)
	}
	return rankVideoCandidates(cards, signals, mode, time.Now().UTC()), nil
}
func GetVideoDiscovery(ctx context.Context, mode, category, raw string, limit int) (*model.VideoDiscoveryPage, error) {
	if limit < 1 || limit > 50 || (mode != "latest" && mode != "hot" && mode != "recommended") || (category != "" && !model.ValidVideoCategory(category)) {
		return nil, ErrInvalidFeedCursor
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	c, e := decodeVideoCursor(raw, mode, category)
	if e != nil {
		return nil, e
	}
	page := &model.VideoDiscoveryPage{Items: make([]model.VideoCard, 0), Mode: mode}
	if mode == "latest" {
		items, e := repo.LatestVideoCards(ctx, category, c.Time, c.ID, limit+1)
		if e != nil {
			return nil, e
		}
		page.HasMore = len(items) > limit
		if page.HasMore {
			items = items[:limit]
			last := items[len(items)-1]
			c.Time, c.ID = last.PublishedAt, last.VideoID
			page.NextCursor = encodeVideoCursor(c)
		}
		page.Items = items
	} else {
		var snapshot dao.VideoSnapshot
		redisCtx, stop := context.WithTimeout(ctx, time.Second)
		if raw != "" {
			snapshot, e = dao.VideoSnapshotRead(redisCtx, c.Snapshot)
			stop()
		} else {
			stop()
			c.Snapshot, snapshot, e = dao.VideoSnapshotCurrent(ctx, mode, category, func() ([]uint64, error) { return videoCandidates(ctx, mode, category) })
		}
		if e != nil {
			if raw == "" && (errors.Is(e, dao.ErrVideoDiscoveryUnavailable) || errors.Is(e, dao.ErrVideoSnapshotExpired)) {
				fallback, err := GetVideoDiscovery(ctx, "latest", category, "", limit)
				if fallback != nil {
					fallback.Degraded = true
				}
				return fallback, err
			}
			return nil, e
		}
		if snapshot.Mode != mode || snapshot.Category != category || c.Offset > len(snapshot.IDs) {
			return nil, ErrInvalidFeedCursor
		}
		// One batch fetch for all remaining <=500 candidates; advance past scanned
		// tombstones, never skip the unconsumed part of a fetched batch.
		cards, e := repo.VideoCards(ctx, snapshot.IDs[c.Offset:], category)
		if e != nil {
			return nil, e
		}
		byID := map[uint64]model.VideoCard{}
		for _, v := range cards {
			byID[v.VideoID] = v
		}
		for c.Offset < len(snapshot.IDs) && len(page.Items) < limit {
			id := snapshot.IDs[c.Offset]
			c.Offset++
			if v, ok := byID[id]; ok {
				page.Items = append(page.Items, v)
			}
		}
		// Skip a hidden tail so has_more=false is accurate for this read.
		for c.Offset < len(snapshot.IDs) {
			if _, ok := byID[snapshot.IDs[c.Offset]]; ok {
				break
			}
			c.Offset++
		}
		page.HasMore = c.Offset < len(snapshot.IDs)
		if page.HasMore {
			page.NextCursor = encodeVideoCursor(c)
		}
	}
	ids := make([]uint64, 0, len(page.Items))
	for _, v := range page.Items {
		ids = append(ids, v.VideoID)
	}
	short, stop := context.WithTimeout(ctx, 150*time.Millisecond)
	defer stop()
	page.DeliveryToken = dao.VideoDeliveryToken(short, ids)
	return page, nil
}

type discoverySearchCursor struct {
	V          int
	Kind, Term string
	ID         uint64
}
type DiscoverySearchPage struct {
	Items      []model.SearchItem `json:"items"`
	NextCursor string             `json:"next_cursor"`
	HasMore    bool               `json:"has_more"`
	Scope      string             `json:"scope"`
}

func SearchDiscovery(ctx context.Context, kind, term, raw string, limit int) (*DiscoverySearchPage, error) {
	if (kind != "video" && kind != "user" && kind != "live") || !utf8.ValidString(term) || len([]rune(term)) < 1 || len([]rune(term)) > 50 || limit < 1 || limit > 50 {
		return nil, ErrInvalidFeedCursor
	}
	c := discoverySearchCursor{V: 1, Kind: kind, Term: term}
	if raw != "" {
		b, e := base64.RawURLEncoding.DecodeString(raw)
		if len(raw) > 1024 || e != nil || json.Unmarshal(b, &c) != nil || c.V != 1 || c.Kind != kind || c.Term != term || c.ID == 0 {
			return nil, ErrInvalidFeedCursor
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	items, e := repo.SearchDiscovery(ctx, kind, term, c.ID, limit+1)
	if e != nil {
		return nil, e
	}
	p := &DiscoverySearchPage{Items: items, HasMore: len(items) > limit, Scope: "recent_5000"}
	if p.HasMore {
		p.Items = items[:limit]
		c.ID = p.Items[limit-1].ID
		b, _ := json.Marshal(c)
		p.NextCursor = base64.RawURLEncoding.EncodeToString(b)
	}
	return p, nil
}
