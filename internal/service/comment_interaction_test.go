package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"

	"live-stream/internal/model"
)

func TestHotCommentCursorCannotCrossScopeOrSort(t *testing.T) {
	valid := hotCommentCursor{Version: 3, Sort: "hot", TargetType: "post", TargetID: 7, Snapshot: "ae9334e0-e17b-4c58-8a90-95127fbc960e", Offset: 20}
	encode := func(c hotCommentCursor) string {
		data, _ := json.Marshal(c)
		return base64.RawURLEncoding.EncodeToString(data)
	}
	if _, err := decodeHotCommentCursor(encode(valid), "post", 7); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"video", "unknown"} {
		if _, err := decodeHotCommentCursor(encode(valid), kind, 7); err == nil {
			t.Fatal("accepted foreign type")
		}
	}
	if _, err := decodeHotCommentCursor(encode(valid), "post", 8); err == nil {
		t.Fatal("accepted foreign target")
	}
	if _, err := decodeCommentCursor(encode(valid), "post", 7, 0); err == nil {
		t.Fatal("hot cursor accepted by latest")
	}
	for _, change := range []func(*hotCommentCursor){func(c *hotCommentCursor) { c.Sort = "latest" }, func(c *hotCommentCursor) { c.RootID = 2 }, func(c *hotCommentCursor) { c.Offset = -1 }, func(c *hotCommentCursor) { c.Offset = 501 }, func(c *hotCommentCursor) { c.Snapshot = "invalid" }} {
		c := valid
		change(&c)
		if _, err := decodeHotCommentCursor(encode(c), "post", 7); err == nil {
			t.Fatal("invalid cursor", c)
		}
	}
}

func TestHotCommentsSkipHiddenAndAdvanceByScannedIDs(t *testing.T) {
	ids := []uint64{8, 7, 6, 5, 4}
	calls := 0
	fetch := func(batch []uint64) ([]model.CommentView, error) {
		calls++
		rows := []model.CommentView{}
		for i := len(batch) - 1; i >= 0; i-- {
			id := batch[i]
			if id != 7 && id != 6 {
				rows = append(rows, model.CommentView{Comment: model.Comment{ID: id}})
			}
		}
		return rows, nil
	}
	items, next, err := scanHotComments(ids, 0, 2, fetch)
	if err != nil || len(items) != 2 || items[0].ID != 8 || items[1].ID != 5 || next != 4 {
		t.Fatal(items, next, err)
	}
	last, end, err := scanHotComments(ids, next, 2, fetch)
	if err != nil || len(last) != 1 || last[0].ID != 4 || end != 5 || calls > 5 {
		t.Fatal(last, end, err)
	}
	allHidden := func([]uint64) ([]model.CommentView, error) { return nil, nil }
	empty, end, err := scanHotComments(ids, 0, 2, allHidden)
	if err != nil || len(empty) != 0 || end != 5 {
		t.Fatal(empty, end, err)
	}
}

func TestReviewAndEditValidationBeforeDatabaseAccess(t *testing.T) {
	for _, req := range []model.ReviewCommentRequest{{Version: 0, Decision: "approved"}, {Version: 1, Decision: "rejected"}, {Version: 1, Decision: "hidden", Reason: " "}, {Version: 1, Decision: "deleted", Reason: "x"}} {
		if err := ReviewComment(context.Background(), 1, 2, req); !errors.Is(err, ErrCommentInput) {
			t.Fatal(req, err)
		}
	}
	if err := EditComment(context.Background(), 1, 2, model.EditCommentRequest{Version: 1, Content: " "}); !errors.Is(err, ErrCommentInput) {
		t.Fatal(err)
	}
	if err := ReportComment(context.Background(), 1, 2, ""); !errors.Is(err, ErrCommentInput) {
		t.Fatal(err)
	}
}
