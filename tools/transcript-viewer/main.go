// Transcript Viewer - Real-time transcription display
// Consumes from Kafka topics and displays via WebSocket to browser
package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/segmentio/kafka-go"
)

//go:embed static/*
var staticFiles embed.FS

// TranscriptEvent represents a transcript message from Kafka
type TranscriptEvent struct {
	EventType     string  `json:"eventType"`
	InteractionID string  `json:"interactionId"`
	TenantID      string  `json:"tenantId"`
	SegmentID     string  `json:"segmentId"`
	Text          string  `json:"text"`
	Confidence    float64 `json:"confidence,omitempty"`
	AudioOffsetMs int64   `json:"audioOffsetMs,omitempty"`
	Timestamp     int64   `json:"timestamp"`
}

// Hub manages WebSocket connections
type Hub struct {
	clients    map[*websocket.Conn]bool
	broadcast  chan TranscriptEvent
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	mu         sync.RWMutex
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func newHub() *Hub {
	return &Hub{
		clients:    make(map[*websocket.Conn]bool),
		broadcast:  make(chan TranscriptEvent, 100),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
	}
}

func (h *Hub) run() {
	for {
		select {
		case conn := <-h.register:
			h.mu.Lock()
			h.clients[conn] = true
			h.mu.Unlock()
			log.Printf("Client connected. Total: %d", len(h.clients))

		case conn := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[conn]; ok {
				delete(h.clients, conn)
				conn.Close()
			}
			h.mu.Unlock()
			log.Printf("Client disconnected. Total: %d", len(h.clients))

		case event := <-h.broadcast:
			h.mu.RLock()
			for conn := range h.clients {
				err := conn.WriteJSON(event)
				if err != nil {
					log.Printf("Write error: %v", err)
					conn.Close()
					delete(h.clients, conn)
				}
			}
			h.mu.RUnlock()
		}
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for local dev
	},
}

func wsHandler(hub *Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket upgrade error: %v", err)
			return
		}
		hub.register <- conn

		// Keep connection alive, handle disconnects
		go func() {
			defer func() {
				hub.unregister <- conn
			}()
			for {
				_, _, err := conn.ReadMessage()
				if err != nil {
					break
				}
			}
		}()
	}
}

func consumeKafka(ctx context.Context, hub *Hub, brokers, topic string) {
	// Use partition reader without consumer group (works better through port-forward)
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:   strings.Split(brokers, ","),
		Topic:     topic,
		Partition: 0, // Read from partition 0 only (simplest for demo)
		MinBytes:  1,
		MaxBytes:  10e6,
	})
	defer reader.Close()

	// Start from the latest offset (only show new messages)
	reader.SetOffsetAt(ctx, time.Now().Add(-1*time.Hour)) // Last hour of messages

	log.Printf("Consuming from Kafka topic: %s partition 0 (last hour)", topic)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			msg, err := reader.ReadMessage(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				log.Printf("Kafka read error on %s: %v", topic, err)
				time.Sleep(time.Second)
				continue
			}

			var event TranscriptEvent
			if err := json.Unmarshal(msg.Value, &event); err != nil {
				log.Printf("JSON unmarshal error: %v", err)
				continue
			}

			log.Printf("📨 Received %s: %s (segment: %s)", event.EventType, truncate(event.Text, 40), event.SegmentID)
			hub.broadcast <- event
		}
	}
}

func main() {
	port := flag.String("port", "8081", "HTTP server port")
	brokers := flag.String("brokers", "localhost:9092", "Kafka brokers (comma-separated)")
	topicPartial := flag.String("topic-partial", "interaction.transcript.partial", "Partial transcript topic")
	topicFinal := flag.String("topic-final", "interaction.transcript.final", "Final transcript topic")
	flag.Parse()

	hub := newHub()
	go hub.run()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start Kafka consumers
	go consumeKafka(ctx, hub, *brokers, *topicPartial)
	go consumeKafka(ctx, hub, *brokers, *topicFinal)

	// Serve static files
	staticFS, _ := fs.Sub(staticFiles, "static")
	http.Handle("/", http.FileServer(http.FS(staticFS)))

	// WebSocket endpoint
	http.HandleFunc("/ws", wsHandler(hub))

	log.Printf("🎙️  Transcript Viewer starting on http://localhost:%s", *port)
	log.Printf("   Kafka brokers: %s", *brokers)
	log.Printf("   Topics: %s, %s", *topicPartial, *topicFinal)

	if err := http.ListenAndServe(":"+*port, nil); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

