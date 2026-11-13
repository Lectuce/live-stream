package service

import (
	"errors"
	"live-stream/internal/model"
	"strings"
	"testing"
)

func TestEditVideoMetadataTransitions(t *testing.T) {
	text := func(s string) *string { return &s }
	for _, tc := range []struct {
		name            string
		published, auto bool
		process, audit  string
		req             model.UpdateVideoRequest
		wantErr         error
		wantAudit       string
	}{
		{name: "published rejected", published: true, req: model.UpdateVideoRequest{Title: text("new")}, wantErr: ErrVideoPublished},
		{name: "empty patch", req: model.UpdateVideoRequest{}, wantErr: ErrVideoParams},
		{name: "blank title", req: model.UpdateVideoRequest{Title: text("  ")}, wantErr: ErrVideoParams},
		{name: "long title", req: model.UpdateVideoRequest{Title: text(strings.Repeat("字", 101))}, wantErr: ErrVideoParams},
		{name: "long description", req: model.UpdateVideoRequest{Description: text(strings.Repeat("字", 5001))}, wantErr: ErrVideoParams},
		{name: "production re-review", process: "ready", audit: "approved", req: model.UpdateVideoRequest{Title: text("新标题")}, wantAudit: "pending"},
		{name: "dev approved", auto: true, process: "ready", req: model.UpdateVideoRequest{Title: text("新标题")}, wantAudit: "approved"},
		{name: "dev unfinished stays pending", auto: true, process: "pending", req: model.UpdateVideoRequest{Title: text("新标题")}, wantAudit: "pending"},
		{name: "clear description", process: "ready", audit: "approved", req: model.UpdateVideoRequest{Description: text("")}, wantAudit: "pending"},
		{name: "no-op preserves rejection", process: "ready", audit: "rejected", req: model.UpdateVideoRequest{Title: text("old")}, wantAudit: "rejected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			video := model.Video{Title: "old", Description: "description", PublishStatus: "draft", AuditStatus: tc.audit, ProcessStatus: tc.process, PlayURL: "/media/test.mp4"}
			if tc.published {
				video.PublishStatus = "published"
			}
			before := video
			err := editVideoMetadata(&video, tc.req, tc.auto)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error=%v want=%v", err, tc.wantErr)
			}
			if err != nil {
				if video != before {
					t.Fatal("invalid edit mutated the video")
				}
				return
			}
			if video.AuditStatus != tc.wantAudit {
				t.Fatalf("audit=%s want=%s", video.AuditStatus, tc.wantAudit)
			}
			if tc.req.Description != nil && video.Description != strings.TrimSpace(*tc.req.Description) {
				t.Fatal("description not saved")
			}
		})
	}
}
