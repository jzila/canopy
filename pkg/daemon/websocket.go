package daemon

import (
	"encoding/json"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jzila/canopy/pkg/logging"
)

const (
	// Time allowed to write a message to the peer
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer
	maxMessageSize = 512

	// Default buffer size for hub broadcast channel
	defaultHubBroadcastBuffer = 256

	// Default buffer size for client send channel
	defaultClientSendBuffer = 256
)

// Hub maintains the set of active clients and broadcasts messages to them
type Hub struct {
	// Registered clients
	clients map[*Client]bool

	// Inbound messages from clients (currently unused, but reserved for future client->server messages)
	broadcast chan []byte

	// Register requests from clients
	register chan *Client

	// Unregister requests from clients
	unregister chan *Client

	// Event bus for subscribing to internal events
	eventBus *EventBus

	// Unsubscribe function for event bus cleanup
	unsubscribe func()

	// Backpressure metrics
	droppedBroadcastEvents int64
	droppedClientMessages  int64
	disconnectedSlowClients int64
}

// Client represents a single WebSocket connection
type Client struct {
	// The hub this client belongs to
	hub *Hub

	// The WebSocket connection
	conn *websocket.Conn

	// Buffered channel of outbound messages
	send chan []byte
}

// NewHub creates a new Hub instance
func NewHub(eventBus *EventBus) *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte, defaultHubBroadcastBuffer),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		eventBus:   eventBus,
	}
}

// Run starts the hub's main event loop
// This should be run in a goroutine
func (h *Hub) Run() {
	// Subscribe to EventBus and forward events to broadcast channel
	h.unsubscribe = h.eventBus.Subscribe(func(event Event) {
		// Marshal event to JSON
		data, err := json.Marshal(event)
		if err != nil {
			logging.Error("error marshaling event", "error", err, "event_type", event.Type)
			return
		}
		// Send to broadcast channel (non-blocking)
		select {
		case h.broadcast <- data:
		default:
			// Drop event if broadcast channel is full
			h.droppedBroadcastEvents++
			logging.Warn("dropped event due to full broadcast channel",
				"event_type", event.Type,
				"total_dropped", h.droppedBroadcastEvents)
		}
	})

	// Main event loop
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true

		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}

		case message := <-h.broadcast:
			// Broadcast to all connected clients
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					// Client's send buffer is full, close the connection
					h.droppedClientMessages++
					h.disconnectedSlowClients++
					logging.Warn("disconnecting slow client due to full send buffer",
						"dropped_messages", h.droppedClientMessages,
						"disconnected_clients", h.disconnectedSlowClients)
					close(client.send)
					delete(h.clients, client)
				}
			}
		}
	}
}

// Shutdown cleanly shuts down the hub
func (h *Hub) Shutdown() {
	if h.unsubscribe != nil {
		h.unsubscribe()
	}
}

// HubMetrics contains backpressure metrics for the hub
type HubMetrics struct {
	DroppedBroadcastEvents  int64
	DroppedClientMessages   int64
	DisconnectedSlowClients int64
	ActiveClients           int
}

// GetMetrics returns the current backpressure metrics
// Note: This method is not thread-safe and should only be called
// for monitoring/debugging purposes
func (h *Hub) GetMetrics() HubMetrics {
	return HubMetrics{
		DroppedBroadcastEvents:  h.droppedBroadcastEvents,
		DroppedClientMessages:   h.droppedClientMessages,
		DisconnectedSlowClients: h.disconnectedSlowClients,
		ActiveClients:           len(h.clients),
	}
}

// ReadPump pumps messages from the WebSocket connection to the hub
//
// The application runs ReadPump in a per-connection goroutine. The application
// ensures that there is at most one reader on a connection by executing all
// reads from this goroutine.
func (c *Client) ReadPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logging.Warn("WebSocket error", "error", err)
			}
			break
		}

		// Currently, we don't process messages from clients
		// This is reserved for future client->server communication
		_ = message
	}
}

// WritePump pumps messages from the hub to the WebSocket connection
//
// A goroutine running WritePump is started for each connection. The
// application ensures that there is at most one writer to a connection by
// executing all writes from this goroutine.
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued messages to the current WebSocket message
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
