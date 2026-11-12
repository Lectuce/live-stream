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
