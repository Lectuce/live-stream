package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"live-stream/internal/dao"
	"live-stream/internal/model"
	"live-stream/internal/repo"
)

func Topic() string {
	if value := strings.TrimSpace(os.Getenv("CONTENT_EVENTS_TOPIC")); value != "" {
		return value
	}
	return "content-events"
}

// Start owns both workers. Caller cancels ctx and waits for done on shutdown.
func Start(ctx context.Context, brokers []string) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		var workers sync.WaitGroup
		workers.Add(2)
		go func() { defer workers.Done(); dispatch(ctx, brokers) }()
		go func() { defer workers.Done(); consume(ctx, brokers) }()
		workers.Wait()
	}()
	return done
}

func wait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func dispatch(ctx context.Context, brokers []string) {
	writer := &kafka.Writer{Addr: kafka.TCP(brokers...), Topic: Topic(), Balancer: &kafka.Hash{},
		RequiredAcks: kafka.RequireAll, Async: false, BatchSize: 1, AllowAutoTopicCreation: true,
		WriteTimeout: 10 * time.Second, ReadTimeout: 10 * time.Second, MaxAttempts: 2}
	defer writer.Close()
	for ctx.Err() == nil {
		event, err := repo.ClaimContentEvent(ctx)
		if err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) && ctx.Err() == nil {
				log.Printf("content outbox claim: %v", err)
			}
			if !wait(ctx, time.Second) {
				return
			}
			continue
		}
		payload, err := json.Marshal(event.ContentEvent)
		if err == nil {
			sendCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			err = writer.WriteMessages(sendCtx, kafka.Message{Key: []byte(fmt.Sprint(event.ActorID)), Value: payload})
			cancel()
		}
		if err != nil && ctx.Err() == nil {
			log.Printf("content outbox %d delivery failed: %v", event.ID, err)
		}
		if finishErr := repo.FinishContentEvent(ctx, event, err); finishErr != nil && ctx.Err() == nil {
			log.Printf("content outbox %d completion failed: %v", event.ID, finishErr)
		}
	}
}

func DecodeEvent(payload []byte) (model.ContentEvent, error) {
	var event model.ContentEvent
	if len(payload) > 65536 {
		return event, errors.New("event exceeds 64 KiB")
	}
	if err := json.Unmarshal(payload, &event); err != nil {
		return event, err
	}
	if _, err := uuid.Parse(event.EventID); err != nil {
		return event, errors.New("invalid event ID")
	}
	if event.EventType != "video.published" && event.EventType != "post.published" {
		return event, errors.New("unsupported event type")
	}
	if event.ActorID == 0 || event.TargetID == 0 || event.PublishedAt.IsZero() || event.PublishedAt.Year() < 1970 || event.PublishedAt.Year() > 9999 || event.PublishedAt.Nanosecond()%1000 != 0 {
		return event, errors.New("invalid event fields")
	}
	if event.BusinessKey != fmt.Sprintf("%s:%d", event.EventType, event.TargetID) {
		return event, errors.New("invalid business key")
	}
	return event, nil
}

func consume(ctx context.Context, brokers []string) {
	reader := kafka.NewReader(kafka.ReaderConfig{Brokers: brokers, Topic: Topic(), GroupID: "following-feed-v1",
		MinBytes: 1, MaxBytes: 1 << 20, MaxWait: time.Second, CommitInterval: 0, StartOffset: kafka.FirstOffset})
	defer reader.Close()
	for ctx.Err() == nil {
		message, err := reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("content feed fetch: %v", err)
			}
			if !wait(ctx, time.Second) {
				return
			}
			continue
		}
		event, decodeErr := DecodeEvent(message.Value)
		// Never fetch/commit later messages while the current database write is unresolved.
		for ctx.Err() == nil {
			if decodeErr == nil {
				err = repo.ProjectContentEvent(ctx, event)
			} else {
				reason := []rune(decodeErr.Error())
				if len(reason) > 1000 {
					reason = reason[:1000]
				}
				payload := strings.ToValidUTF8(string(message.Value[:min(len(message.Value), 4096)]), "?")
				err = dao.SC.Db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&model.FeedDeadLetter{
					Topic: message.Topic, Partition: message.Partition, Offset: message.Offset, Reason: string(reason), Payload: payload,
				}).Error
			}
			if err == nil {
				break
			}
			log.Printf("content feed persistence: %v", err)
			if !wait(ctx, time.Second) {
				return
			}
		}
		for ctx.Err() == nil {
			if err = reader.CommitMessages(ctx, message); err == nil {
				break
			}
			log.Printf("content feed commit: %v", err)
			if !wait(ctx, time.Second) {
				return
			}
		}
	}
}
