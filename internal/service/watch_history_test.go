package service

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestWatchCursorBoundToViewerAndCompleteOrderingKey(t *testing.T) {
	c := watchCursor{Version: 1, User: 9, Epoch: 2, Time: time.Now().UTC().Truncate(time.Microsecond), ID: 4}
	b, _ := json.Marshal(c)
	raw := base64.RawURLEncoding.EncodeToString(b)
	got, err := decodeWatchCursor(raw, 9)
	if err != nil || got.ID != c.ID || !got.Time.Equal(c.Time) {
		t.Fatal(got, err)
	}
	if _, err := decodeWatchCursor(raw, 10); err == nil {
		t.Fatal("cross-viewer cursor accepted")
	}
	for _, invalid := range []string{"invalid", strings.Repeat("x", 513), base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"u":9}`))} {
		if _, err := decodeWatchCursor(invalid, 9); err == nil {
			t.Fatal("invalid cursor accepted")
		}
	}
}
