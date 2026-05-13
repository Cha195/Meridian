package api

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"

	"github.com/cha195/meridian/internal/events"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type WebSocketBroadcaster struct {
	clients map[*websocket.Conn]chan []byte
	mu      sync.Mutex
}

func NewWebSocketBroadcaster() *WebSocketBroadcaster {
	return &WebSocketBroadcaster{
		clients: make(map[*websocket.Conn]chan []byte),
	}
}

func (b *WebSocketBroadcaster) Write(event events.DiagnosticEvent) error {
	b.mu.Lock()
	if len(b.clients) == 0 {
		b.mu.Unlock()
		return nil
	}

	data, err := json.Marshal(event)
	if err != nil {
		b.mu.Unlock()
		return err
	}

	for _, ch := range b.clients {
		select {
		case ch <- data:
		default:
		}
	}
	b.mu.Unlock()
	return nil
}

func (b *WebSocketBroadcaster) Flush() error { return nil }
func (b *WebSocketBroadcaster) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for conn, ch := range b.clients {
		close(ch)
		conn.Close()
	}
	b.clients = make(map[*websocket.Conn]chan []byte)
	return nil
}

func (b *WebSocketBroadcaster) addClient(conn *websocket.Conn) chan []byte {
	ch := make(chan []byte, 64)
	b.mu.Lock()
	b.clients[conn] = ch
	b.mu.Unlock()
	return ch
}

func (b *WebSocketBroadcaster) removeClient(conn *websocket.Conn) {
	b.mu.Lock()
	if ch, ok := b.clients[conn]; ok {
		close(ch)
		delete(b.clients, conn)
	}
	b.mu.Unlock()
	conn.Close()
}

func (s *APIServer) handleEventsStream(w http.ResponseWriter, r *http.Request) {
	if s.wsBroadcaster == nil {
		http.Error(w, "websocket not available", http.StatusServiceUnavailable)
		return
	}

	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "missing key parameter", http.StatusUnauthorized)
		return
	}

	valid := false
	for _, p := range s.config.Projects {
		if p.APIKey == key {
			valid = true
			break
		}
	}
	if !valid {
		http.Error(w, "invalid api key", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("websocket upgrade failed: %v", err)
		return
	}

	ch := s.wsBroadcaster.addClient(conn)

	go func() {
		for {
			if _, _, err := conn.NextReader(); err != nil {
				break
			}
		}
		s.wsBroadcaster.removeClient(conn)
	}()

	go func() {
		for data := range ch {
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				s.wsBroadcaster.removeClient(conn)
				return
			}
		}
	}()
}
