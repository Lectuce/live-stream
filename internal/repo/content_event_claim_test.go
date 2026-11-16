package repo

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/gorm"
)

func TestClaimContentEventEmptyIsIdle(t *testing.T) {
	mock := communityDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT \\* FROM `outbox_events` WHERE .* ORDER BY id ASC LIMIT \\? FOR UPDATE SKIP LOCKED").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()

	event, err := ClaimContentEvent(context.Background())
	if event == nil {
		t.Fatal("claim should return a non-nil empty event for a uniform caller contract")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected idle sentinel, got %v", err)
	}
}
