package ws

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestHubRunProcessesMoreThanWorkerCount(t *testing.T) {
	hub := newHub(1)
	runDone := make(chan struct{})
	go func() {
		hub.Run()
		close(runDone)
	}()
	t.Cleanup(func() {
		hub.Stop()
		select {
		case <-runDone:
		case <-time.After(time.Second):
			t.Error("hub did not stop")
		}
	})

	const clientCount = 8
	for i := 0; i < clientCount; i++ {
		hub.register <- NewClient(hub, nil, 1, uint64(i+1), "tester")
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		hub.mu.Lock()
		room := hub.rooms[1]
		count := 0
		if room != nil {
			room.mu.RLock()
			count = len(room.clients)
			room.mu.RUnlock()
		}
		hub.mu.Unlock()
		if count == clientCount {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("hub did not register all %d clients", clientCount)
}

func TestConcurrentBroadcastAndUnregister(t *testing.T) {
	hub := NewHub()
	client := NewClient(hub, nil, 1, 1, "tester")
	hub.handleRegister(client)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 1000; i++ {
			hub.broadcastToRoom(&Message{RoomID: 1})
		}
	}()

	hub.handleUnregister(client)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("broadcast did not finish")
	}

	client.sendMu.RLock()
	closed := client.sendClosed
	client.sendMu.RUnlock()
	if !closed {
		t.Fatal("client send channel was not closed by unregister")
	}
}

func TestPresenceUpdatesAreCoalesced(t *testing.T) {
	hub := NewHub()
	hub.presencePeriod = 10 * time.Millisecond
	client := NewClient(hub, nil, 1, 1, "tester")
	hub.handleRegister(client)
	for i := 0; i < 100; i++ {
		hub.markPresenceDirty(1)
	}

	done := make(chan struct{})
	go func() {
		hub.presenceWorker()
		close(done)
	}()

	deadline := time.After(200 * time.Millisecond)
	for len(client.state) == 0 {
		select {
		case <-deadline:
			t.Fatal("coalesced online count was not broadcast")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	time.Sleep(15 * time.Millisecond)
	if got := len(client.state); got != 1 {
		t.Fatalf("expected one coalesced online-count frame, got %d", got)
	}

	hub.Stop()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("presence worker did not stop")
	}
}

func TestReplaceStateKeepsOnlyLatestValue(t *testing.T) {
	client := NewClient(newHub(1), nil, 1, 1, "tester")
	first, err := websocket.NewPreparedMessage(websocket.TextMessage, []byte(`{"online_count":1}`))
	if err != nil {
		t.Fatal(err)
	}
	latest, err := websocket.NewPreparedMessage(websocket.TextMessage, []byte(`{"online_count":2}`))
	if err != nil {
		t.Fatal(err)
	}

	if result := client.replaceState(first); result != enqueueOK {
		t.Fatalf("first replaceState() = %v, want enqueueOK", result)
	}
	if result := client.replaceState(latest); result != enqueueOK {
		t.Fatalf("second replaceState() = %v, want enqueueOK", result)
	}
	if got := len(client.state); got != 1 {
		t.Fatalf("state queue length = %d, want 1", got)
	}
	if got := <-client.state; got != latest {
		t.Fatal("state queue did not replace the stale value")
	}
}

func TestLikeUpdateBroadcastsCombinedLatestRoomState(t *testing.T) {
	hub := newHub(1)
	client := NewClient(hub, nil, 1, 1, "tester")
	hub.handleRegister(client)

	hub.handleStateUpdate(&Message{
		RoomID: 1,
		Data: map[string]interface{}{
			"sub_type":   "like_count",
			"like_count": float64(42),
		},
	})

	room := hub.rooms[1]
	room.mu.RLock()
	likeCount := room.likeCount
	room.mu.RUnlock()
	if likeCount != 42 {
		t.Fatalf("room like count = %d, want 42", likeCount)
	}
	if got := len(client.state); got != 1 {
		t.Fatalf("combined room-state frames = %d, want 1", got)
	}
}

func TestRegisterInitializesLikeCountFromDurableRoomState(t *testing.T) {
	hub := newHub(1)
	client := NewClient(hub, nil, 7, 1, "tester", 99)
	hub.handleRegister(client)

	room := hub.rooms[7]
	room.mu.RLock()
	likeCount := room.likeCount
	room.mu.RUnlock()
	if likeCount != 99 {
		t.Fatalf("initial room like count=%d, want 99", likeCount)
	}
}

func TestTrySendDistinguishesFullAndClosed(t *testing.T) {
	hub := newHub(1)
	client := NewClient(hub, nil, 1, 1, "tester")
	data, err := websocket.NewPreparedMessage(websocket.TextMessage, []byte(`{"type":"danma"}`))
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < cap(client.send); i++ {
		if result := client.trySendData(data, 0, false, cap(client.send)); result != enqueueOK {
			t.Fatalf("trySend() = %v before queue was full", result)
		}
	}
	if result := client.trySendData(data, 0, false, cap(client.send)); result != enqueueDroppedOldest {
		t.Fatalf("trySendData() = %v, want enqueueDroppedOldest", result)
	}

	closedClient := NewClient(hub, nil, 1, 2, "tester")
	closedClient.closeSend()
	if result := closedClient.trySendData(data, 0, false, cap(closedClient.send)); result != enqueueClosed {
		t.Fatalf("trySend() = %v, want enqueueClosed", result)
	}
}

func TestBroadcastReusesSerializedPayload(t *testing.T) {
	hub := newHub(1)
	first := NewClient(hub, nil, 1, 1, "first")
	second := NewClient(hub, nil, 1, 2, "second")
	hub.handleRegister(first)
	hub.handleRegister(second)

	hub.broadcastToRoom(&Message{RoomID: 1, Type: "danma", Content: "hello"})
	firstPayload := <-first.send
	secondPayload := <-second.send
	if firstPayload.message == nil || secondPayload.message == nil || firstPayload.message != secondPayload.message {
		t.Fatal("broadcast created a different serialized payload for each client")
	}
	if got := hub.metrics.deliveryEnqueuedTotal.Load(); got != 2 {
		t.Fatalf("delivery enqueued total = %d, want 2", got)
	}
}

func TestQueueFullAllowsBurstThenClosesPersistentSlowClient(t *testing.T) {
	hub := newHub(1)
	client := NewClient(hub, nil, 1, 1, "tester")
	hub.handleRegister(client)
	data, err := websocket.NewPreparedMessage(websocket.TextMessage, []byte(`{"type":"danma"}`))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < cap(client.send); i++ {
		client.send <- outboundFrame{message: data}
	}

	hub.broadcastToRoom(&Message{RoomID: 1, Type: "danma", Content: "overflow"})
	if got := hub.metrics.queueFullTotal.Load(); got != 1 {
		t.Fatalf("queue full total = %d, want 1", got)
	}
	if client.isClosing() {
		t.Fatal("a single queue-full event closed the client")
	}
	if got := len(client.control); got != 1 {
		t.Fatalf("resync control messages = %d, want 1", got)
	}
	if got := hub.metrics.resyncRequiredTotal.Load(); got != 1 {
		t.Fatalf("resync required total = %d, want 1", got)
	}

	for i := 1; i < int(hub.adaptive.slowStrikeLimit); i++ {
		hub.broadcastToRoom(&Message{RoomID: 1, Type: "danma", Content: "persistent-overflow"})
	}
	if !client.isClosing() {
		t.Fatal("persistently queue-full client was not marked closing")
	}
	if got := len(hub.unregister); got != 1 {
		t.Fatalf("unregister signals = %d, want 1", got)
	}
}

func TestBroadcastQueueIsStablePerRoomShard(t *testing.T) {
	hub := newHub(32)
	for roomID := uint64(1); roomID <= 100; roomID++ {
		seen := make(map[int]struct{}, fanoutShardCount)
		for shard := 0; shard < fanoutShardCount; shard++ {
			first := hub.broadcastQueueIndex(roomID, shard)
			second := hub.broadcastQueueIndex(roomID, shard)
			if first != second {
				t.Fatalf("room %d shard %d moved from queue %d to %d", roomID, shard, first, second)
			}
			if first < 0 || first >= len(hub.broadcastQueues) {
				t.Fatalf("room %d shard %d mapped outside queue range: %d", roomID, shard, first)
			}
			seen[first] = struct{}{}
		}
		if len(seen) != fanoutShardCount {
			t.Fatalf("room %d uses %d workers, want %d", roomID, len(seen), fanoutShardCount)
		}
	}
}

func TestRoomClientsAreEvenlySharded(t *testing.T) {
	hub := newHub(32)
	const clients = fanoutShardCount * 2
	for i := 0; i < clients; i++ {
		hub.handleRegister(NewClient(hub, nil, 1, uint64(i+1), "tester"))
	}

	room := hub.rooms[1]
	room.mu.RLock()
	defer room.mu.RUnlock()
	for shard, members := range room.shards {
		if len(members) != 2 {
			t.Fatalf("shard %d has %d clients, want 2", shard, len(members))
		}
	}
}

func TestShardedBroadcastPreservesPerClientFIFO(t *testing.T) {
	hub := newHub(32)
	clients := make([]*Client, 0, fanoutShardCount*2)
	for i := 0; i < fanoutShardCount*2; i++ {
		client := NewClient(hub, nil, 1, uint64(i+1), "tester")
		hub.handleRegister(client)
		clients = append(clients, client)
	}
	for _, queue := range hub.broadcastQueues {
		go hub.broadcastWorker(queue)
	}
	t.Cleanup(hub.Stop)

	hub.enqueueBroadcast(Message{RoomID: 1, RoomSeq: 10, Type: "danma", Content: "first"})
	hub.enqueueBroadcast(Message{RoomID: 1, RoomSeq: 11, Type: "danma", Content: "second"})

	var firstFrame, secondFrame *websocket.PreparedMessage
	for index, client := range clients {
		var first, second *websocket.PreparedMessage
		select {
		case frame := <-client.send:
			first = frame.message
		case <-time.After(time.Second):
			t.Fatalf("client %d did not receive first frame", index)
		}
		select {
		case frame := <-client.send:
			second = frame.message
		case <-time.After(time.Second):
			t.Fatalf("client %d did not receive second frame", index)
		}
		if first == second {
			t.Fatalf("client %d received the same frame twice", index)
		}
		if firstFrame == nil {
			firstFrame, secondFrame = first, second
			continue
		}
		if first != firstFrame || second != secondFrame {
			t.Fatalf("client %d observed different frame ordering", index)
		}
	}
}

func TestMetricsHandler(t *testing.T) {
	hub := newHub(1)
	client := NewClient(hub, nil, 1, 1, "tester")
	hub.handleRegister(client)
	hub.broadcastToRoom(&Message{RoomID: 1, Type: "danma", Content: "hello"})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/debug/ws/metrics", nil)
	hub.MetricsHandler().ServeHTTP(recorder, request)
	if recorder.Code != 200 {
		t.Fatalf("metrics status = %d, want 200", recorder.Code)
	}
	var snapshot MetricsSnapshot
	if err := json.Unmarshal(recorder.Body.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode metrics: %v", err)
	}
	if snapshot.Connections != 1 || snapshot.BroadcastMessagesTotal != 1 || snapshot.ClientQueueDepth != 1 {
		t.Fatalf("unexpected metrics snapshot: %+v", snapshot)
	}
	if snapshot.BroadcastDuration.Count != 1 {
		t.Fatalf("broadcast duration count = %d, want 1", snapshot.BroadcastDuration.Count)
	}
}

func TestClientAcknowledgementOnlyMovesForward(t *testing.T) {
	client := NewClient(newHub(1), nil, 1, 1, "tester")
	client.acknowledge(10)
	client.acknowledge(7)
	client.acknowledge(10)
	if got := client.lastAck.Load(); got != 10 {
		t.Fatalf("last ack = %d, want 10", got)
	}
	client.acknowledge(11)
	if got := client.lastAck.Load(); got != 11 {
		t.Fatalf("last ack = %d, want 11", got)
	}
}

func TestHotRoomPolicySamplesOnlyAfterFanoutBudgetIsExceeded(t *testing.T) {
	hub := newHub(4)
	hub.adaptive.hotOnlineThreshold = 1000
	hub.adaptive.maxFanoutPerSecond = 10
	hub.adaptive.maxSampleStride = 16
	for i := 0; i < 10; i++ {
		hub.handleRegister(NewClient(hub, nil, 7, uint64(i+1), "tester"))
	}

	startedAt := time.Now()
	deliver, policy := hub.planDanmaDelivery(Message{RoomID: 7, RoomSeq: 1}, startedAt)
	if !deliver || policy != nil {
		t.Fatalf("first event deliver=%v policy=%+v, want full push without transition", deliver, policy)
	}
	deliver, policy = hub.planDanmaDelivery(
		Message{RoomID: 7, RoomSeq: 2},
		startedAt.Add(500*time.Millisecond),
	)
	if policy == nil || policy.sampleStride <= 1 || !policy.hot {
		t.Fatalf("fanout overload did not enter sampled hot mode: %+v", policy)
	}
	if deliver {
		t.Fatal("sampled event unexpectedly selected for delivery")
	}
}

func TestHotRoomDropsOldestAtHighWatermark(t *testing.T) {
	hub := newHub(1)
	hub.adaptive.queueHighWatermark = 2
	client := NewClient(hub, nil, 9, 1, "tester")
	hub.handleRegister(client)
	frame, err := websocket.NewPreparedMessage(websocket.TextMessage, []byte(`{"type":"danma"}`))
	if err != nil {
		t.Fatal(err)
	}
	client.send <- outboundFrame{message: frame, roomSeq: 1}
	client.send <- outboundFrame{message: frame, roomSeq: 2}

	result := client.trySendData(frame, 3, true, hub.adaptive.queueHighWatermark)
	if result != enqueueDroppedOldest {
		t.Fatalf("trySendData()=%v, want enqueueDroppedOldest", result)
	}
	first := <-client.send
	second := <-client.send
	if first.roomSeq != 2 || second.roomSeq != 3 {
		t.Fatalf("queue sequences=%d,%d, want 2,3", first.roomSeq, second.roomSeq)
	}
}
