package repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"live-stream/internal/dao"
	"live-stream/internal/model"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// LikeAggregationEvent is the minimal durable identity of one consumed like.
// Kafka partition+offset, not an in-memory window boundary, determines whether
// the event was already included in a committed aggregate.
type LikeAggregationEvent struct {
	Topic     string
	Partition int
	Offset    int64
	RoomID    uint64
}

type LikeBatchApplyResult struct {
	CountsByRoom map[uint64]int64
	DeltaByRoom  map[uint64]int64
	BatchCount   int
	EventCount   int64
}

type likeSource struct {
	topic     string
	partition int
}

type likeBatchPlan struct {
	batch model.LikeBatch
}

// ApplyLikeBatches stores one aggregate row per room and consumed partition
// range, increments each room once by the aggregate delta, and advances a
// partition checkpoint in the same MySQL transaction. If Kafka replays after
// the DB commit but before its offset commit, events at or below LastOffset are
// skipped even when the next in-memory batch has different boundaries.
func ApplyLikeBatches(
	ctx context.Context,
	consumerGroup string,
	events []LikeAggregationEvent,
) (result LikeBatchApplyResult, err error) {
	result.CountsByRoom = make(map[uint64]int64)
	result.DeltaByRoom = make(map[uint64]int64)
	if len(events) == 0 {
		return result, nil
	}
	if consumerGroup == "" {
		return result, fmt.Errorf("like aggregate consumer group cannot be empty")
	}

	bySource := make(map[likeSource][]LikeAggregationEvent)
	roomIDs := make(map[uint64]struct{})
	for _, event := range events {
		if event.Topic == "" {
			return result, fmt.Errorf("like aggregate topic cannot be empty")
		}
		if event.Partition < 0 || event.Offset < 0 {
			return result, fmt.Errorf(
				"invalid like kafka position topic=%s partition=%d offset=%d",
				event.Topic,
				event.Partition,
				event.Offset,
			)
		}
		if event.RoomID == 0 {
			return result, fmt.Errorf("like aggregate room_id cannot be zero")
		}
		source := likeSource{topic: event.Topic, partition: event.Partition}
		bySource[source] = append(bySource[source], event)
		roomIDs[event.RoomID] = struct{}{}
	}

	sources := make([]likeSource, 0, len(bySource))
	for source := range bySource {
		sources = append(sources, source)
	}
	sort.Slice(sources, func(i, j int) bool {
		if sources[i].topic != sources[j].topic {
			return sources[i].topic < sources[j].topic
		}
		return sources[i].partition < sources[j].partition
	})

	err = dao.SC.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, source := range sources {
			checkpoint, err := lockLikeCheckpoint(tx, consumerGroup, source)
			if err != nil {
				return err
			}
			plans, lastOffset, err := buildLikeBatchPlans(
				consumerGroup,
				source,
				bySource[source],
				checkpoint.LastOffset,
			)
			if err != nil {
				return err
			}
			for _, plan := range plans {
				inserted := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&plan.batch)
				if inserted.Error != nil {
					return fmt.Errorf("insert like batch %s: %w", plan.batch.BatchID, inserted.Error)
				}
				if inserted.RowsAffected == 0 {
					continue
				}

				updated := tx.Model(&model.Room{}).
					Where("room_id = ?", plan.batch.RoomID).
					UpdateColumn("like_count", gorm.Expr("like_count + ?", plan.batch.Delta))
				if updated.Error != nil {
					return fmt.Errorf("increment room %d like count by %d: %w", plan.batch.RoomID, plan.batch.Delta, updated.Error)
				}
				if updated.RowsAffected != 1 {
					return fmt.Errorf("increment room %d like count: room not found", plan.batch.RoomID)
				}
				result.DeltaByRoom[plan.batch.RoomID] += plan.batch.Delta
				result.BatchCount++
				result.EventCount += plan.batch.Delta
			}

			if lastOffset > checkpoint.LastOffset {
				updated := tx.Model(&model.LikeAggregationCheckpoint{}).
					Where(
						"consumer_group = ? AND topic = ? AND kafka_partition = ?",
						consumerGroup,
						source.topic,
						source.partition,
					).
					Updates(map[string]any{
						"last_offset": lastOffset,
						"updated_at":  time.Now(),
					})
				if updated.Error != nil {
					return fmt.Errorf("advance like checkpoint: %w", updated.Error)
				}
				if updated.RowsAffected != 1 {
					return fmt.Errorf("advance like checkpoint: checkpoint not found")
				}
			}
		}

		ids := make([]uint64, 0, len(roomIDs))
		for roomID := range roomIDs {
			ids = append(ids, roomID)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		var rooms []model.Room
		if err := tx.Select("room_id", "like_count").Where("room_id IN ?", ids).Find(&rooms).Error; err != nil {
			return fmt.Errorf("read aggregated room like counts: %w", err)
		}
		if len(rooms) != len(ids) {
			return fmt.Errorf("read aggregated room like counts: found %d of %d rooms", len(rooms), len(ids))
		}
		for _, room := range rooms {
			result.CountsByRoom[room.RoomID] = room.LikeCount
		}
		return nil
	})
	return result, err
}

func lockLikeCheckpoint(
	tx *gorm.DB,
	consumerGroup string,
	source likeSource,
) (model.LikeAggregationCheckpoint, error) {
	seed := model.LikeAggregationCheckpoint{
		ConsumerGroup: consumerGroup,
		Topic:         source.topic,
		Partition:     source.partition,
		LastOffset:    -1,
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&seed).Error; err != nil {
		return seed, fmt.Errorf("create like checkpoint: %w", err)
	}
	var checkpoint model.LikeAggregationCheckpoint
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(
			"consumer_group = ? AND topic = ? AND kafka_partition = ?",
			consumerGroup,
			source.topic,
			source.partition,
		).
		Take(&checkpoint).Error
	if err != nil {
		return checkpoint, fmt.Errorf("lock like checkpoint: %w", err)
	}
	return checkpoint, nil
}

func buildLikeBatchPlans(
	consumerGroup string,
	source likeSource,
	events []LikeAggregationEvent,
	lastAppliedOffset int64,
) ([]likeBatchPlan, int64, error) {
	byOffset := make(map[int64]LikeAggregationEvent, len(events))
	for _, event := range events {
		if event.Topic != source.topic || event.Partition != source.partition {
			return nil, lastAppliedOffset, fmt.Errorf("like event source does not match batch source")
		}
		if previous, exists := byOffset[event.Offset]; exists && previous.RoomID != event.RoomID {
			return nil, lastAppliedOffset, fmt.Errorf(
				"conflicting room ids at kafka partition=%d offset=%d",
				source.partition,
				event.Offset,
			)
		}
		byOffset[event.Offset] = event
	}
	offsets := make([]int64, 0, len(byOffset))
	for offset := range byOffset {
		if offset > lastAppliedOffset {
			offsets = append(offsets, offset)
		}
	}
	sort.Slice(offsets, func(i, j int) bool { return offsets[i] < offsets[j] })
	if len(offsets) == 0 {
		return nil, lastAppliedOffset, nil
	}

	type roomRange struct {
		start int64
		end   int64
		delta int64
	}
	byRoom := make(map[uint64]roomRange)
	for _, offset := range offsets {
		event := byOffset[offset]
		rangeValue, exists := byRoom[event.RoomID]
		if !exists {
			rangeValue.start = offset
		}
		rangeValue.end = offset
		rangeValue.delta++
		byRoom[event.RoomID] = rangeValue
	}
	roomIDs := make([]uint64, 0, len(byRoom))
	for roomID := range byRoom {
		roomIDs = append(roomIDs, roomID)
	}
	sort.Slice(roomIDs, func(i, j int) bool { return roomIDs[i] < roomIDs[j] })

	now := time.Now()
	plans := make([]likeBatchPlan, 0, len(roomIDs))
	for _, roomID := range roomIDs {
		rangeValue := byRoom[roomID]
		batchID := makeLikeBatchID(
			consumerGroup,
			source.topic,
			source.partition,
			roomID,
			rangeValue.start,
			rangeValue.end,
		)
		plans = append(plans, likeBatchPlan{batch: model.LikeBatch{
			BatchID:       batchID,
			ConsumerGroup: consumerGroup,
			Topic:         source.topic,
			Partition:     source.partition,
			RoomID:        roomID,
			StartOffset:   rangeValue.start,
			EndOffset:     rangeValue.end,
			Delta:         rangeValue.delta,
			CreatedAt:     now,
		}})
	}
	return plans, offsets[len(offsets)-1], nil
}

func makeLikeBatchID(
	consumerGroup string,
	topic string,
	partition int,
	roomID uint64,
	startOffset int64,
	endOffset int64,
) string {
	var raw strings.Builder
	raw.WriteString(consumerGroup)
	raw.WriteByte('|')
	raw.WriteString(topic)
	raw.WriteByte('|')
	raw.WriteString(strconv.Itoa(partition))
	raw.WriteByte('|')
	raw.WriteString(strconv.FormatUint(roomID, 10))
	raw.WriteByte('|')
	raw.WriteString(strconv.FormatInt(startOffset, 10))
	raw.WriteByte('|')
	raw.WriteString(strconv.FormatInt(endOffset, 10))
	sum := sha256.Sum256([]byte(raw.String()))
	return hex.EncodeToString(sum[:])
}
