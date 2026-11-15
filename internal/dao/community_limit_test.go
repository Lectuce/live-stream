package dao

import (
	"context"
	"testing"
)

func TestCommunityLimitConfigurationAndUnavailableRedis(t *testing.T) {
	t.Setenv("COMMUNITY_LIMIT_REPORT", "4")
	a, ip, pair := CommunityLimitPolicy("report")
	if a != 4 || ip != 20 || pair != 2 {
		t.Fatal(a, ip, pair)
	}
	t.Setenv("COMMUNITY_LIMIT_REPORT", "0")
	a, _, _ = CommunityLimitPolicy("report")
	if a != 10 {
		t.Fatal("zero disabled protection")
	}
	previous := SC
	SC = nil
	t.Cleanup(func() { SC = previous })
	allowed, err := AllowCommunityWrite(context.Background(), "report", 7, "127.0.0.1", "post:1")
	if allowed || err == nil {
		t.Fatal("failed open")
	}
}
