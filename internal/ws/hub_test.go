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
	for len(client.send) == 0 {
		select {
		case <-deadline:
			t.Fatal("coalesced online count was not broadcast")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	time.Sleep(15 * time.Millisecond)
	if got := len(client.send); got != 1 {
		t.Fatalf("expected one coalesced online-count frame, got %d", got)
	}

	hub.Stop()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("presence worker did not stop")
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
		if result := client.trySend(data); result != enqueueOK {
			t.Fatalf("trySend() = %v before queue was full", result)
		}
	}
	if result := client.trySend(data); result != enqueueFull {
		t.Fatalf("trySend() = %v, want enqueueFull", result)
	}

	closedClient := NewClient(hub, nil, 1, 2, "tester")
	closedClient.closeSend()
	if result := closedClient.trySend(data); result != enqueueClosed {
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
	if firstPayload == nil || secondPayload == nil || firstPayload != secondPayload {
		t.Fatal("broadcast created a different serialized payload for each client")
	}
	if got := hub.metrics.deliveryEnqueuedTotal.Load(); got != 2 {
		t.Fatalf("delivery enqueued total = %d, want 2", got)
	}
}

func TestQueueFullClosesAndUnregistersClient(t *testing.T) {
	hub := newHub(1)
	client := NewClient(hub, nil, 1, 1, "tester")
	hub.handleRegister(client)
	data, err := websocket.NewPreparedMessage(websocket.TextMessage, []byte(`{"type":"danma"}`))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < cap(client.send); i++ {
		client.send <- data
	}

	hub.broadcastToRoom(&Message{RoomID: 1, Type: "danma", Content: "overflow"})
	if got := hub.metrics.queueFullTotal.Load(); got != 1 {
		t.Fatalf("queue full total = %d, want 1", got)
	}
	if !client.isClosing() {
		t.Fatal("queue-full client connection was not marked closing")
	}
	if got := len(hub.unregister); got != 1 {
		t.Fatalf("unregister signals = %d, want 1", got)
	}
}

func TestBroadcastQueueIsStablePerRoom(t *testing.T) {
	hub := newHub(4)
	for roomID := uint64(1); roomID <= 100; roomID++ {
		first := hub.broadcastQueueIndex(roomID)
		second := hub.broadcastQueueIndex(roomID)
		if first != second {
			t.Fatalf("room %d moved from queue %d to %d", roomID, first, second)
		}
		if first < 0 || first >= len(hub.broadcastQueues) {
			t.Fatalf("room %d mapped outside queue range: %d", roomID, first)
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
