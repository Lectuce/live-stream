package service

import (
	"errors"
	"testing"
)

func TestHotLiveFeedCursorRoundTrip(t *testing.T) {
	want := hotLiveFeedCursor{
		Version:    hotLiveFeedCursorVersion,
		SnapshotID: "2dc08783-74eb-4f75-a04d-fc48aa2928ee",
		Offset:     20,
	}

	encoded, err := encodeHotLiveFeedCursor(want)
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}

	got, err := decodeHotLiveFeedCursor(encoded)
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}

	if got != want {
		t.Fatalf("cursor mismatch: got=%+v want=%+v", got, want)
	}
}

func TestDecodeHotLiveFeedCursorRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name   string
		cursor hotLiveFeedCursor
	}{
		{
			name: "wrong version",
			cursor: hotLiveFeedCursor{
				Version:    hotLiveFeedCursorVersion + 1,
				SnapshotID: "2dc08783-74eb-4f75-a04d-fc48aa2928ee",
				Offset:     20,
			},
		},
		{
			name: "missing snapshot",
			cursor: hotLiveFeedCursor{
				Version: hotLiveFeedCursorVersion,
				Offset:  20,
			},
		},
		{
			name: "negative offset",
			cursor: hotLiveFeedCursor{
				Version:    hotLiveFeedCursorVersion,
				SnapshotID: "2dc08783-74eb-4f75-a04d-fc48aa2928ee",
				Offset:     -1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := encodeHotLiveFeedCursor(tt.cursor)
			if err != nil {
				t.Fatalf("encode cursor: %v", err)
			}

			_, err = decodeHotLiveFeedCursor(encoded)
			if !errors.Is(err, ErrInvalidFeedCursor) {
				t.Fatalf("error=%v, want ErrInvalidFeedCursor", err)
			}
		})
	}

	if _, err := decodeHotLiveFeedCursor("not-base64!"); !errors.Is(err, ErrInvalidFeedCursor) {
		t.Fatalf("malformed cursor error=%v, want ErrInvalidFeedCursor", err)
	}
}
