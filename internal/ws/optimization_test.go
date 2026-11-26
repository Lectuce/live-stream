package ws

import (
	"testing"
	"time"
)

func TestSnapshotReuseAndRemoval(t *testing.T) {
	h := newHub(1)
	c := NewClient(h, nil, 1, 1, "test")
	h.handleRegister(c)
	r := h.lookupRoom(1)
	first := r.snapshotShard(c.fanoutShard)
	second := r.snapshotShard(c.fanoutShard)
	if len(first) != 1 || &first[0] != &second[0] {
		t.Fatal("snapshot not reused")
	}
	h.handleUnregister(c)
	if len(r.snapshotShard(c.fanoutShard)) != 0 || len(first) != 1 {
		t.Fatal("old snapshot mutated or removal missing")
	}
	h.handleRegister(NewClient(h, nil, 1, 2, "new"))
	h.broadcastRoomShard(r, c.fanoutShard, nil, 1, false)
	if h.metrics.deliveryEnqueuedTotal.Load() != 0 {
		t.Fatal("old room delivered to replacement")
	}
}

func TestCloseQueueOverflowDoesNotBlockFanout(t *testing.T) {
	h := newHub(1)
	for i := 0; i < cap(h.closeQueue)+10; i++ {
		c := NewClient(h, nil, 1, uint64(i), "slow")
		h.requestClientClose(c, 4001, "slow")
		if !c.isClosing() || c.trySendControl(nil) != enqueueClosed {
			t.Fatal("closing client accepted work")
		}
	}
	if len(h.closeQueue) != cap(h.closeQueue) {
		t.Fatal("unbounded close queue")
	}
	healthy := NewClient(h, nil, 1, 999, "healthy")
	h.handleRegister(healthy)
	h.broadcastToRoom(&Message{RoomID: 1, Type: "danma", RoomSeq: 1})
	if len(healthy.send) != 1 {
		t.Fatal("close overload blocked healthy client")
	}
}

func TestStopCleansConnectionsAndWorkers(t *testing.T) {
	h := newHub(1)
	c := NewClient(h, nil, 1, 1, "test")
	h.handleRegister(c)
	done := make(chan struct{})
	go func() { h.Run(); close(done) }()
	// Ensure Run has started without relying on a sleep.
	h.register <- NewClient(h, nil, 2, 2, "other")
	deadline := time.Now().Add(time.Second)
	for h.lookupRoom(2) == nil {
		if time.Now().After(deadline) {
			t.Fatal("registration stalled")
		}
		time.Sleep(time.Millisecond)
	}

	h.Stop()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not exit")
	}
	if !c.isClosing() || h.metrics.connections.Load() != 0 {
		t.Fatal("connections leaked")
	}
}

func BenchmarkFanoutSnapshot(b *testing.B) {
	h := newHub(4)
	clients := make([]*Client, 8000)
	for i := range clients {
		clients[i] = NewClient(h, nil, 1, uint64(i), "viewer")
		h.handleRegister(clients[i])
	}
	r := h.lookupRoom(1)
	frame, _ := prepareMessage(&Message{RoomID: 1, Type: "danma", Content: "test"})
	for shard := 0; shard < fanoutShardCount; shard++ {
		r.snapshotShard(shard)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for shard := 0; shard < fanoutShardCount; shard++ {
			h.broadcastRoomShard(r, shard, frame, uint64(i+1), false)
		}
		for _, c := range clients {
			<-c.send
		}
	}
}
