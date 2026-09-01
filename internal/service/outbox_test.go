package service

import (
	"testing"
	"time"
)

func TestOutboxRetryDelayIsBounded(t *testing.T) {
	tests := []struct {
		attempts uint
		want     time.Duration
	}{
		{attempts: 0, want: 100 * time.Millisecond},
		{attempts: 1, want: 200 * time.Millisecond},
		{attempts: 6, want: 6400 * time.Millisecond},
		{attempts: 100, want: 6400 * time.Millisecond},
	}
	for _, test := range tests {
		if got := outboxRetryDelay(test.attempts); got != test.want {
			t.Fatalf("attempts=%d delay=%s, want %s", test.attempts, got, test.want)
		}
	}
}
