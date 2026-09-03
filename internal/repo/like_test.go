package repo

import "testing"

func TestBuildLikeBatchPlansGroupsByRoomAndUsesKafkaRange(t *testing.T) {
	source := likeSource{topic: "danmu_topic", partition: 3}
	events := []LikeAggregationEvent{
		{Topic: source.topic, Partition: source.partition, Offset: 10, RoomID: 2},
		{Topic: source.topic, Partition: source.partition, Offset: 11, RoomID: 1},
		{Topic: source.topic, Partition: source.partition, Offset: 12, RoomID: 2},
	}

	plans, lastOffset, err := buildLikeBatchPlans("realtime", source, events, 9)
	if err != nil {
		t.Fatal(err)
	}
	if lastOffset != 12 {
		t.Fatalf("last offset = %d, want 12", lastOffset)
	}
	if len(plans) != 2 {
		t.Fatalf("plans = %d, want 2", len(plans))
	}
	if got := plans[0].batch; got.RoomID != 1 || got.Delta != 1 || got.StartOffset != 11 || got.EndOffset != 11 {
		t.Fatalf("room 1 batch = %+v", got)
	}
	if got := plans[1].batch; got.RoomID != 2 || got.Delta != 2 || got.StartOffset != 10 || got.EndOffset != 12 {
		t.Fatalf("room 2 batch = %+v", got)
	}

	again, _, err := buildLikeBatchPlans("realtime", source, events, 9)
	if err != nil {
		t.Fatal(err)
	}
	if plans[0].batch.BatchID != again[0].batch.BatchID || plans[1].batch.BatchID != again[1].batch.BatchID {
		t.Fatal("batch ids are not deterministic")
	}
}

func TestBuildLikeBatchPlansSkipsCheckpointedOffsetsAcrossNewBoundary(t *testing.T) {
	source := likeSource{topic: "danmu_topic", partition: 0}
	events := []LikeAggregationEvent{
		{Topic: source.topic, Partition: source.partition, Offset: 20, RoomID: 7},
		{Topic: source.topic, Partition: source.partition, Offset: 21, RoomID: 7},
		{Topic: source.topic, Partition: source.partition, Offset: 22, RoomID: 7},
	}

	plans, lastOffset, err := buildLikeBatchPlans("realtime", source, events, 21)
	if err != nil {
		t.Fatal(err)
	}
	if lastOffset != 22 || len(plans) != 1 {
		t.Fatalf("last=%d plans=%d", lastOffset, len(plans))
	}
	if got := plans[0].batch; got.Delta != 1 || got.StartOffset != 22 || got.EndOffset != 22 {
		t.Fatalf("replay batch = %+v", got)
	}
}

func TestBuildLikeBatchPlansDeduplicatesSameKafkaOffset(t *testing.T) {
	source := likeSource{topic: "danmu_topic", partition: 1}
	event := LikeAggregationEvent{Topic: source.topic, Partition: source.partition, Offset: 4, RoomID: 9}
	plans, _, err := buildLikeBatchPlans("realtime", source, []LikeAggregationEvent{event, event}, -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || plans[0].batch.Delta != 1 {
		t.Fatalf("plans = %+v", plans)
	}
}

func TestBuildLikeBatchPlansRejectsConflictingKafkaOffset(t *testing.T) {
	source := likeSource{topic: "danmu_topic", partition: 1}
	_, _, err := buildLikeBatchPlans("realtime", source, []LikeAggregationEvent{
		{Topic: source.topic, Partition: source.partition, Offset: 4, RoomID: 9},
		{Topic: source.topic, Partition: source.partition, Offset: 4, RoomID: 10},
	}, -1)
	if err == nil {
		t.Fatal("expected conflicting offset error")
	}
}
