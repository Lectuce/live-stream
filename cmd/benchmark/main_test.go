package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResolveActiveClientCount(t *testing.T) {
	tests := []struct {
		name     string
		total    int
		ratio    float64
		override int
		want     int
	}{
		{name: "ratio", total: 1000, ratio: 0.01, override: -1, want: 10},
		{name: "minimum one", total: 10, ratio: 0.001, override: -1, want: 1},
		{name: "listener only", total: 1000, ratio: 0, override: -1, want: 0},
		{name: "override", total: 1000, ratio: 1, override: 7, want: 7},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := resolveActiveClientCount(test.total, test.ratio, test.override); got != test.want {
				t.Fatalf("resolveActiveClientCount() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestActiveClientsAreDistributedExactly(t *testing.T) {
	const total = 1000
	const active = 10
	count := 0
	ordinals := make([]int, 0, active)
	for clientID := 0; clientID < total; clientID++ {
		if ordinal, ok := activeClientOrdinal(clientID, total, active); ok {
			count++
			ordinals = append(ordinals, ordinal)
		}
	}
	if count != active {
		t.Fatalf("selected %d active clients, want %d", count, active)
	}
	for ordinal, got := range ordinals {
		if got != ordinal {
			t.Fatalf("active ordinal %d = %d", ordinal, got)
		}
	}
}

func TestLoadSessionIDs(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "sids.txt")
	first := fmt.Sprintf("%064x", 1)
	second := fmt.Sprintf("%064x", 2)
	if err := os.WriteFile(path, []byte(first+"\n\n"+second+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sessionIDs, err := loadSessionIDs(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessionIDs) != 2 || sessionIDs[0] != first || sessionIDs[1] != second {
		t.Fatalf("loadSessionIDs() = %#v", sessionIDs)
	}
}

func TestBenchmarkClientIPIsDistinct(t *testing.T) {
	seen := make(map[string]struct{})
	for ordinal := 0; ordinal < 1000; ordinal++ {
		address := benchmarkClientIP(ordinal)
		if _, exists := seen[address]; exists {
			t.Fatalf("duplicate benchmark address %q", address)
		}
		seen[address] = struct{}{}
	}
}

func TestBenchmarkSentAt(t *testing.T) {
	want := time.Unix(123, 456789)
	content := fmt.Sprintf("bench-7-42-%d", want.UnixNano())
	got, ok := benchmarkSentAt(content)
	if !ok {
		t.Fatal("benchmarkSentAt() did not parse a benchmark timestamp")
	}
	if !got.Equal(want) {
		t.Fatalf("benchmarkSentAt() = %s, want %s", got, want)
	}

	for _, invalid := range []string{"hello", "bench-1-2", "bench-1-2-invalid", "bench-1-2-0"} {
		if _, ok := benchmarkSentAt(invalid); ok {
			t.Fatalf("benchmarkSentAt(%q) unexpectedly succeeded", invalid)
		}
	}
}

func TestLatencyHistogramPercentiles(t *testing.T) {
	var histogram latencyHistogram
	for i := 0; i < 50; i++ {
		histogram.observe(time.Millisecond)
	}
	for i := 0; i < 45; i++ {
		histogram.observe(50 * time.Millisecond)
	}
	for i := 0; i < 5; i++ {
		histogram.observe(time.Second)
	}

	if got := histogram.percentile(0.50); got != time.Millisecond {
		t.Fatalf("p50 = %s, want %s", got, time.Millisecond)
	}
	if got := histogram.percentile(0.95); got != 50*time.Millisecond {
		t.Fatalf("p95 = %s, want %s", got, 50*time.Millisecond)
	}
	if got := histogram.percentile(0.99); got != time.Second {
		t.Fatalf("p99 = %s, want %s", got, time.Second)
	}
}

func TestDeliveryTrackerDeduplicatesReplayAndLive(t *testing.T) {
	tracker := newDeliveryTracker()
	message := incomingMessage{MessageID: "same", RoomSeq: 42}
	if !tracker.observe(message) {
		t.Fatal("first delivery was treated as duplicate")
	}
	if tracker.observe(message) {
		t.Fatal("duplicate delivery was counted again")
	}
}

func TestAcceptedMessagesCreateExpectedFanoutOnce(t *testing.T) {
	var stats metrics
	stats.audience.Store(1000)
	stats.registerSent("message-1")
	stats.recordTerminal("message-1", "accepted")
	stats.recordTerminal("message-1", "accepted")
	stats.recordTerminal("unknown", "accepted")

	if got := stats.accepted.Load(); got != 1 {
		t.Fatalf("accepted=%d, want 1", got)
	}
	if got := stats.expected.Load(); got != 1000 {
		t.Fatalf("expected=%d, want 1000", got)
	}
}

func TestRejectedMessagesDoNotCreateExpectedFanout(t *testing.T) {
	var stats metrics
	stats.audience.Store(1000)
	stats.registerSent("message-1")
	stats.recordTerminal("message-1", "rejected")
	stats.recordTerminal("message-1", "accepted")

	if got := stats.rejected.Load(); got != 1 {
		t.Fatalf("rejected=%d, want 1", got)
	}
	if got := stats.expected.Load(); got != 0 {
		t.Fatalf("expected=%d, want 0", got)
	}
}

func TestReplayHTTPBaseURL(t *testing.T) {
	got, err := replayHTTPBaseURL("", "wss://example.com/api/v1/ws")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://example.com" {
		t.Fatalf("derived base URL=%q", got)
	}
	got, err = replayHTTPBaseURL("http://127.0.0.1:9000/", "ws://unused")
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://127.0.0.1:9000" {
		t.Fatalf("configured base URL=%q", got)
	}
}
