package model

import (
	"encoding/json"
	"testing"
)

func TestVideoIDsSerializeAsStrings(t *testing.T) {
	// IDs above JavaScript's safe integer range must survive transport exactly.
	payload, err := json.Marshal(Video{VideoID: 9007199254740993, OwnerID: 9007199254740995})
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(payload, &result); err != nil {
		t.Fatal(err)
	}
	if result["video_id"] != "9007199254740993" || result["owner_id"] != "9007199254740995" {
		t.Fatalf("IDs were not preserved as strings: %s", payload)
	}
}

func TestOwnerVideoHidesStorageFilename(t *testing.T) {
	payload, err := json.Marshal(OwnerVideo{
		Video:    Video{VideoID: 9007199254740993, MediaFile: "private-storage.mp4"},
		HasMedia: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(payload, &result); err != nil {
		t.Fatal(err)
	}
	if result["video_id"] != "9007199254740993" || result["has_media"] != true {
		t.Fatalf("invalid owner DTO: %s", payload)
	}
	if _, exposed := result["media_file"]; exposed {
		t.Fatalf("storage filename exposed: %s", payload)
	}
}
