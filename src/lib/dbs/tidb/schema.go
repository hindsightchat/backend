package database

import (
	"time"

	uuid "github.com/satori/go.uuid"
	"gorm.io/gorm"
)

type BaseModel struct {
	ID        uuid.UUID `gorm:"type:char(36);primaryKey"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (base *BaseModel) BeforeCreate(tx *gorm.DB) (err error) {
	base.ID = uuid.NewV4()
	return
}

type User struct {
	BaseModel
	Username string `gorm:"type:varchar(50);uniqueIndex;not null"`
	Domain   string `gorm:"type:varchar(100);not null"` // e.g .aurality.stream

	Email    string `gorm:"type:varchar(100);uniqueIndex;not null"`
	Password string `gorm:"type:varchar(255);not null"`

	IsDomainVerified bool `gorm:"not null;default:false"`

	// Relations
	Tokens            []UserToken      `gorm:"foreignKey:UserID"`
	OwnedServers      []Server         `gorm:"foreignKey:OwnerID"`
	ServerMemberships []ServerMember   `gorm:"foreignKey:UserID"`
	ChannelMessages   []ChannelMessage `gorm:"foreignKey:AuthorID"`
	DMParticipations  []DMParticipant  `gorm:"foreignKey:UserID"`
	DirectMessages    []DirectMessage  `gorm:"foreignKey:AuthorID"`
}


type UserToken struct {
	BaseModel
	UserID    uuid.UUID `gorm:"type:char(36);not null;index"`
	Token     string    `gorm:"type:char(64);not null;uniqueIndex"`
	ExpiresAt int64     `gorm:"not null;index"`

	User User `gorm:"foreignKey:UserID"`
}

type Server struct {
	BaseModel
	Name        string    `gorm:"type:varchar(100);not null"`
	Description string    `gorm:"type:varchar(500)"`
	Icon        string    `gorm:"type:varchar(255)"` // URL to server icon
	OwnerID     uuid.UUID `gorm:"type:char(36);not null;index"`

	OwnedDomain string `gorm:"type:varchar(100);uniqueIndex"` // e.g. mydomain.com

	Owner    User           `gorm:"foreignKey:OwnerID"`
	Channels []Channel      `gorm:"foreignKey:ServerID"`
	Members  []ServerMember `gorm:"foreignKey:ServerID"`
	Roles    []Role         `gorm:"foreignKey:ServerID"`
}

// permission role within server
type Role struct {
	BaseModel
	ServerID    uuid.UUID `gorm:"type:char(36);not null;index"`
	Name        string    `gorm:"type:varchar(100);not null"`
	Color       string    `gorm:"type:varchar(7)"` // Hex color e.g. #FF5733
	Permissions uint64    `gorm:"not null;default:0"`
	Position    int       `gorm:"not null;default:0"` // role hierarchy position, higher means more priority
	IsDefault   bool      `gorm:"not null;default:false"`

	Server Server `gorm:"foreignKey:ServerID"`
}

// server member represents a user's membership in a server and their roles in that server
type ServerMember struct {
	BaseModel
	ServerID uuid.UUID `gorm:"type:char(36);not null;uniqueIndex:idx_server_user"`
	UserID   uuid.UUID `gorm:"type:char(36);not null;uniqueIndex:idx_server_user"`
	JoinedAt time.Time `gorm:"not null"`

	Server Server `gorm:"foreignKey:ServerID"`
	User   User   `gorm:"foreignKey:UserID"`
	Roles  []Role `gorm:"many2many:server_member_roles;"`
}

// channel represents a channel within a server
type Channel struct {
	BaseModel
	ServerID    uuid.UUID `gorm:"type:char(36);not null;index"`
	Name        string    `gorm:"type:varchar(100);not null"`
	Description string    `gorm:"type:varchar(500)"`
	Type        int       `gorm:"not null;default:0"` // 0=text, 1=voice
	Position    int       `gorm:"not null;default:0"`

	Server   Server           `gorm:"foreignKey:ServerID"`
	Messages []ChannelMessage `gorm:"foreignKey:ChannelID"`
}

// channel message represents a message in a server channel
type ChannelMessage struct {
	BaseModel
	ChannelID   uuid.UUID  `gorm:"type:char(36);not null;index"`
	AuthorID    uuid.UUID  `gorm:"type:char(36);not null;index"`
	Content     string     `gorm:"type:text;not null"`
	Attachments string     `gorm:"type:json"` // JSON array of attachments
	ReplyToID   *uuid.UUID `gorm:"type:char(36);index"`
	EditedAt    *time.Time

	Channel Channel         `gorm:"foreignKey:ChannelID"`
	Author  User            `gorm:"foreignKey:AuthorID"`
	ReplyTo *ChannelMessage `gorm:"foreignKey:ReplyToID"`
}

// DMConversation represents a DM conversation (1:1 or group)
type DMConversation struct {
	BaseModel
	Name    string `gorm:"type:varchar(100)"` // Only for group DMs
	IsGroup bool   `gorm:"not null;default:false"`

	Participants []DMParticipant `gorm:"foreignKey:ConversationID"`
	Messages     []DirectMessage `gorm:"foreignKey:ConversationID"`
}

// DMParticipant represents a user in a DM conversation
type DMParticipant struct {
	BaseModel
	ConversationID uuid.UUID `gorm:"type:char(36);not null;uniqueIndex:idx_conv_user"`
	UserID         uuid.UUID `gorm:"type:char(36);not null;uniqueIndex:idx_conv_user"`
	JoinedAt       time.Time `gorm:"not null"`
	LastReadAt     *time.Time

	Conversation DMConversation `gorm:"foreignKey:ConversationID"`
	User         User           `gorm:"foreignKey:UserID"`
}

// DirectMessage represents a message in a DM conversation
type DirectMessage struct {
	BaseModel
	ConversationID uuid.UUID  `gorm:"type:char(36);not null;index"`
	AuthorID       uuid.UUID  `gorm:"type:char(36);not null;index"`
	Content        string     `gorm:"type:text;not null"`
	Attachments    string     `gorm:"type:json"`
	ReplyToID      *uuid.UUID `gorm:"type:char(36);index"`
	EditedAt       *time.Time

	Conversation DMConversation `gorm:"foreignKey:ConversationID"`
	Author       User           `gorm:"foreignKey:AuthorID"`
	ReplyTo      *DirectMessage `gorm:"foreignKey:ReplyToID"`
}

var Schema = []interface{}{
	&User{},
	&UserToken{},

	// Servers
	&Server{},
	&Role{},
	&ServerMember{},
	&Channel{},
	&ChannelMessage{},

	// Direct Messages
	&DMConversation{},
	&DMParticipant{},
	&DirectMessage{},
}
