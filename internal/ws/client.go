package ws

import (
	"live-stream/internal/config"
	"live-stream/internal/dao"
	"live-stream/internal/kafka"
	"live-stream/internal/model"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/gorilla/websocket"
)

const (
	writeWait         = 10 * time.Second
	pongWait          = 60 * time.Second
	pingPeriod        = pongWait * 9 / 10
	clientQueueSize   = 256
	controlQueueSize  = 8
	gapNoticeInterval = 5 * time.Second
)

var nextConnectionID atomic.Uint64

type enqueueResult uint8

const (
	enqueueOK enqueueResult = iota
	enqueueClosed
	enqueueFull
	enqueueDroppedOldest
)

type outboundFrame struct {
	message *websocket.PreparedMessage
	roomSeq uint64
}

type Client struct {
	hub              *Hub
	conn             *websocket.Conn
	RoomID           uint64                          // 当前客户端所在的房间
	send             chan outboundFrame              // 下发给前端的消息队列
	control          chan *websocket.PreparedMessage // accepted/error/resync 等高优先级控制消息
	state            chan *websocket.PreparedMessage // 在线人数等可覆盖的最新状态
	UserID           uint64                          // 当前客户端的用户id
	UserName         string
	initialLikeCount int64
	connectionID     uint64
	fanoutShard      int

	sendMu          sync.RWMutex
	sendClosed      bool
	unregisterOnce  sync.Once
	closeOnce       sync.Once
	closing         atomic.Bool
	lastAck         atomic.Uint64
	queueFullStreak atomic.Uint32
	queueFullSince  atomic.Int64
	lastGapNotice   atomic.Int64
	lastWrittenSeq  atomic.Uint64
}

func NewClient(
	hub *Hub,
	conn *websocket.Conn,
	roomID uint64,
	userID uint64,
	userName string,
	initialLikeCount ...int64,
) *Client {
	connectionID := nextConnectionID.Add(1)
	likeCount := int64(0)
	if len(initialLikeCount) > 0 {
		likeCount = initialLikeCount[0]
	}
	return &Client{
		hub:              hub,
		conn:             conn,
		RoomID:           roomID,
		UserID:           userID,
		UserName:         userName,
		initialLikeCount: likeCount,
		connectionID:     connectionID,
		fanoutShard:      int(connectionID % fanoutShardCount),
		send:             make(chan outboundFrame, clientQueueSize),
		control:          make(chan *websocket.PreparedMessage, controlQueueSize),
		state:            make(chan *websocket.PreparedMessage, 1),
	}
}

func (c *Client) trySendData(
	message *websocket.PreparedMessage,
	roomSeq uint64,
	preferLatest bool,
	highWatermark int,
) enqueueResult {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	if c.sendClosed {
		return enqueueClosed
	}
	frame := outboundFrame{message: message, roomSeq: roomSeq}
	if preferLatest && highWatermark > 0 && len(c.send) >= highWatermark {
		select {
		case <-c.send:
		default:
		}
		select {
		case c.send <- frame:
			return enqueueDroppedOldest
		default:
			return enqueueFull
		}
	}
	select {
	case c.send <- frame:
		if len(c.send) < cap(c.send)/2 && c.queueFullStreak.Load() != 0 {
			c.queueFullStreak.Store(0)
			c.queueFullSince.Store(0)
		}
		return enqueueOK
	default:
		// The queue contains stale realtime events. Drop the oldest one so the
		// client remains close to live, then ask it to replay the missing range.
		select {
		case <-c.send:
		default:
			return enqueueFull
		}
		select {
		case c.send <- frame:
			return enqueueDroppedOldest
		default:
			return enqueueFull
		}
	}
}

func (c *Client) trySendControl(message *websocket.PreparedMessage) enqueueResult {
	c.sendMu.RLock()
	defer c.sendMu.RUnlock()
	if c.sendClosed {
		return enqueueClosed
	}
	select {
	case c.control <- message:
		return enqueueOK
	default:
		return enqueueFull
	}
}

func (c *Client) replaceState(message *websocket.PreparedMessage) enqueueResult {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	if c.sendClosed {
		return enqueueClosed
	}
	select {
	case c.state <- message:
		return enqueueOK
	default:
	}
	// State is replaceable: discard the stale value and keep only the newest.
	select {
	case <-c.state:
	default:
	}
	c.state <- message
	return enqueueOK
}

// recordQueueFull distinguishes a short burst from a persistently slow
// connection. The first dropped live event emits one resync hint; only a
// sustained high-water mark closes the socket.
func (c *Client) recordQueuePressure(
	now time.Time,
	strikeLimit uint32,
	grace time.Duration,
) (notifyGap, disconnect bool) {
	nowNanos := now.UnixNano()
	if c.queueFullSince.Load() == 0 {
		c.queueFullSince.CompareAndSwap(0, nowNanos)
	}
	streak := c.queueFullStreak.Add(1)
	for {
		lastNotice := c.lastGapNotice.Load()
		if lastNotice != 0 && nowNanos-lastNotice < int64(gapNoticeInterval) {
			break
		}
		if c.lastGapNotice.CompareAndSwap(lastNotice, nowNanos) {
			notifyGap = true
			break
		}
	}
	since := c.queueFullSince.Load()
	disconnect = streak >= strikeLimit || (since > 0 && nowNanos-since >= int64(grace))
	return notifyGap, disconnect
}

func (c *Client) closeSend() {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	if c.sendClosed {
		return
	}
	c.sendClosed = true
	close(c.send)
	close(c.control)
	close(c.state)
}

func (c *Client) signalUnregister() {
	c.unregisterOnce.Do(func() {
		select {
		case c.hub.unregister <- c:
		case <-c.hub.ctx.Done():
		}
	})
}

func (c *Client) closeConnection() {
	c.closeConnectionWithReason(0, "")
}

func (c *Client) closeConnectionWithReason(code int, reason string) {
	c.closeOnce.Do(func() {
		c.closing.Store(true)
		if c.conn != nil {
			if code != 0 {
				_ = c.conn.WriteControl(
					websocket.CloseMessage,
					websocket.FormatCloseMessage(code, reason),
					time.Now().Add(time.Second),
				)
			}
			_ = c.conn.Close()
		}
	})
}

func (c *Client) isClosing() bool {
	return c.closing.Load()
}

func (c *Client) acknowledge(sequence uint64) {
	for {
		previous := c.lastAck.Load()
		if sequence <= previous || c.lastAck.CompareAndSwap(previous, sequence) {
			return
		}
	}
}

func (c *Client) readPump() {
	defer func() {
		c.closeConnection()
		c.signalUnregister()
	}()

	if err := c.conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		return
	}
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		var msg Message
		err := c.conn.ReadJSON(&msg)
		if err != nil {
			break
		}

		if msg.Type == model.MsgTypeAck {
			if msg.RoomID != 0 && msg.RoomID != c.RoomID {
				c.hub.enqueueJSON(c, map[string]interface{}{
					"type": "error",
					"code": http.StatusBadRequest,
					"msg":  "ACK room_id 与当前房间不一致",
				})
				continue
			}
			c.acknowledge(msg.AckSeq)
			c.hub.metrics.ackTotal.Add(1)
			continue
		}

		// 匿名用户禁止发送弹幕，直接拦截并返回提示
		if c.UserID == 0 {
			c.hub.enqueueJSON(c, map[string]interface{}{
				"type": "error",
				"code": http.StatusUnauthorized,
				"msg":  "登录后才能发送弹幕",
			})
			continue
		}

		if msg.MessageID == "" {
			msg.MessageID = uuid.NewString()
		} else {
			parsedMessageID, parseErr := uuid.Parse(msg.MessageID)
			if parseErr != nil {
				c.hub.enqueueJSON(c, map[string]interface{}{
					"type": "error",
					"code": http.StatusBadRequest,
					"msg":  "message_id 必须是 UUID",
				})
				continue
			}
			// Canonicalize equivalent UUID spellings so they cannot bypass the
			// durable message_id uniqueness constraint.
			msg.MessageID = parsedMessageID.String()
		}
		msg.UserID = c.UserID
		msg.RoomID = c.RoomID
		msg.UserName = c.UserName

		if msg.Type == "" {
			msg.Type = model.MsgTypeDanma
		}
		if msg.Type == model.MsgTypeLike {
			maxPerSecond := 10
			if config.GlobalConfig != nil && config.GlobalConfig.Danmaku.Like.MaxPerSecond > 0 {
				maxPerSecond = config.GlobalConfig.Danmaku.Like.MaxPerSecond
			}
			allowed, limitErr := dao.AllowRoomLike(c.hub.ctx, c.RoomID, c.UserID, maxPerSecond)
			if limitErr != nil {
				c.hub.enqueueJSON(c, map[string]interface{}{
					"type":       "error",
					"code":       http.StatusServiceUnavailable,
					"message_id": msg.MessageID,
					"msg":        "点赞服务暂时不可用",
				})
				continue
			}
			if !allowed {
				c.hub.enqueueJSON(c, map[string]interface{}{
					"type":       "error",
					"code":       http.StatusTooManyRequests,
					"message_id": msg.MessageID,
					"msg":        "点赞过于频繁，请稍后再试",
				})
				continue
			}
		}

		err = kafka.SendDanmaMsg(c.hub.ctx, msg)
		if err != nil {
			log.Printf("produce danma to kafka failed: %v", err)
			c.hub.enqueueJSON(c, map[string]interface{}{
				"type":       "error",
				"code":       http.StatusServiceUnavailable,
				"message_id": msg.MessageID,
				"msg":        "弹幕暂时发送失败，请使用相同 message_id 重试",
			})
			continue
		}
		c.hub.enqueueJSON(c, map[string]interface{}{
			"type":       model.MsgTypeAccepted,
			"message_id": msg.MessageID,
		})
	}
}

func (c *Client) writePump() {
	defer func() {
		c.closeConnection()
		c.signalUnregister()
	}()
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	dataChannel := c.send
	controlChannel := c.control
	stateChannel := c.state

	writePrepared := func(message *websocket.PreparedMessage) bool {
		if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
			return false
		}
		return c.conn.WritePreparedMessage(message) == nil
	}

	for dataChannel != nil || controlChannel != nil || stateChannel != nil {
		// Control messages must not wait behind hundreds of stale danmaku.
		if controlChannel != nil {
			select {
			case message, ok := <-controlChannel:
				if !ok {
					controlChannel = nil
					continue
				}
				if !writePrepared(message) {
					if !c.isClosing() {
						c.hub.metrics.writeErrorsTotal.Add(1)
					}
					return
				}
				continue
			default:
			}
		}
		if stateChannel != nil {
			select {
			case message, ok := <-stateChannel:
				if !ok {
					stateChannel = nil
					continue
				}
				if !writePrepared(message) {
					if !c.isClosing() {
						c.hub.metrics.writeErrorsTotal.Add(1)
					}
					return
				}
				continue
			default:
			}
		}

		select {
		case message, ok := <-controlChannel:
			if !ok {
				controlChannel = nil
				continue
			}
			if !writePrepared(message) {
				if !c.isClosing() {
					c.hub.metrics.writeErrorsTotal.Add(1)
				}
				return
			}
		case frame, ok := <-dataChannel:
			if !ok {
				dataChannel = nil
				continue
			}
			if !writePrepared(frame.message) {
				if !c.isClosing() {
					c.hub.metrics.writeErrorsTotal.Add(1)
				}
				return
			}
			if frame.roomSeq > 0 {
				c.lastWrittenSeq.Store(frame.roomSeq)
			}
		case message, ok := <-stateChannel:
			if !ok {
				stateChannel = nil
				continue
			}
			if !writePrepared(message) {
				if !c.isClosing() {
					c.hub.metrics.writeErrorsTotal.Add(1)
				}
				return
			}
		case <-ticker.C:
			if err := c.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait)); err != nil {
				if !c.isClosing() {
					c.hub.metrics.writeErrorsTotal.Add(1)
				}
				return
			}
		}
	}
}
