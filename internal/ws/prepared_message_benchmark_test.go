package ws

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func BenchmarkWebSocketFanoutFrame(b *testing.B) {
	const connections = 32
	payload := bytes.Repeat([]byte("benchmark-frame-"), 64)

	b.Run("WriteMessage", func(b *testing.B) {
		serverConnections, cleanup := benchmarkConnections(b, connections)
		defer cleanup()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, connection := range serverConnections {
				_ = connection.SetWriteDeadline(time.Now().Add(writeWait))
				if err := connection.WriteMessage(websocket.TextMessage, payload); err != nil {
					b.Fatal(err)
				}
			}
		}
	})

	b.Run("WritePreparedMessage", func(b *testing.B) {
		serverConnections, cleanup := benchmarkConnections(b, connections)
		defer cleanup()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			frame, err := websocket.NewPreparedMessage(websocket.TextMessage, payload)
			if err != nil {
				b.Fatal(err)
			}
			for _, connection := range serverConnections {
				_ = connection.SetWriteDeadline(time.Now().Add(writeWait))
				if err := connection.WritePreparedMessage(frame); err != nil {
					b.Fatal(err)
				}
			}
		}
	})
}

func benchmarkConnections(b *testing.B, count int) ([]*websocket.Conn, func()) {
	b.Helper()
	type upgradeResult struct {
		connection *websocket.Conn
		err        error
	}
	results := make(chan upgradeResult, count)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := upgrader.Upgrade(writer, request, nil)
		results <- upgradeResult{connection: connection, err: err}
	}))

	serverConnections := make([]*websocket.Conn, 0, count)
	clientConnections := make([]*websocket.Conn, 0, count)
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http")
	for i := 0; i < count; i++ {
		clientConnection, _, err := websocket.DefaultDialer.Dial(endpoint, nil)
		if err != nil {
			b.Fatal(err)
		}
		result := <-results
		if result.err != nil {
			b.Fatal(result.err)
		}
		serverConnections = append(serverConnections, result.connection)
		clientConnections = append(clientConnections, clientConnection)
		go func(connection *websocket.Conn) {
			for {
				if _, _, err := connection.ReadMessage(); err != nil {
					return
				}
			}
		}(clientConnection)
	}

	cleanup := func() {
		for _, connection := range serverConnections {
			_ = connection.Close()
		}
		for _, connection := range clientConnections {
			_ = connection.Close()
		}
		server.Close()
	}
	return serverConnections, cleanup
}
