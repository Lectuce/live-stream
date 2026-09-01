package kafka

import (
	"testing"
	"time"
)

func TestRealtimeDurationHistogramSnapshot(t *testing.T) {
	var histogram realtimeDurationHistogram
	histogram.observe(5 * time.Millisecond)
	histogram.observe(20 * time.Millisecond)
	histogram.observe(2 * time.Second)

	snapshot := snapshotRealtimeDuration(&histogram)
	if snapshot.Count != 3 {
		t.Fatalf("count = %d, want 3", snapshot.Count)
	}
	if snapshot.MaxNanoseconds != uint64(2*time.Second) {
		t.Fatalf("max = %d, want %d", snapshot.MaxNanoseconds, 2*time.Second)
	}
	if got := snapshot.Buckets["5000us"]; got != 1 {
		t.Fatalf("5ms cumulative bucket = %d, want 1", got)
	}
	if got := snapshot.Buckets["50000us"]; got != 2 {
		t.Fatalf("50ms cumulative bucket = %d, want 2", got)
	}
	if got := snapshot.Buckets["2000000us"]; got != 3 {
		t.Fatalf("2s cumulative bucket = %d, want 3", got)
	}
}
