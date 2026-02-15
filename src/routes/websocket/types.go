package websocket

import (
	"time"

	"github.com/hindsightchat/backend/src/types"
	uuid "github.com/satori/go.uuid"
)

// opcodes
type OpCode int

const (
	// client -> server
	OpHeartbeat      OpCode = 1 // sent periodically by client to keep the connection alive, contains timestamp - server should respond with OpHeartbeatAck containing same timestamp
	OpIdentify       OpCode = 2 // sent to identify/authenticate the client after connecting, contains auth token
	OpPresenceUpdate OpCode = 3 // sent when user updates presence (status or activity)
	OpFocusChange    OpCode = 4 // sent when user changes focus (e.g focuses a different channel, server or conversation, or unfocuses)

	// server -> client
	OpDispatch       OpCode = 0  // e.g for events
	OpHeartbeatAck   OpCode = 11 // sent in response to heartbeat, can be used to measure latency
	OpReady          OpCode = 12 // sent after successful identify, contains initial state data
	OpInvalidSession OpCode = 13 // sent when session is invalid, client should re-identify

	// bidirectional
	OpTypingStart   OpCode = 20 // sent when user starts typing in a channel or conversation
	OpTypingStop    OpCode = 21 // sent when user stops typing (after a timeout) - client should implement a timeout to stop typing after e.g 5 seconds of inactivity
	OpMessageCreate OpCode = 22 // sent when a new message is created in a channel or conversation
	OpMessageEdit   OpCode = 23 // sent when a message is edited in a channel or conversation
	OpMessageDelete OpCode = 24 // sent when a message is deleted in a channel or conversation
	OpMessageAck    OpCode = 25 // sent when a message is read by the client, contains message ID and channel/conversation ID
)

// event types for dispatch
type EventType string

const (
	// messages
	EventChannelMessageCreate EventType = "CHANNEL_MESSAGE_CREATE"
	EventChannelMessageUpdate EventType = "CHANNEL_MESSAGE_UPDATE"
	EventChannelMessageDelete EventType = "CHANNEL_MESSAGE_DELETE"
	EventDMMessageCreate      EventType = "DM_MESSAGE_CREATE"
	EventDMMessageUpdate      EventType = "DM_MESSAGE_UPDATE"
	EventDMMessageDelete      EventType = "DM_MESSAGE_DELETE"

	// lightweight notifications (unfocused)
	EventChannelMessageNotify EventType = "CHANNEL_MESSAGE_NOTIFY"
	EventDMMessageNotify      EventType = "DM_MESSAGE_NOTIFY"

	// typing
	EventTypingStart EventType = "TYPING_START"
	EventTypingStop  EventType = "TYPING_STOP"

	// presence
	EventPresenceUpdate EventType = "PRESENCE_UPDATE"

	// server events
	EventServerUpdate       EventType = "SERVER_UPDATE"
	EventServerMemberAdd    EventType = "SERVER_MEMBER_ADD"
	EventServerMemberRemove EventType = "SERVER_MEMBER_REMOVE"
	EventServerMemberUpdate EventType = "SERVER_MEMBER_UPDATE"
	EventChannelCreate      EventType = "CHANNEL_CREATE"
	EventChannelUpdate      EventType = "CHANNEL_UPDATE"
	EventChannelDelete      EventType = "CHANNEL_DELETE"

	// dm events
	EventDMCreate          EventType = "DM_CREATE"
	EventDMParticipantAdd  EventType = "DM_PARTICIPANT_ADD"
	EventDMParticipantLeft EventType = "DM_PARTICIPANT_LEFT"

	// user
	EventUserUpdate EventType = "USER_UPDATE"

	// friends
	EventFriendRequestCreate   EventType = "FRIEND_REQUEST_CREATE"
	EventFriendRequestAccepted EventType = "FRIEND_REQUEST_ACCEPTED"
	EventFriendRemove          EventType = "FRIEND_REMOVE"

	// read state
	EventMessageAck EventType = "MESSAGE_ACK"
)

// base message structure
type Message struct {
	Op    OpCode    `json:"op"`
	Data  any       `json:"d,omitempty"`
	Event EventType `json:"t,omitempty"`
	Nonce string    `json:"nonce,omitempty"`
}

// payloads

type IdentifyPayload struct {
	Token string `json:"token"`
}

type ReadyPayload struct {
	User      UserBrief            `json:"user"`
	SessionID string               `json:"session_id"`
	Users     []UserWithPresence   `json:"users"`
}

type UserWithPresence struct {
	ID            uuid.UUID       `json:"id"`
	Username      string          `json:"username"`
	Domain        string          `json:"domain"`
	ProfilePicURL string          `json:"profilePicURL,omitempty"`
	Presence      *PresenceData   `json:"presence,omitempty"`
}

type HeartbeatPayload struct {
	Timestamp int64 `json:"ts"`
}

type FocusPayload struct {
	ChannelID      *uuid.UUID `json:"channel_id,omitempty"`
	ServerID       *uuid.UUID `json:"server_id,omitempty"`
	ConversationID *uuid.UUID `json:"conversation_id,omitempty"`
}

type PresenceUpdatePayload struct {
	UserID   uuid.UUID       `json:"user_id"`
	Status   string          `json:"status"`
	Activity *types.Activity `json:"activity,omitempty"`
}

type TypingPayload struct {
	ChannelID      *uuid.UUID `json:"channel_id,omitempty"`
	ServerID       *uuid.UUID `json:"server_id,omitempty"`
	ConversationID *uuid.UUID `json:"conversation_id,omitempty"`
	UserID         uuid.UUID  `json:"user_id"`
	User           *UserBrief `json:"user,omitempty"`
}

type ChannelMessagePayload struct {
	ID          uuid.UUID  `json:"id"`
	ChannelID   uuid.UUID  `json:"channel_id"`
	ServerID    uuid.UUID  `json:"server_id"`
	AuthorID    uuid.UUID  `json:"author_id"`
	Author      *UserBrief `json:"author,omitempty"`
	Content     string     `json:"content"`
	Attachments []any      `json:"attachments,omitempty"`
	ReplyToID   *uuid.UUID `json:"reply_to_id,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	EditedAt    *time.Time `json:"edited_at,omitempty"`
}

type DMMessagePayload struct {
	ID             uuid.UUID  `json:"id"`
	ConversationID uuid.UUID  `json:"conversation_id"`
	AuthorID       uuid.UUID  `json:"author_id"`
	Author         *UserBrief `json:"author,omitempty"`
	Content        string     `json:"content"`
	Attachments    []any      `json:"attachments,omitempty"`
	ReplyToID      *uuid.UUID `json:"reply_to_id,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	EditedAt       *time.Time `json:"edited_at,omitempty"`
}

// lightweight notify payloads (for unfocused clients)
type ChannelMessageNotifyPayload struct {
	ChannelID uuid.UUID `json:"channel_id"`
	ServerID  uuid.UUID `json:"server_id"`
	MessageID uuid.UUID `json:"message_id"`
	AuthorID  uuid.UUID `json:"author_id"`
}

type DMMessageNotifyPayload struct {
	ConversationID uuid.UUID `json:"conversation_id"`
	MessageID      uuid.UUID `json:"message_id"`
	AuthorID       uuid.UUID `json:"author_id"`
}

type MessageDeletePayload struct {
	MessageID      uuid.UUID  `json:"message_id"`
	ChannelID      *uuid.UUID `json:"channel_id,omitempty"`
	ServerID       *uuid.UUID `json:"server_id,omitempty"`
	ConversationID *uuid.UUID `json:"conversation_id,omitempty"`
}

type MessageAckPayload struct {
	ChannelID      *uuid.UUID `json:"channel_id,omitempty"`
	ConversationID *uuid.UUID `json:"conversation_id,omitempty"`
	MessageID      uuid.UUID  `json:"message_id"`
}

type UserBrief struct {
	ID            uuid.UUID `json:"id"`
	Username      string    `json:"username"`
	Domain        string    `json:"domain"`
	ProfilePicURL string    `json:"profilePicURL,omitempty"`
	Email         string    `json:"email"`
}

type ErrorPayload struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
