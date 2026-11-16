package repo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"live-stream/internal/model"
)

func TestWatchProgressOrderingAndRevocation(t *testing.T) {
	for _, scenario := range []string{"rewind", "retry", "old seq", "reused seq", "old session", "old version", "clear", "delete", "missing", "beyond duration", "unknown duration", "zero seq"} {
		t.Run(scenario, func(t *testing.T) {
			position := uint64(30000)
			row := model.WatchHistory{ID: 1, HistoryEpoch: 1, SessionID: "A", SessionVersion: 2, LastSeq: 4, PositionMS: 60000}
			req := model.WatchProgressRequest{WatchSessionRequest: model.WatchSessionRequest{SessionID: "A", SessionVersion: 2, HistoryEpoch: 1}, Seq: 5, PositionMS: &position}
			epoch, duration := uint64(1), uint64(120000)
			var want error
			update := false
			switch scenario {
			case "rewind":
				update = true
			case "retry":
				req.Seq, position = 4, 60000
			case "old seq":
				req.Seq = 3
			case "reused seq":
				req.Seq = 4
				want = ErrWatchInput
			case "old session":
				req.SessionID = "B"
				want = ErrWatchConflict
			case "old version":
				req.SessionVersion = 1
				want = ErrWatchConflict
			case "clear":
				epoch = 2
				want = ErrWatchConflict
			case "delete":
				now := time.Now()
				row.DeletedAt = &now
				want = ErrWatchConflict
			case "missing":
				row.ID = 0
				want = ErrWatchConflict
			case "beyond duration":
				position = 120001
				want = ErrWatchInput
			case "unknown duration":
				duration = 0
				want = ErrWatchInput
			case "zero seq":
				req.Seq = 0
				want = ErrWatchInput
			}
			got, err := validateWatchProgress(row, epoch, duration, req)
			if got != update || !errors.Is(err, want) {
				t.Fatalf("update=%v err=%v; want %v %v", got, err, update, want)
			}
		})
	}
}

func expectWatchState(mock sqlmock.Sqlmock, epoch uint64) {
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `watch_history_states`.*ON DUPLICATE KEY UPDATE").WithArgs(uint64(9), uint64(1)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT .* FROM `watch_history_states`.*FOR UPDATE").WithArgs(uint64(9), uint64(9), 1).WillReturnRows(sqlmock.NewRows([]string{"user_id", "history_epoch"}).AddRow(9, epoch))
}

func expectWatchVideo(mock sqlmock.Sqlmock) {
	mock.ExpectQuery("SELECT .* FROM `videos`.*publish_status='published'.*process_status='ready'.*audit_status='approved'.*FOR SHARE").WithArgs(uint64(7), 1).WillReturnRows(sqlmock.NewRows([]string{"video_id", "duration_ms"}).AddRow(7, 120000))
}

func TestWatchClaimRetriesAndDelayedClaims(t *testing.T) {
	for _, scenario := range []string{"retry", "new", "stale claim", "clear"} {
		t.Run(scenario, func(t *testing.T) {
			mock := communityDB(t)
			epoch := uint64(1)
			if scenario == "clear" {
				epoch = 2
			}
			expectWatchState(mock, epoch)
			req := model.WatchSessionRequest{SessionID: "A", HistoryEpoch: 1, SessionVersion: 1}
			if scenario != "clear" {
				expectWatchVideo(mock)
				mock.ExpectQuery("SELECT .* FROM `watch_history` WHERE user_id = .*video_id = .*LIMIT").WithArgs(uint64(9), uint64(7), 1).WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "video_id", "history_epoch", "session_id", "session_version", "last_seq", "position_ms", "last_watched_at"}).AddRow(4, 9, 7, 1, "A", 2, 8, 60000, time.Now()))
				if scenario == "new" {
					req.SessionID, req.SessionVersion = "B", 2
					mock.ExpectExec("UPDATE `watch_history` SET").WillReturnResult(sqlmock.NewResult(0, 1))
				}
				if scenario == "stale claim" {
					req.SessionID = "B"
				}
			}
			bad := scenario == "clear" || scenario == "stale claim"
			if bad {
				mock.ExpectRollback()
			} else {
				mock.ExpectCommit()
			}
			result, err := StartWatchSession(context.Background(), 9, 7, req)
			if bad {
				if !errors.Is(err, ErrWatchConflict) {
					t.Fatal(err)
				}
				return
			}
			if err != nil || result.PositionMS != 60000 {
				t.Fatal(result, err)
			}
			if scenario == "retry" && (result.SessionVersion != 2 || result.LastSeq != 8) {
				t.Fatal("retry reset sequence", result)
			}
			if scenario == "new" && (result.SessionVersion != 3 || result.LastSeq != 0 || result.SessionID != "B") {
				t.Fatal(result)
			}
		})
	}
}

func TestWatchSaveRewindInTransactionAndIgnoreOldSamples(t *testing.T) {
	for _, seq := range []uint64{3, 5} {
		mock := communityDB(t)
		expectWatchState(mock, 1)
		mock.ExpectQuery("SELECT .* FROM `watch_history` WHERE user_id = .*video_id = .*LIMIT").WithArgs(uint64(9), uint64(7), 1).WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "video_id", "history_epoch", "session_id", "session_version", "last_seq", "position_ms"}).AddRow(4, 9, 7, 1, "A", 2, 4, 60000))
		expectWatchVideo(mock)
		if seq == 5 {
			mock.ExpectExec("UPDATE `watch_history` SET `last_seq`=.*,`last_watched_at`=.*,`position_ms`=.*WHERE id = .* AND user_id =").WithArgs(seq, sqlmock.AnyArg(), uint64(30000), uint64(4), uint64(9)).WillReturnResult(sqlmock.NewResult(0, 1))
		}
		mock.ExpectCommit()
		position := uint64(30000)
		err := SaveWatchProgress(context.Background(), 9, 7, model.WatchProgressRequest{WatchSessionRequest: model.WatchSessionRequest{SessionID: "A", SessionVersion: 2, HistoryEpoch: 1}, Seq: seq, PositionMS: &position})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestWatchClearLocksBeforeInvalidatingAllSessions(t *testing.T) {
	mock := communityDB(t)
	expectWatchState(mock, 3)
	mock.ExpectExec("UPDATE `watch_history_states` SET `history_epoch`=.*WHERE `user_id` =").WithArgs(uint64(4), uint64(9)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := DeleteWatchHistory(context.Background(), 9, 0); err != nil {
		t.Fatal(err)
	}
}

func TestWatchDeletingAbsentRecordStillRevokesEarlierRead(t *testing.T) {
	mock := communityDB(t)
	expectWatchState(mock, 1)
	mock.ExpectQuery("SELECT .* FROM `watch_history` WHERE user_id = .*video_id = .*LIMIT").WithArgs(uint64(9), uint64(7), 1).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	// A tombstone with version 1 defeats a delayed claim based on an absent row
	// (version 0). No video ownership or availability is needed to remove history.
	mock.ExpectExec("INSERT INTO `watch_history`").WithArgs(uint64(9), uint64(7), uint64(1), uint64(0), "", uint64(1), uint64(0), nil, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(4, 1))
	mock.ExpectCommit()
	if err := DeleteWatchHistory(context.Background(), 9, 7); err != nil {
		t.Fatal(err)
	}
}

func TestWatchListScopesViewerEpochAndPublicMetadata(t *testing.T) {
	mock := communityDB(t)
	expectWatchState(mock, 2)
	before := time.Now().UTC().Truncate(time.Microsecond)
	mock.ExpectQuery("SELECT .*COALESCE.* FROM watch_history AS h LEFT JOIN videos v ON .*v.deleted_at IS NULL.*v.publish_status='published'.*v.process_status='ready'.*v.audit_status='approved'.*WHERE .*h.user_id = .*h.history_epoch = .*h.deleted_at IS NULL.*h.last_watched_at IS NOT NULL.*h.last_watched_at < .*ORDER BY h.last_watched_at DESC, h.id DESC LIMIT").WithArgs(uint64(9), uint64(2), before, before, uint64(5), 21).WillReturnRows(sqlmock.NewRows([]string{"id", "video_id", "title", "available"}).AddRow(4, 7, "", false))
	mock.ExpectCommit()
	items, epoch, err := ListWatchHistory(context.Background(), 9, 2, before, 5, 21)
	if err != nil || epoch != 2 || len(items) != 1 || items[0].Available || items[0].Title != "" {
		t.Fatal(items, epoch, err)
	}
}

func TestWatchReadMasksClearedAndDeletedPositions(t *testing.T) {
	now := time.Now()
	row := model.WatchHistory{VideoID: 7, HistoryEpoch: 1, PositionMS: 60000, LastWatchedAt: &now}
	if got := watchDTO(row, 1, 30000); !got.HasHistory || got.PositionMS != 30000 {
		t.Fatal(got)
	}
	if got := watchDTO(row, 2, 120000); got.HasHistory || got.PositionMS != 0 {
		t.Fatal(got)
	}
	row.DeletedAt = &now
	if got := watchDTO(row, 1, 120000); got.HasHistory || got.PositionMS != 0 {
		t.Fatal(got)
	}
}
