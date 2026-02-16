package websocket

import (
	"log"
	"sync"

	database "github.com/hindsightchat/backend/src/lib/dbs/tidb"
	"github.com/hindsightchat/backend/src/types"
	uuid "github.com/satori/go.uuid"
)

type Hub struct {
	clients             map[*Client]bool
	userClients         map[uuid.UUID]map[*Client]bool
	serverClients       map[uuid.UUID]map[*Client]bool
	conversationClients map[uuid.UUID]map[*Client]bool

	register   chan *Client
	unregister chan *Client

	presence *PresenceManager
	mu       sync.RWMutex
}

var hub *Hub

func GetHub() *Hub {
	return hub
}

func NewHub() *Hub {
	h := &Hub{
		clients:             make(map[*Client]bool),
		userClients:         make(map[uuid.UUID]map[*Client]bool),
		serverClients:       make(map[uuid.UUID]map[*Client]bool),
		conversationClients: make(map[uuid.UUID]map[*Client]bool),
		register:            make(chan *Client),
		unregister:          make(chan *Client),
		presence:            NewPresenceManager(),
	}
	hub = h
	return h
}

func (h *Hub) Presence() *PresenceManager {
	return h.presence
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			log.Printf("[ws] client connected: session=%s", client.sessionID)

		case client := <-h.unregister:
			h.handleUnregister(client)
		}
	}
}

func (h *Hub) handleUnregister(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.clients[client]; !ok {
		return
	}

	delete(h.clients, client)

	if client.identified {
		if clients, ok := h.userClients[client.userID]; ok {
			delete(clients, client)
			if len(clients) == 0 {
				delete(h.userClients, client.userID)
				go h.presence.SetOffline(client.userID)
				go h.broadcastPresenceChange(client.userID, "offline", nil)
			}
		}

		for serverID := range client.servers {
			if clients, ok := h.serverClients[serverID]; ok {
				delete(clients, client)
				if len(clients) == 0 {
					delete(h.serverClients, serverID)
				}
			}
		}

		for convID := range client.conversations {
			if clients, ok := h.conversationClients[convID]; ok {
				delete(clients, client)
				if len(clients) == 0 {
					delete(h.conversationClients, convID)
				}
			}
		}
	}

	close(client.send)
	log.Printf("[ws] client disconnected: session=%s user=%s", client.sessionID, client.userID)
}

// registers client after successful auth
func (h *Hub) RegisterIdentifiedClient(client *Client, userID uuid.UUID, user *UserBrief) {
	h.mu.Lock()
	defer h.mu.Unlock()

	client.userID = userID
	client.user = user
	client.identified = true

	if h.userClients[userID] == nil {
		h.userClients[userID] = make(map[*Client]bool)
	}
	h.userClients[userID][client] = true
}

// subscription management
func (h *Hub) SubscribeToServer(client *Client, serverID uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()

	client.SubscribeServer(serverID)

	if h.serverClients[serverID] == nil {
		h.serverClients[serverID] = make(map[*Client]bool)
	}
	h.serverClients[serverID][client] = true
}

func (h *Hub) UnsubscribeFromServer(client *Client, serverID uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()

	client.UnsubscribeServer(serverID)

	if clients, ok := h.serverClients[serverID]; ok {
		delete(clients, client)
		if len(clients) == 0 {
			delete(h.serverClients, serverID)
		}
	}
}

func (h *Hub) SubscribeToConversation(client *Client, convID uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()

	client.SubscribeConversation(convID)

	if h.conversationClients[convID] == nil {
		h.conversationClients[convID] = make(map[*Client]bool)
	}
	h.conversationClients[convID][client] = true
}

func (h *Hub) UnsubscribeFromConversation(client *Client, convID uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()

	client.UnsubscribeConversation(convID)

	if clients, ok := h.conversationClients[convID]; ok {
		delete(clients, client)
		if len(clients) == 0 {
			delete(h.conversationClients, convID)
		}
	}
}

// send methods
func (h *Hub) SendToUser(userID uuid.UUID, msg *Message) {
	h.mu.RLock()
	clients := h.userClients[userID]
	h.mu.RUnlock()

	for client := range clients {
		client.Send(msg)
	}
}

func (h *Hub) SendToServer(serverID uuid.UUID, msg *Message) {
	h.mu.RLock()
	clients := h.serverClients[serverID]
	h.mu.RUnlock()

	for client := range clients {
		client.Send(msg)
	}
}

func (h *Hub) SendToConversation(convID uuid.UUID, msg *Message) {
	h.mu.RLock()
	clients := h.conversationClients[convID]
	h.mu.RUnlock()

	for client := range clients {
		client.Send(msg)
	}
}

// dispatch helpers
func (h *Hub) DispatchToUser(userID uuid.UUID, event EventType, data any) {
	h.SendToUser(userID, &Message{Op: OpDispatch, Event: event, Data: data})
}

func (h *Hub) DispatchToServer(serverID uuid.UUID, event EventType, data any) {
	h.SendToServer(serverID, &Message{Op: OpDispatch, Event: event, Data: data})
}

func (h *Hub) DispatchToConversation(convID uuid.UUID, event EventType, data any) {
	h.SendToConversation(convID, &Message{Op: OpDispatch, Event: event, Data: data})
}

// focus-aware dispatch for channel messages
func (h *Hub) DispatchChannelMessage(serverID, channelID uuid.UUID, fullPayload ChannelMessagePayload) {
	h.mu.RLock()
	clients := h.serverClients[serverID]
	h.mu.RUnlock()

	notifyPayload := ChannelMessageNotifyPayload{
		ChannelID: channelID,
		ServerID:  serverID,
		MessageID: fullPayload.ID,
		AuthorID:  fullPayload.AuthorID,
	}

	for client := range clients {
		if client.IsFocusedOnChannel(channelID) {
			client.SendDispatch(EventChannelMessageCreate, fullPayload)
		} else {
			client.SendDispatch(EventChannelMessageNotify, notifyPayload)
		}
	}
}

// focus-aware dispatch for dm messages
func (h *Hub) DispatchDMMessage(convID uuid.UUID, fullPayload DMMessagePayload) {
	h.mu.RLock()
	clients := h.conversationClients[convID]
	h.mu.RUnlock()

	notifyPayload := DMMessageNotifyPayload{
		ConversationID: convID,
		MessageID:      fullPayload.ID,
		AuthorID:       fullPayload.AuthorID,
	}

	for client := range clients {
		if client.IsFocusedOnConversation(convID) {
			client.SendDispatch(EventDMMessageCreate, fullPayload)
		} else {
			client.SendDispatch(EventDMMessageNotify, notifyPayload)
		}
	}
}

// focus-aware dispatch for typing events (only sends to focused clients)
func (h *Hub) DispatchTypingToConversation(convID uuid.UUID, event EventType, payload TypingPayload) {
	h.mu.RLock()
	clients := h.conversationClients[convID]
	h.mu.RUnlock()

	for client := range clients {
		if client.IsFocusedOnConversation(convID) {
			client.SendDispatch(event, payload)
		}
	}
}

func (h *Hub) DispatchTypingToChannel(serverID, channelID uuid.UUID, event EventType, payload TypingPayload) {
	h.mu.RLock()
	clients := h.serverClients[serverID]
	h.mu.RUnlock()

	for client := range clients {
		if client.IsFocusedOnChannel(channelID) {
			client.SendDispatch(event, payload)
		}
	}
}

// query methods
func (h *Hub) GetOnlineUsers() []uuid.UUID {
	h.mu.RLock()
	defer h.mu.RUnlock()

	users := make([]uuid.UUID, 0, len(h.userClients))
	for userID, clients := range h.userClients {
		if len(clients) > 0 {
			users = append(users, userID)
		}
	}
	return users
}

func (h *Hub) IsUserOnline(userID uuid.UUID) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	clients, ok := h.userClients[userID]
	return ok && len(clients) > 0
}

func (h *Hub) GetUserClients(userID uuid.UUID) []*Client {
	h.mu.RLock()
	defer h.mu.RUnlock()

	clientMap := h.userClients[userID]
	clients := make([]*Client, 0, len(clientMap))
	for client := range clientMap {
		clients = append(clients, client)
	}
	return clients
}

// internal helpers
func (h *Hub) broadcastPresenceChange(userID uuid.UUID, status string, activity *types.Activity) {

	// get activity from valkey

	payload := PresenceUpdatePayload{
		UserID:   userID,
		Status:   status,
		Activity: activity,
	}

	var memberships []database.ServerMember
	database.DB.Where("user_id = ?", userID).Find(&memberships)

	for _, m := range memberships {
		h.DispatchToServer(m.ServerID, EventPresenceUpdate, payload)
	}

	var participants []database.DMParticipant
	database.DB.Where("user_id = ?", userID).Find(&participants)

	for _, p := range participants {
		h.DispatchToConversation(p.ConversationID, EventPresenceUpdate, payload)
	}
}

// loads subscriptions silently (no data sent to client)
func (h *Hub) LoadUserSubscriptions(client *Client) error {
	// load server memberships
	var memberships []database.ServerMember
	if err := database.DB.Where("user_id = ?", client.userID).Find(&memberships).Error; err != nil {
		return err
	}

	for _, m := range memberships {
		h.SubscribeToServer(client, m.ServerID)
	}

	// load dm conversations
	var participants []database.DMParticipant
	if err := database.DB.Where("user_id = ?", client.userID).Find(&participants).Error; err != nil {
		return err
	}

	for _, p := range participants {
		h.SubscribeToConversation(client, p.ConversationID)
	}

	return nil
}
