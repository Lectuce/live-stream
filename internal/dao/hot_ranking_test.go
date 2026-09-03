package dao

import (
	"math"
	"testing"
	"time"
)

func TestBlendMinuteBucketsApproximatesTrailingMinute(t *testing.T) {
	base := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		second int
		want   float64
	}{
		{second: 0, want: 120},
		{second: 30, want: 70},
		{second: 59, want: 20 + 100.0/60},
	}
	for _, test := range tests {
		got := blendMinuteBuckets(20, 100, base.Add(time.Duration(test.second)*time.Second))
		if math.Abs(got-test.want) > 1e-9 {
			t.Fatalf("second=%d got=%f want=%f", test.second, got, test.want)
		}
	}
}

func TestRedisNumberTreatsMissingAndInvalidAsZero(t *testing.T) {
	if redisNumber(nil) != 0 || redisNumber("invalid") != 0 || redisNumber("12") != 12 {
		t.Fatal("unexpected Redis number conversion")
	}
}
