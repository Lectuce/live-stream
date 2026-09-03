package hotrank

import (
	"math"
	"testing"
	"time"
)

func TestCalculateHotScoreUsesLogSignalsAndFreshness(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	settings := Settings{
		OnlineWeight:    3,
		LikeWeight:      2,
		DanmaWeight:     1,
		FreshnessWeight: 1,
		FreshnessDecay:  30 * time.Minute,
	}
	signals := Signals{
		Online:    99,
		Likes:     9,
		Danmas:    4,
		StartedAt: now.Add(-30 * time.Minute),
	}
	want := 3*math.Log(100) + 2*math.Log(10) + math.Log(5) + math.Exp(-1)
	got := CalculateHotScore(settings, signals, now)
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("score=%f want=%f", got, want)
	}
}

func TestCalculateHotScoreRewardsRecentActivityNotHistoricalLikes(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	settings := Settings{
		OnlineWeight:    3,
		LikeWeight:      2,
		DanmaWeight:     1,
		FreshnessWeight: 1,
		FreshnessDecay:  30 * time.Minute,
	}
	idle := CalculateHotScore(settings, Signals{StartedAt: now.Add(-time.Hour)}, now)
	active := CalculateHotScore(settings, Signals{
		Online:    10,
		Likes:     5,
		Danmas:    2,
		StartedAt: now.Add(-time.Hour),
	}, now)
	newRoom := CalculateHotScore(settings, Signals{StartedAt: now}, now)
	if active <= idle {
		t.Fatalf("active score=%f should exceed idle=%f", active, idle)
	}
	if newRoom <= idle {
		t.Fatalf("new room score=%f should exceed old idle=%f", newRoom, idle)
	}
}
