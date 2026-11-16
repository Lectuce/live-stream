package config

import (
	"strings"
	"testing"
)

func TestLiveMediaConfig(t *testing.T) {
	t.Setenv("LIVE_MEDIA_SECRET", strings.Repeat("a", 32))
	t.Setenv("LIVE_PUSH_BASE", "rtmp://example.test:1935/live")
	t.Setenv("LIVE_PLAY_BASE", "/live-media")
	t.Setenv("LIVE_SRS_API", "")
	c, err := LiveMediaConfig()
	if err != nil {
		t.Fatal(err)
	}
	if c.API != "http://127.0.0.1:1985" {
		t.Fatal(c.API)
	}
	if c.Token("one") == c.Token("two") {
		t.Fatal("tokens must bind the stream")
	}
	for _, tt := range []struct{ key, value string }{
		{"LIVE_MEDIA_SECRET", "short"}, {"LIVE_PUSH_BASE", "https://host/live"}, {"LIVE_PUSH_BASE", "rtmp://host/other"}, {"LIVE_PLAY_BASE", "javascript:alert(1)"}, {"LIVE_PLAY_BASE", "//external.test/live"},
	} {
		t.Run(tt.key+tt.value, func(t *testing.T) {
			t.Setenv(tt.key, tt.value)
			if _, err := LiveMediaConfig(); err == nil {
				t.Fatal("accepted invalid config")
			}
		})
	}
}
