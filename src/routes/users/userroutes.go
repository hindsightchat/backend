package usersroutes

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hindsightchat/backend/src/lib/authhelper"
	database "github.com/hindsightchat/backend/src/lib/dbs/tidb"
	valkeydb "github.com/hindsightchat/backend/src/lib/dbs/valkey"
	"github.com/hindsightchat/backend/src/lib/httpresponder"
	"github.com/hindsightchat/backend/src/middleware"
	"github.com/hindsightchat/backend/src/routes/websocket"
	uuid "github.com/satori/go.uuid"
)

type conversationResponse struct {
	ID           string      `json:"id"`
	Name         string      `json:"name,omitempty"`
	IsGroup      bool        `json:"is_group"`
	Participants []userBrief `json:"participants"`
	LastReadAt   *time.Time  `json:"last_read_at,omitempty"`
	CreatedAt    time.Time   `json:"created_at"`
}

type serverResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Icon        string    `json:"icon,omitempty"`
	OwnerID     string    `json:"owner_id"`
	JoinedAt    time.Time `json:"joined_at"`
}

type userBrief struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Domain   string `json:"domain"`

	Presence *websocket.PresenceData `json:"presence,omitempty"`
}

func RegisterRoutes(r chi.Router) {
	r.Route("/users", func(r chi.Router) {
		r.Use(middleware.RouteRequiresAuthentication)

		r.Route("/@me", func(r chi.Router) {
			r.Get("/conversations", getConversations)
			r.Get("/servers", getServers)
		})

		r.Route("/{id}", func(r chi.Router) {
			// get user by ID
			r.Get("/", func(w http.ResponseWriter, r *http.Request) {
				userID := chi.URLParam(r, "id")

				// validate user ID as UUID
				uid, err := uuid.FromString(userID)
				if err != nil {
					httpresponder.SendErrorResponse(w, r, "Invalid user ID format!", http.StatusBadRequest)
					return
				}

				var user database.User
				err = database.DB.Where("id = ?", uid).First(&user).Error
				if err != nil {
					httpresponder.SendErrorResponse(w, r, "User not found!", http.StatusNotFound)
					return
				}

				var presence websocket.PresenceData

				bytes, err := valkeydb.GetValkeyClient().Get(r.Context(), valkeydb.PRESENCE_PREFIX+user.ID.String()).Bytes()

				if err == nil {
					if err := json.Unmarshal(bytes, &presence); err == nil {
						// presence successfully loaded, can include in response if we want

						if presence.Status == "offline" {
							// if offline, set presence to nil to avoid showing stale activity info
							presence = websocket.PresenceData{}
						}
					} else {
						fmt.Printf("Failed to unmarshal presence for user %s: %v\n", user.Username, err)
					}
				}

				httpresponder.SendSuccessResponse(w, r, userBrief{
					ID:       user.ID.String(),
					Username: user.Username,
					Domain:   user.Domain,
					Presence: &presence,
				})
			})
		})
	})
}

func getConversations(w http.ResponseWriter, r *http.Request) {
	user, err := authhelper.GetUserFromRequest(r)
	if err != nil || user == nil {
		httpresponder.SendErrorResponse(w, r, "unauthorized", http.StatusUnauthorized)
		return
	}

	myUserID := user.ID.String()

	// get conversations user is part of
	var myParticipations []database.DMParticipant
	err = database.DB.
		Preload("Conversation").
		Where("user_id = ?", user.ID).
		Find(&myParticipations).Error

	if err != nil {
		httpresponder.SendErrorResponse(w, r, "failed to fetch conversations", http.StatusInternalServerError)
		return
	}

	if len(myParticipations) == 0 {
		httpresponder.SendSuccessResponse(w, r, []conversationResponse{})
		return
	}

	// collect conversation ids
	convIDs := make([]uuid.UUID, len(myParticipations))
	for i, p := range myParticipations {
		convIDs[i] = p.ConversationID
	}

	// get all participants for these conversations
	var allParticipants []database.DMParticipant
	err = database.DB.
		Where("conversation_id IN ?", convIDs).
		Find(&allParticipants).Error

	if err != nil {
		httpresponder.SendErrorResponse(w, r, "failed to fetch participants", http.StatusInternalServerError)
		return
	}

	// collect other user ids
	userIDSet := make(map[string]uuid.UUID)
	for _, p := range allParticipants {
		uid := p.UserID.String()
		if uid != myUserID {
			userIDSet[uid] = p.UserID
		}
	}

	// fetch users directly
	usersMap := make(map[string]database.User)
	for _, userID := range userIDSet {
		var u database.User
		if err := database.DB.Where("id = ?", userID).First(&u).Error; err == nil {
			usersMap[u.ID.String()] = u
		}
	}

	// group participants by conversation
	participantsByConv := make(map[string][]string)
	for _, p := range allParticipants {
		convID := p.ConversationID.String()
		participantsByConv[convID] = append(participantsByConv[convID], p.UserID.String())
	}

	// build response
	conversations := make([]conversationResponse, 0, len(myParticipations))
	for _, p := range myParticipations {
		convID := p.Conversation.ID.String()

		conv := conversationResponse{
			ID:           convID,
			Name:         p.Conversation.Name,
			IsGroup:      p.Conversation.IsGroup,
			LastReadAt:   p.LastReadAt,
			CreatedAt:    p.Conversation.CreatedAt,
			Participants: make([]userBrief, 0),
		}

		// add other participants
		for _, userID := range participantsByConv[convID] {
			if userID != myUserID || p.Conversation.IsGroup {
				if u, ok := usersMap[userID]; ok {
					conv.Participants = append(conv.Participants, userBrief{
						ID:       u.ID.String(),
						Username: u.Username,
						Domain:   u.Domain,
					})
				}
			}
		}

		conversations = append(conversations, conv)
	}

	httpresponder.SendSuccessResponse(w, r, conversations)
}

func getServers(w http.ResponseWriter, r *http.Request) {
	user, err := authhelper.GetUserFromRequest(r)
	if err != nil || user == nil {
		httpresponder.SendErrorResponse(w, r, "unauthorized", http.StatusUnauthorized)
		return
	}

	var memberships []database.ServerMember
	err = database.DB.
		Preload("Server").
		Where("user_id = ?", user.ID).
		Find(&memberships).Error

	if err != nil {
		httpresponder.SendErrorResponse(w, r, "failed to fetch servers", http.StatusInternalServerError)
		return
	}

	servers := make([]serverResponse, 0, len(memberships))
	for _, m := range memberships {
		servers = append(servers, serverResponse{
			ID:          m.Server.ID.String(),
			Name:        m.Server.Name,
			Description: m.Server.Description,
			Icon:        m.Server.Icon,
			OwnerID:     m.Server.OwnerID.String(),
			JoinedAt:    m.JoinedAt,
		})
	}

	httpresponder.SendSuccessResponse(w, r, servers)
}
