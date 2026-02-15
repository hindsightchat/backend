package websocket

import (
	"encoding/json"
	"log"
	"time"

	"github.com/hindsightchat/backend/src/lib/authhelper"
	database "github.com/hindsightchat/backend/src/lib/dbs/tidb"
	"github.com/hindsightchat/backend/src/types"
	uuid "github.com/satori/go.uuid"
)

// routes incoming messages to handlers
func (h *Hub) HandleMessage(client *Client, msg *Message) {
	if !client.IsIdentified() && msg.Op != OpIdentify {
		client.SendError(4001, "not authenticated")
		return
	}

	switch msg.Op {
	case OpIdentify:
		h.handleIdentify(client, msg)
	case OpHeartbeat:
		h.handleHeartbeat(client, msg)
	case OpPresenceUpdate:
		h.handlePresenceUpdate(client, msg)
	case OpFocusChange:
		h.handleFocusChange(client, msg)
	case OpTypingStart:
		h.handleTypingStart(client, msg)
	case OpTypingStop:
		h.handleTypingStop(client, msg)
	case OpMessageCreate:
		h.handleMessageCreate(client, msg)
	case OpMessageEdit:
		h.handleMessageEdit(client, msg)
	case OpMessageDelete:
		h.handleMessageDelete(client, msg)
	case OpMessageAck:
		h.handleMessageAck(client, msg)
	default:
		client.SendError(4002, "unknown opcode")
	}
}

func (h *Hub) handleIdentify(client *Client, msg *Message) {
	if client.IsIdentified() {
		client.SendError(4003, "already identified")
		return
	}

	data, err := json.Marshal(msg.Data)
	if err != nil {
		client.SendError(4000, "invalid payload")
		return
	}

	var payload IdentifyPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		client.SendError(4000, "invalid payload")
		return
	}

	if payload.Token == "" {
		client.Send(&Message{Op: OpInvalidSession})
		return
	}

	// validate token
	userIDStr, err := authhelper.GetUserIDFromToken(payload.Token)
	if err != nil || userIDStr == "" {
		client.Send(&Message{Op: OpInvalidSession})
		return
	}

	userID, err := uuid.FromString(userIDStr)
	if err != nil {
		client.Send(&Message{Op: OpInvalidSession})
		return
	}

	// fetch user
	var user database.User
	if err := database.DB.Where("id = ?", userID).First(&user).Error; err != nil {
		client.Send(&Message{Op: OpInvalidSession})
		return
	}

	userBrief := &UserBrief{
		ID:            user.ID,
		Username:      user.Username,
		Domain:        user.Domain,
		Email:         user.Email,
		ProfilePicURL: user.ProfilePicURL,
	}

	// register and subscribe
	h.RegisterIdentifiedClient(client, userID, userBrief)

	if err := h.LoadUserSubscriptions(client); err != nil {
		log.Printf("[ws] failed to load subscriptions: %v", err)
	}

	h.presence.SetOnline(userID, "online", nil)

	// load all relevant users with presence
	users := h.loadRelevantUsers(userID)

	client.Send(&Message{
		Op: OpReady,
		Data: ReadyPayload{
			User:      *userBrief,
			SessionID: client.sessionID,
			Users:     users,
		},
	})

	go h.broadcastPresenceChange(userID, "online", &types.Activity{})

	log.Printf("[ws] user identified: %s (%s)", user.Username, userID)
}

// loadRelevantUsers gathers all users the client needs to know about:
// friends, conversation participants, server members
func (h *Hub) loadRelevantUsers(userID uuid.UUID) []UserWithPresence {
	userMap := make(map[uuid.UUID]database.User)

	// get friends
	var friendships []database.Friendship
	database.DB.Where("user1_id = ? OR user2_id = ?", userID, userID).Find(&friendships)

	var friendIDs []uuid.UUID
	for _, f := range friendships {
		if f.User1ID == userID {
			friendIDs = append(friendIDs, f.User2ID)
		} else {
			friendIDs = append(friendIDs, f.User1ID)
		}
	}

	// get conversation participants
	var myParticipations []database.DMParticipant
	database.DB.Where("user_id = ?", userID).Find(&myParticipations)

	var convIDs []uuid.UUID
	for _, p := range myParticipations {
		convIDs = append(convIDs, p.ConversationID)
	}

	var otherParticipants []database.DMParticipant
	if len(convIDs) > 0 {
		database.DB.Where("conversation_id IN ? AND user_id != ?", convIDs, userID).Find(&otherParticipants)
	}

	var participantIDs []uuid.UUID
	for _, p := range otherParticipants {
		participantIDs = append(participantIDs, p.UserID)
	}

	// get server members
	var myMemberships []database.ServerMember
	database.DB.Where("user_id = ?", userID).Find(&myMemberships)

	var serverIDs []uuid.UUID
	for _, m := range myMemberships {
		serverIDs = append(serverIDs, m.ServerID)
	}

	var otherMembers []database.ServerMember
	if len(serverIDs) > 0 {
		database.DB.Where("server_id IN ? AND user_id != ?", serverIDs, userID).Find(&otherMembers)
	}

	var memberIDs []uuid.UUID
	for _, m := range otherMembers {
		memberIDs = append(memberIDs, m.UserID)
	}

	// combine all unique user IDs
	allIDs := make(map[uuid.UUID]bool)
	for _, id := range friendIDs {
		allIDs[id] = true
	}
	for _, id := range participantIDs {
		allIDs[id] = true
	}
	for _, id := range memberIDs {
		allIDs[id] = true
	}

	if len(allIDs) == 0 {
		return []UserWithPresence{}
	}

	// fetch all users
	var uniqueIDs []uuid.UUID
	for id := range allIDs {
		uniqueIDs = append(uniqueIDs, id)
	}

	var users []database.User
	database.DB.Where("id IN ?", uniqueIDs).Find(&users)

	for _, u := range users {
		userMap[u.ID] = u
	}

	// get presence for all users
	presences := h.presence.GetMultiplePresences(uniqueIDs)

	// build result
	result := make([]UserWithPresence, 0, len(userMap))
	for id, u := range userMap {
		uwp := UserWithPresence{
			ID:            u.ID,
			Username:      u.Username,
			Domain:        u.Domain,
			ProfilePicURL: u.ProfilePicURL,
		}
		if p, ok := presences[id]; ok {
			uwp.Presence = p
		}
		result = append(result, uwp)
	}

	return result
}

func (h *Hub) handleHeartbeat(client *Client, msg *Message) {
	// refresh presence TTL to keep user online
	if client.IsIdentified() {
		h.presence.RefreshPresence(client.userID)
	}

	client.Send(&Message{
		Op:   OpHeartbeatAck,
		Data: HeartbeatPayload{Timestamp: time.Now().UnixMilli()},
	})
}

func (h *Hub) handleFocusChange(client *Client, msg *Message) {
	data, err := json.Marshal(msg.Data)
	if err != nil {
		return
	}

	var payload FocusPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return
	}

	// validate access before setting focus
	if payload.ServerID != nil && !client.IsInServer(*payload.ServerID) {
		return
	}
	if payload.ConversationID != nil && !client.IsInConversation(*payload.ConversationID) {
		return
	}

	client.SetFocus(payload.ChannelID, payload.ServerID, payload.ConversationID)
	// send akcnowledgement back
	client.SendAck(msg.Nonce, map[string]any{
		"channel_id":      payload.ChannelID,
		"server_id":       payload.ServerID,
		"conversation_id": payload.ConversationID,
	})
}

func (h *Hub) handlePresenceUpdate(client *Client, msg *Message) {
	data, err := json.Marshal(msg.Data)
	if err != nil {
		return
	}

	var payload PresenceUpdatePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return
	}

	validStatuses := map[string]bool{"online": true, "idle": true, "dnd": true, "offline": true}
	if !validStatuses[payload.Status] {
		client.SendError(4000, "invalid status")
		return
	}

	client.SetStatus(payload.Status)
	client.SetActivity(payload.Activity)

	h.presence.SetOnline(client.userID, payload.Status, payload.Activity)

	go h.broadcastPresenceChange(client.userID, payload.Status, payload.Activity)
}

func (h *Hub) handleTypingStart(client *Client, msg *Message) {
	data, err := json.Marshal(msg.Data)
	if err != nil {
		return
	}

	var payload TypingPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return
	}

	payload.UserID = client.userID
	payload.User = client.user

	if payload.ChannelID != nil && payload.ServerID != nil {
		if !client.IsInServer(*payload.ServerID) {
			return
		}
		h.DispatchToServer(*payload.ServerID, EventTypingStart, payload)
	} else if payload.ConversationID != nil {
		if !client.IsInConversation(*payload.ConversationID) {
			return
		}
		h.DispatchToConversation(*payload.ConversationID, EventTypingStart, payload)
	}
}

func (h *Hub) handleTypingStop(client *Client, msg *Message) {
	data, err := json.Marshal(msg.Data)
	if err != nil {
		return
	}

	var payload TypingPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return
	}

	payload.UserID = client.userID
	payload.User = client.user

	if payload.ChannelID != nil && payload.ServerID != nil {
		if !client.IsInServer(*payload.ServerID) {
			return
		}
		h.DispatchToServer(*payload.ServerID, EventTypingStop, payload)
	} else if payload.ConversationID != nil {
		if !client.IsInConversation(*payload.ConversationID) {
			return
		}
		h.DispatchToConversation(*payload.ConversationID, EventTypingStop, payload)
	}
}

func (h *Hub) handleMessageCreate(client *Client, msg *Message) {
	data, err := json.Marshal(msg.Data)
	if err != nil {
		client.SendError(4000, "invalid payload")
		return
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		client.SendError(4000, "invalid payload")
		return
	}

	if _, ok := raw["channel_id"]; ok {
		h.handleChannelMessageCreate(client, msg, data)
	} else if _, ok := raw["conversation_id"]; ok {
		h.handleDMMessageCreate(client, msg, data)
	} else {
		client.SendError(4000, "missing channel_id or conversation_id")
	}
}

func (h *Hub) handleChannelMessageCreate(client *Client, msg *Message, data []byte) {
	var payload ChannelMessagePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		client.SendError(4000, "invalid payload")
		return
	}

	if !client.IsInServer(payload.ServerID) {
		client.SendError(4003, "not in server")
		return
	}

	var channel database.Channel
	if err := database.DB.Where("id = ? AND server_id = ?", payload.ChannelID, payload.ServerID).First(&channel).Error; err != nil {
		client.SendError(4004, "channel not found")
		return
	}

	dbMsg := database.ChannelMessage{
		ChannelID:   channel.ID,
		AuthorID:    client.userID,
		Content:     payload.Content,
		Attachments: "[]",
		ReplyToID:   payload.ReplyToID,
	}

	if err := database.DB.Create(&dbMsg).Error; err != nil {
		client.SendError(5000, "failed to create message")
		return
	}

	responsePayload := ChannelMessagePayload{
		ID:        dbMsg.ID,
		ChannelID: dbMsg.ChannelID,
		ServerID:  payload.ServerID,
		AuthorID:  dbMsg.AuthorID,
		Author:    client.user,
		Content:   dbMsg.Content,
		ReplyToID: dbMsg.ReplyToID,
		CreatedAt: dbMsg.CreatedAt,
	}

	// focus-aware dispatch
	h.DispatchChannelMessage(payload.ServerID, payload.ChannelID, responsePayload)

	if msg.Nonce != "" {
		client.SendAck(msg.Nonce, map[string]any{"id": dbMsg.ID})
	}
}

func (h *Hub) handleDMMessageCreate(client *Client, msg *Message, data []byte) {
	var payload DMMessagePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		client.SendError(4000, "invalid payload")
		return
	}

	if !client.IsInConversation(payload.ConversationID) {
		client.SendError(4003, "not in conversation")
		return
	}

	dbMsg := database.DirectMessage{
		ConversationID: payload.ConversationID,
		AuthorID:       client.userID,
		Content:        payload.Content,
		Attachments:    "[]",
		ReplyToID:      payload.ReplyToID,
	}

	if err := database.DB.Create(&dbMsg).Error; err != nil {
		client.SendError(5000, "failed to create message")
		return
	}

	responsePayload := DMMessagePayload{
		ID:             dbMsg.ID,
		ConversationID: dbMsg.ConversationID,
		AuthorID:       dbMsg.AuthorID,
		Author:         client.user,
		Content:        dbMsg.Content,
		ReplyToID:      dbMsg.ReplyToID,
		CreatedAt:      dbMsg.CreatedAt,
	}

	// focus-aware dispatch
	h.DispatchDMMessage(payload.ConversationID, responsePayload)

	database.DB.Model(&database.DMParticipant{}).
		Where("conversation_id = ? AND user_id = ?", payload.ConversationID, client.userID).
		Updates(map[string]any{"last_read_at": time.Now()})

	if msg.Nonce != "" {
		client.SendAck(msg.Nonce, map[string]any{"id": dbMsg.ID})
	}
}

func (h *Hub) handleMessageEdit(client *Client, msg *Message) {
	data, err := json.Marshal(msg.Data)
	if err != nil {
		client.SendError(4000, "invalid payload")
		return
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		client.SendError(4000, "invalid payload")
		return
	}

	messageIDStr, _ := raw["id"].(string)
	content, _ := raw["content"].(string)
	messageID, err := uuid.FromString(messageIDStr)
	if err != nil || content == "" {
		client.SendError(4000, "invalid payload")
		return
	}

	now := time.Now()

	if channelIDStr, ok := raw["channel_id"].(string); ok {
		channelID, _ := uuid.FromString(channelIDStr)
		serverIDStr, _ := raw["server_id"].(string)
		serverID, _ := uuid.FromString(serverIDStr)

		if !client.IsInServer(serverID) {
			client.SendError(4003, "not in server")
			return
		}

		result := database.DB.Model(&database.ChannelMessage{}).
			Where("id = ? AND channel_id = ? AND author_id = ?", messageID, channelID, client.userID).
			Updates(map[string]any{"content": content, "edited_at": now})

		if result.RowsAffected == 0 {
			client.SendError(4004, "message not found or not authorized")
			return
		}

		h.DispatchToServer(serverID, EventChannelMessageUpdate, ChannelMessagePayload{
			ID:        messageID,
			ChannelID: channelID,
			ServerID:  serverID,
			AuthorID:  client.userID,
			Content:   content,
			EditedAt:  &now,
		})
		return
	}

	if convIDStr, ok := raw["conversation_id"].(string); ok {
		convID, _ := uuid.FromString(convIDStr)

		if !client.IsInConversation(convID) {
			client.SendError(4003, "not in conversation")
			return
		}

		result := database.DB.Model(&database.DirectMessage{}).
			Where("id = ? AND conversation_id = ? AND author_id = ?", messageID, convID, client.userID).
			Updates(map[string]any{"content": content, "edited_at": now})

		if result.RowsAffected == 0 {
			client.SendError(4004, "message not found or not authorized")
			return
		}

		h.DispatchToConversation(convID, EventDMMessageUpdate, DMMessagePayload{
			ID:             messageID,
			ConversationID: convID,
			AuthorID:       client.userID,
			Content:        content,
			EditedAt:       &now,
		})
	}
}

func (h *Hub) handleMessageDelete(client *Client, msg *Message) {
	data, err := json.Marshal(msg.Data)
	if err != nil {
		client.SendError(4000, "invalid payload")
		return
	}

	var payload MessageDeletePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		client.SendError(4000, "invalid payload")
		return
	}

	if payload.ChannelID != nil && payload.ServerID != nil {
		if !client.IsInServer(*payload.ServerID) {
			client.SendError(4003, "not in server")
			return
		}

		result := database.DB.Where("id = ? AND channel_id = ? AND author_id = ?",
			payload.MessageID, payload.ChannelID, client.userID).
			Delete(&database.ChannelMessage{})

		if result.RowsAffected == 0 {
			client.SendError(4004, "message not found or not authorized")
			return
		}

		h.DispatchToServer(*payload.ServerID, EventChannelMessageDelete, payload)

	} else if payload.ConversationID != nil {
		if !client.IsInConversation(*payload.ConversationID) {
			client.SendError(4003, "not in conversation")
			return
		}

		result := database.DB.Where("id = ? AND conversation_id = ? AND author_id = ?",
			payload.MessageID, payload.ConversationID, client.userID).
			Delete(&database.DirectMessage{})

		if result.RowsAffected == 0 {
			client.SendError(4004, "message not found or not authorized")
			return
		}

		h.DispatchToConversation(*payload.ConversationID, EventDMMessageDelete, payload)
	}
}

func (h *Hub) handleMessageAck(client *Client, msg *Message) {
	data, err := json.Marshal(msg.Data)
	if err != nil {
		return
	}

	var payload MessageAckPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return
	}

	now := time.Now()

	if payload.ConversationID != nil {
		if !client.IsInConversation(*payload.ConversationID) {
			return
		}

		database.DB.Model(&database.DMParticipant{}).
			Where("conversation_id = ? AND user_id = ?", payload.ConversationID, client.userID).
			Updates(map[string]any{"last_read_at": now})

		h.DispatchToConversation(*payload.ConversationID, EventMessageAck, map[string]any{
			"user_id":         client.userID,
			"conversation_id": payload.ConversationID,
			"message_id":      payload.MessageID,
			"read_at":         now,
		})
	}
}

// external helpers for rest api usage

func NotifyChannelMessage(serverID, channelID uuid.UUID, payload ChannelMessagePayload) {
	if hub != nil {
		hub.DispatchChannelMessage(serverID, channelID, payload)
	}
}

func NotifyDMMessage(convID uuid.UUID, payload DMMessagePayload) {
	if hub != nil {
		hub.DispatchDMMessage(convID, payload)
	}
}

func NotifyUserUpdate(userID uuid.UUID, fields map[string]any) {
	if hub != nil {
		hub.DispatchToUser(userID, EventUserUpdate, map[string]any{
			"user_id": userID,
			"fields":  fields,
		})
	}
}

func NotifyServerMemberJoin(serverID uuid.UUID, user UserBrief) {
	if hub != nil {
		hub.DispatchToServer(serverID, EventServerMemberAdd, map[string]any{
			"server_id": serverID,
			"user":      user,
		})
	}
}

func NotifyServerMemberLeave(serverID uuid.UUID, userID uuid.UUID) {
	if hub != nil {
		hub.DispatchToServer(serverID, EventServerMemberRemove, map[string]any{
			"server_id": serverID,
			"user_id":   userID,
		})
	}
}
