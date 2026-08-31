package main

import "testing"

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
	for clientID := 0; clientID < total; clientID++ {
		if isActiveClient(clientID, total, active) {
			count++
		}
	}
	if count != active {
		t.Fatalf("selected %d active clients, want %d", count, active)
	}
}
