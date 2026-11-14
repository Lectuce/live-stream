package service

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestFollowingCursorBoundaries(t *testing.T) {
	valid := followingCursor{Version: 1, Viewer: 7, ID: 99, Time: time.Date(2026, 11, 13, 2, 3, 4, 123456000, time.UTC)}
	encode := func(c followingCursor) string {
		data, _ := json.Marshal(c)
		return base64.RawURLEncoding.EncodeToString(data)
	}
	got, err := decodeFollowingCursor(encode(valid), 7)
	if err != nil || got.ID != 99 || !got.Time.Equal(valid.Time) {
		t.Fatalf("%+v %v", got, err)
	}
	for _, value := range []string{"invalid", strings.Repeat("a", 1025), encode(followingCursor{Version: 2, Viewer: 7, ID: 1, Time: valid.Time}), encode(followingCursor{Version: 1, Viewer: 8, ID: 1, Time: valid.Time}), encode(followingCursor{Version: 1, Viewer: 7, Time: valid.Time}), encode(followingCursor{Version: 1, Viewer: 7, ID: 1, Time: valid.Time.Add(time.Nanosecond)})} {
		if _, err := decodeFollowingCursor(value, 7); err == nil {
			t.Fatal("accepted invalid cursor")
		}
	}
}
