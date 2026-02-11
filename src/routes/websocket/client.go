package websocket

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hindsightchat/backend/src/types"
	uuid "github.com/satori/go.uuid"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 512 * 1024
)

type Client struct {
	hub       *Hub
	conn      *websocket.Conn
	send      chan []byte
	sessionID string

	userID     uuid.UUID
	user       *UserBrief
	identified bool

	// presence
	status   string
	activity *types.Activity

	// focus tracking
	focusedChannel      *uuid.UUID
	focusedServer       *uuid.UUID
	focusedConversation *uuid.UUID

	// subscriptions
	servers       map[uuid.UUID]bool
	conversations map[uuid.UUID]bool
	mu            sync.RWMutex
}

func NewClient(hub *Hub, conn *websocket.Conn) *Client {
	return &Client{
		hub:           hub,
		conn:          conn,
		send:          make(chan []byte, 256),
		sessionID:     uuid.NewV4().String(),
		servers:       make(map[uuid.UUID]bool),
		conversations: make(map[uuid.UUID]bool),
		status:        "online",
	}
}

func (c *Client) SessionID() string {
	return c.sessionID
}

func (c *Client) UserID() uuid.UUID {
	return c.userID
}

func (c *Client) User() *UserBrief {
	return c.user
}

func (c *Client) IsIdentified() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.identified
}

func (c *Client) Status() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

func (c *Client) Activity() *types.Activity {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.activity
}

func (c *Client) SetStatus(status string) {
	c.mu.Lock()
	c.status = status
	c.mu.Unlock()
}

func (c *Client) SetActivity(activity *types.Activity) {
	c.mu.Lock()
	c.activity = activity
	c.mu.Unlock()
}

// focus management
func (c *Client) SetFocus(channelID, serverID, conversationID *uuid.UUID) {
	c.mu.Lock()
	c.focusedChannel = channelID
	c.focusedServer = serverID
	c.focusedConversation = conversationID
	c.mu.Unlock()
}

func (c *Client) ClearFocus() {
	c.mu.Lock()
	c.focusedChannel = nil
	c.focusedServer = nil
	c.focusedConversation = nil
	c.mu.Unlock()
}

func (c *Client) IsFocusedOnChannel(channelID uuid.UUID) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.focusedChannel != nil && *c.focusedChannel == channelID
}

func (c *Client) IsFocusedOnConversation(convID uuid.UUID) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.focusedConversation != nil && *c.focusedConversation == convID
}

// subscriptions
func (c *Client) SubscribeServer(serverID uuid.UUID) {
	c.mu.Lock()
	c.servers[serverID] = true
	c.mu.Unlock()
}

func (c *Client) UnsubscribeServer(serverID uuid.UUID) {
	c.mu.Lock()
	delete(c.servers, serverID)
	c.mu.Unlock()
}

func (c *Client) IsInServer(serverID uuid.UUID) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.servers[serverID]
}

func (c *Client) SubscribeConversation(convID uuid.UUID) {
	c.mu.Lock()
	c.conversations[convID] = true
	c.mu.Unlock()
}

func (c *Client) UnsubscribeConversation(convID uuid.UUID) {
	c.mu.Lock()
	delete(c.conversations, convID)
	c.mu.Unlock()
}

func (c *Client) IsInConversation(convID uuid.UUID) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conversations[convID]
}

func (c *Client) GetServerIDs() []uuid.UUID {
	c.mu.RLock()
	defer c.mu.RUnlock()
	ids := make([]uuid.UUID, 0, len(c.servers))
	for id := range c.servers {
		ids = append(ids, id)
	}
	return ids
}

func (c *Client) GetConversationIDs() []uuid.UUID {
	c.mu.RLock()
	defer c.mu.RUnlock()
	ids := make([]uuid.UUID, 0, len(c.conversations))
	for id := range c.conversations {
		ids = append(ids, id)
	}
	return ids
}

// pumps
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
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("[ws] read error for session %s: %v", c.sessionID, err)
			}
			break
		}

		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			c.SendError(4000, "invalid message format")
			continue
		}

		c.hub.HandleMessage(c, &msg)
	}
}

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
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// batch queued messages
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

func (c *Client) Send(msg *Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[ws] marshal error: %v", err)
		return
	}

	select {
	case c.send <- data:
	default:
		log.Printf("[ws] client %s buffer full", c.sessionID)
	}
}

func (c *Client) SendDispatch(event EventType, data any) {
	c.Send(&Message{
		Op:    OpDispatch,
		Event: event,
		Data:  data,
	})
}

func (c *Client) SendError(code int, message string) {
	c.Send(&Message{
		Op: OpDispatch,
		Data: ErrorPayload{
			Code:    code,
			Message: message,
		},
	})
}

func (c *Client) SendAck(nonce string, data any) {
	c.Send(&Message{
		Op:    OpDispatch,
		Nonce: nonce,
		Data:  data,
	})
}
