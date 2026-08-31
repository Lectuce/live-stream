package ws

import (
	"testing"
	"time"
)

func TestHubRunProcessesMoreThanWorkerCount(t *testing.T) {
	hub := NewHub()
	hub.workerNum = 1
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
