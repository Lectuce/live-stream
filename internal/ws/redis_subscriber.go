package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"live-stream/internal/dao"
	"live-stream/internal/model"
	"log"
)

func StartRedisSubscriber(ctx context.Context, hub *Hub) error {
	pubSub, err := dao.Subscribe(ctx, dao.DanmuChannel)
	if err != nil {
		return fmt.Errorf("subscribe redis channel: %w", err)
	}
	defer pubSub.Close()

	log.Printf("redis subscriber started, channel=%s", dao.DanmuChannel)

	messageChan := pubSub.Channel()

	for {
		select {
		case <-ctx.Done():
			return nil

		case redisMessage, ok := <-messageChan:
			if !ok {
				return fmt.Errorf("redis subscription channel closed")
			}

			var message Message
			if err := json.Unmarshal(
				[]byte(redisMessage.Payload),
				&message,
			); err != nil {
				log.Printf(
					"unmarshal redis message failed, payload=%s, err=%v",
					redisMessage.Payload,
					err,
				)
				continue
			}

			if message.Type == model.MsgTypeSystem {
				hub.handleStateUpdate(&message)
				continue
			}
			hub.enqueueBroadcast(message)
		}
	}
}
