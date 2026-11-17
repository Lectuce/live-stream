package app

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestListenServersRollsBackOnFailure(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	_ = probe.Close()
	// The second listener conflicts with the first one.
	_, err = listenServers([]*http.Server{{Addr: addr}, {Addr: addr}})
	if err == nil {
		t.Fatal("expected a port conflict")
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("first listener was not released: %v", err)
	}
	_ = listener.Close()
}

func TestShutdownServersDrainsRequest(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	server := &http.Server{Addr: "127.0.0.1:0", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		_, _ = io.WriteString(w, "completed")
	})}
	listeners, err := listenServers([]*http.Server{server})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	go server.Serve(listeners[0])
	clientDone := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get("http://" + listeners[0].Addr().String())
		if err == nil {
			defer resp.Body.Close()
			var body []byte
			body, err = io.ReadAll(resp.Body)
			if err == nil && string(body) != "completed" {
				err = errors.New("in-flight response was lost")
			}
		}
		clientDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("request did not enter handler")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- shutdownServers(ctx, []*http.Server{server}) }()
	select {
	case err := <-stopped:
		close(release)
		t.Fatalf("shutdown returned while request was active: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	if err := <-clientDone; err != nil {
		t.Fatal(err)
	}
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
}

func TestWaitWorkersHonorsDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitWorkers(ctx, make(chan struct{})); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	done := make(chan struct{})
	close(done)
	if err := waitWorkers(context.Background(), done); err != nil {
		t.Fatal(err)
	}
}
