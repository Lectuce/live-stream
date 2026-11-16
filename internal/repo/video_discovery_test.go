package repo

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/gorm"
	"live-stream/internal/dao"
	"testing"
	"time"
)

func TestPlayCreditRejectsSeekPauseRewindAndLongGaps(t *testing.T) {
	for _, tc := range []struct {
		from, to uint64
		elapsed  time.Duration
		want     uint64
	}{
		{0, 5000, 5 * time.Second, 5000}, {5000, 10000, 5 * time.Second, 5000}, {0, 10000, 5 * time.Second, 5000},
		{0, 60000, 5 * time.Second, 0}, {5000, 5000, 10 * time.Second, 0}, {5000, 1000, 5 * time.Second, 0}, {0, 10000, time.Hour, 0},
	} {
		if got := playCredit(tc.from, tc.to, tc.elapsed); got != tc.want {
			t.Fatal(tc, got)
		}
	}
}
func TestBehaviorDuplicateDoesNotIncrementMetric(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		m := communityDB(t)
		m.ExpectBegin()
		insert := m.ExpectExec("INSERT INTO `video_behaviors`").WithArgs(time.Now().UTC().Format("2006-01-02"), uint64(9), uint64(7), "play")
		if duplicate {
			insert.WillReturnResult(sqlmock.NewResult(0, 0))
		} else {
			insert.WillReturnResult(sqlmock.NewResult(0, 1))
			m.ExpectExec("INSERT INTO `video_metrics`.*ON DUPLICATE KEY UPDATE `plays`=plays \\+ 1").WillReturnResult(sqlmock.NewResult(0, 1))
		}
		m.ExpectCommit()
		e := dao.SC.Db.Transaction(func(tx *gorm.DB) error { return recordVideoBehavior(tx, 9, 7, "play") })
		if e != nil {
			t.Fatal(e)
		}
	}
}
func TestLatestDiscoveryUsesPublicationTimeAndVisibility(t *testing.T) {
	m := communityDB(t)
	before := time.Now().UTC()
	m.ExpectQuery("SELECT .* FROM videos v JOIN users u .*v.deleted_at IS NULL.*v.publish_status='published'.*v.process_status='ready'.*v.audit_status='approved'.*v.category = .*v.published_at < .*v.published_at = .*v.video_id < .*ORDER BY v.published_at DESC,v.video_id DESC LIMIT").WithArgs("games", before, before, uint64(8), 21).WillReturnRows(sqlmock.NewRows([]string{"video_id"}).AddRow(7))
	items, e := LatestVideoCards(context.Background(), "games", before, 8, 21)
	if e != nil || len(items) != 1 {
		t.Fatal(items, e)
	}
}
func TestDiscoverySearchIsBoundedAndEscapesWildcards(t *testing.T) {
	m := communityDB(t)
	m.ExpectQuery("SELECT .*LIMIT 5000.*title LIKE .*ESCAPE '!'.*ORDER BY id DESC LIMIT").WithArgs("%100!%!_!!%", uint64(0), uint64(0), 21).WillReturnRows(sqlmock.NewRows([]string{"id", "title"}).AddRow(7, "100%_!"))
	items, e := SearchDiscovery(context.Background(), "video", "100%_!", 0, 21)
	if e != nil || len(items) != 1 || items[0].Kind != "video" {
		t.Fatal(items, e)
	}
}
