package outbox

import (
	"encoding/json"
	"github.com/google/uuid"
	"live-stream/internal/model"
	"strings"
	"testing"
	"time"
)

func TestEventValidation(t *testing.T) {
	event := model.ContentEvent{EventID: uuid.NewString(), BusinessKey: "video.published:1", EventType: "video.published", ActorID: 7, TargetID: 1, PublishedAt: time.Now().UTC().Truncate(time.Microsecond)}
	data, _ := json.Marshal(event)
	if _, err := DecodeEvent(data); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"id", "kind", "key", "owner", "time"} {
		bad := event
		switch field {
		case "id":
			bad.EventID = "oops"
		case "kind":
			bad.EventType = "unknown"
		case "key":
			bad.BusinessKey = "video.published:2"
		case "owner":
			bad.ActorID = 0
		case "time":
			bad.PublishedAt = time.Time{}
		}
		data, _ := json.Marshal(bad)
		if _, err := DecodeEvent(data); err == nil {
			t.Fatal(field)
		}
	}
	if _, err := DecodeEvent([]byte(strings.Repeat("a", 65537))); err == nil {
		t.Fatal("unbounded payload")
	}
}

func TestLiveStartedEventAccepted(t *testing.T) {
	event := model.ContentEvent{EventID: uuid.NewString(), BusinessKey: "live.started:42", EventType: "live.started", ActorID: 7, TargetID: 42, PublishedAt: time.Now().UTC().Truncate(time.Microsecond)}
	data, _ := json.Marshal(event)
	if _, err := DecodeEvent(data); err != nil {
		t.Fatal(err)
	}
}
