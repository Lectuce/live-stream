package service

import (
	"live-stream/internal/model"
	"strings"
	"testing"
)

func TestReviewRequestValidation(t *testing.T) {
	for _, req := range []model.ReviewVideoRequest{
		{Decision: "approved"}, {ContentVersion: 1, Decision: "publish"},
		{ContentVersion: 1, Decision: "rejected", Reason: "  "},
		{ContentVersion: 1, Decision: "rejected", Reason: strings.Repeat("字", 1001)},
	} {
		if ValidateReviewRequest(&req) == nil {
			t.Fatalf("accepted invalid request: %+v", req)
		}
	}
	valid := model.ReviewVideoRequest{ContentVersion: 1, Decision: "rejected", Reason: " 原因 "}
	if ValidateReviewRequest(&valid) != nil || valid.Reason != "原因" {
		t.Fatal("valid rejection failed")
	}
}

func TestReviewRejectsStaleOrUnavailableContent(t *testing.T) {
	base := model.Video{ContentVersion: 3, AuditStatus: "pending", ProcessStatus: "ready", PublishStatus: "draft", MediaFile: "private.mp4"}
	if checkReviewVersion(&base, 3) != nil {
		t.Fatal("valid review rejected")
	}
	for name, change := range map[string]func(*model.Video){
		"edited":           func(v *model.Video) { v.ContentVersion++ },
		"already reviewed": func(v *model.Video) { v.AuditStatus = "approved" },
		"published":        func(v *model.Video) { v.PublishStatus = "published" },
		"unprocessed":      func(v *model.Video) { v.ProcessStatus = "pending" },
		"missing file":     func(v *model.Video) { v.MediaFile = "" },
		"deleted":          func(v *model.Video) { v.DeletedAt.Valid = true },
	} {
		t.Run(name, func(t *testing.T) {
			v := base
			change(&v)
			if checkReviewVersion(&v, 3) != ErrReviewConflict {
				t.Fatal("unsafe review accepted")
			}
		})
	}
}

func TestMetadataEditInvalidatesReviewVersion(t *testing.T) {
	v := model.Video{Title: "old", Description: "", ContentVersion: 7, AuditReason: "old rejection", AuditStatus: "rejected", ProcessStatus: "ready", PublishStatus: "draft", MediaFile: "file.mp4"}
	title := "new"
	if err := editVideoMetadata(&v, model.UpdateVideoRequest{Title: &title}, false); err != nil {
		t.Fatal(err)
	}
	if v.ContentVersion != 8 || v.AuditReason != "" || v.AuditStatus != "pending" || v.SubmittedAt == nil {
		t.Fatalf("not invalidated: %+v", v)
	}
	if err := checkReviewVersion(&v, 7); err != ErrReviewConflict {
		t.Fatal("old decision accepted")
	}
	if err := editVideoMetadata(&v, model.UpdateVideoRequest{Title: &title}, false); err != nil {
		t.Fatal(err)
	}
	if v.ContentVersion != 8 {
		t.Fatal("no-op edit changed version")
	}
}
