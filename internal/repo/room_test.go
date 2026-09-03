package repo

import (
	"live-stream/internal/dao"
	"live-stream/internal/model"
	"testing"
)

func TestOrderRoomsByHotRankingPreservesRedisOrderAndSkipsStaleRooms(t *testing.T) {
	ranking := []dao.HotRoomRank{
		{RoomID: 3, Score: 30},
		{RoomID: 2, Score: 20},
		{RoomID: 1, Score: 10},
	}
	rooms := []model.HotRoomDTO{
		{RoomID: 1, RoomTitle: "one"},
		{RoomID: 3, RoomTitle: "three"},
	}
	scores := map[uint64]float64{1: 10, 2: 20, 3: 30}

	ordered, seen := orderRoomsByHotRanking(ranking, rooms, scores, 10)
	if len(ordered) != 2 || ordered[0].RoomID != 3 || ordered[1].RoomID != 1 {
		t.Fatalf("unexpected order: %+v", ordered)
	}
	if ordered[0].HotScore != 30 || ordered[1].HotScore != 10 {
		t.Fatalf("scores not attached: %+v", ordered)
	}
	if _, exists := seen[2]; exists {
		t.Fatal("stale room should not be marked as seen")
	}
}
