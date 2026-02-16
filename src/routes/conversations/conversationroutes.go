package conversationroutes

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hindsightchat/backend/src/lib/authhelper"
	database "github.com/hindsightchat/backend/src/lib/dbs/tidb"
	"github.com/hindsightchat/backend/src/lib/httpresponder"
	"github.com/hindsightchat/backend/src/middleware"
	"github.com/hindsightchat/backend/src/routes/websocket"
	uuid "github.com/satori/go.uuid"
)

type authorBrief struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Domain   string `json:"domain"`
}

type messageResponse struct {
	ID          string      `json:"id"`
	Content     string      `json:"content"`
	Attachments string      `json:"attachments,omitempty"`
	Author      authorBrief `json:"author"`
	ReplyToID   *string     `json:"reply_to_id,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
	EditedAt    *time.Time  `json:"edited_at,omitempty"`
}

type CreateConversationRequest struct {
	UserIDs []string `json:"user_ids"` // list of user IDs to include in the conversation (excluding the creator)
	Title   string   `json:"title"`    // optional title for the conversation (for group DMs)
}

func RegisterRoutes(r chi.Router) {
	r.Route("/conversation", func(r chi.Router) {
		r.Use(middleware.RouteRequiresAuthentication)

		r.Post("/create", func(w http.ResponseWriter, r *http.Request) {
			// create group DM conversation with specified users

			user, err := authhelper.GetUserFromRequest(r)
			if err != nil || user == nil {
				httpresponder.SendErrorResponse(w, r, "You are not logged in", http.StatusUnauthorized)
				return
			}

			var req CreateConversationRequest
			err = json.NewDecoder(r.Body).Decode(&req)

			if err != nil {
				httpresponder.SendErrorResponse(w, r, "Invalid request body", http.StatusBadRequest)
				return
			}

			// validate user IDs
			if len(req.UserIDs) == 0 {
				httpresponder.SendErrorResponse(w, r, "At least one user ID is required to create a conversation", http.StatusBadRequest)
				return
			}

			var participantIDs []uuid.UUID
			for _, idStr := range req.UserIDs {
				id, err := uuid.FromString(idStr)
				if err != nil {
					httpresponder.SendErrorResponse(w, r, "Invalid user ID format: "+idStr, http.StatusBadRequest)
					return
				}
				participantIDs = append(participantIDs, id)
			}

			// check if the user is friends with all specified users
			for _, participantID := range participantIDs {
				var friendship database.Friendship
				err = database.DB.
					Where("(user1_id = ? AND user2_id = ?) OR (user1_id = ? AND user2_id = ?)",
						user.ID, participantID, participantID, user.ID).
					First(&friendship).Error

				if err != nil {
					httpresponder.SendErrorResponse(w, r, "You can only create conversations with your friends. Not friends with user ID: "+participantID.String(), http.StatusBadRequest)
					return
				}
			}

			groupName := req.Title

			if groupName == "" {
				// generate group name by concatenating usernames of participants
				var participantUsers []database.User
				err = database.DB.Where("id IN ?", participantIDs).Find(&participantUsers).Error
				if err != nil {
					httpresponder.SendErrorResponse(w, r, "Failed to fetch participant user data", http.StatusInternalServerError)
					return
				}

				for _, participant := range participantUsers {
					if groupName != "" {
						groupName += ", "
					}
					groupName += participant.Username
				}

				// max 20 chars for group name, truncate if necessary
				if len(groupName) > 20 {
					groupName = groupName[:20]
				}

			}

			conv := database.DMConversation{
				Name:    groupName,
				IsGroup: true,
			}

			// create conversation
			err = database.DB.Create(&conv).Error
			if err != nil {
				httpresponder.SendErrorResponse(w, r, "Failed to create conversation", http.StatusInternalServerError)
				return
			}

			// create participant entries for each user (including the creator)
			participants := make([]database.DMParticipant, 0, len(participantIDs)+1)

			// add creator as participant
			participants = append(participants, database.DMParticipant{
				ConversationID: conv.ID,
				UserID:         user.ID,
				JoinedAt:       time.Now(),
			})

			for _, participantID := range participantIDs {
				participants = append(participants, database.DMParticipant{
					ConversationID: conv.ID,
					UserID:         participantID,
					JoinedAt:       time.Now(),
				})
			}

			err = database.DB.Create(&participants).Error
			if err != nil {
				httpresponder.SendErrorResponse(w, r, "Failed to add participants to conversation", http.StatusInternalServerError)
				return
			}

			// notify all participants via websocket and subscribe them to the conversation
			notifyNewGroupDM(&conv, participants, user)

			httpresponder.SendSuccessResponse(w, r, map[string]string{
				"conversation_id": conv.ID.String(),
			})
		})

		r.Route("/{id}", func(r chi.Router) {
			r.Get("/messages", func(w http.ResponseWriter, r *http.Request) {
				// query params:
				// - limit (optional, default 50, max 100)
				// - before (optional, message ID to paginate before)
				// - after (optional, message ID to paginate after)
				// - around (optional, message ID to paginate around, returns messages before and after the given ID)

				user, err := authhelper.GetUserFromRequest(r)
				if err != nil || user == nil {
					httpresponder.SendErrorResponse(w, r, "You are not logged in!", http.StatusUnauthorized)
					return
				}

				// get id of conversation from URL
				conversationID := chi.URLParam(r, "id")

				// validate conversation ID as UUID
				convUUID, err := uuid.FromString(conversationID)
				if err != nil {
					httpresponder.SendErrorResponse(w, r, "Invalid conversation ID format!", http.StatusBadRequest)
					return
				}

				// verify user is a participant in this conversation
				var participant database.DMParticipant
				err = database.DB.
					Where("conversation_id = ? AND user_id = ?", convUUID, user.ID).
					First(&participant).Error

				if err != nil {
					httpresponder.SendErrorResponse(w, r, "Conversation not found or you are not a participant!", http.StatusNotFound)
					return
				}

				// get query params
				limitStr := r.URL.Query().Get("limit")
				before := r.URL.Query().Get("before")
				after := r.URL.Query().Get("after")
				around := r.URL.Query().Get("around")

				// set default limit
				limit := 50
				if limitStr != "" {
					limitInt, err := strconv.Atoi(limitStr)
					if err != nil || limitInt <= 0 || limitInt > 100 {
						httpresponder.SendErrorResponse(w, r, "Invalid limit value! Must be a number between 1 and 100.", http.StatusBadRequest)
						return
					}
					limit = limitInt
				}

				var messages []database.DirectMessage

				// build query based on pagination params
				query := database.DB.
					Where("conversation_id = ?", convUUID).
					Preload("Author")

				if around != "" {
					// around pagination: get messages before and after the given ID
					aroundUUID, err := uuid.FromString(around)
					if err != nil {
						httpresponder.SendErrorResponse(w, r, "Invalid 'around' message ID format!", http.StatusBadRequest)
						return
					}

					// get the reference message to find its created_at
					var refMessage database.DirectMessage
					err = database.DB.Where("id = ? AND conversation_id = ?", aroundUUID, convUUID).First(&refMessage).Error
					if err != nil {
						httpresponder.SendErrorResponse(w, r, "Reference message not found!", http.StatusNotFound)
						return
					}

					halfLimit := limit / 2

					// get messages before (older)
					var beforeMessages []database.DirectMessage
					database.DB.
						Where("conversation_id = ? AND created_at < ?", convUUID, refMessage.CreatedAt).
						Order("created_at DESC").
						Limit(halfLimit).
						Preload("Author").
						Find(&beforeMessages)

					// get messages after (newer), including the reference message
					var afterMessages []database.DirectMessage
					database.DB.
						Where("conversation_id = ? AND created_at >= ?", convUUID, refMessage.CreatedAt).
						Order("created_at ASC").
						Limit(limit - halfLimit).
						Preload("Author").
						Find(&afterMessages)

					// combine: reverse beforeMessages and append afterMessages
					messages = make([]database.DirectMessage, 0, len(beforeMessages)+len(afterMessages))
					for i := len(beforeMessages) - 1; i >= 0; i-- {
						messages = append(messages, beforeMessages[i])
					}
					messages = append(messages, afterMessages...)

				} else if before != "" {
					// before pagination: get messages older than the given ID
					beforeUUID, err := uuid.FromString(before)
					if err != nil {
						httpresponder.SendErrorResponse(w, r, "Invalid 'before' message ID format!", http.StatusBadRequest)
						return
					}

					// get the reference message
					var refMessage database.DirectMessage
					err = database.DB.Where("id = ? AND conversation_id = ?", beforeUUID, convUUID).First(&refMessage).Error
					if err != nil {
						httpresponder.SendErrorResponse(w, r, "Reference message not found!", http.StatusNotFound)
						return
					}

					err = query.
						Where("created_at < ?", refMessage.CreatedAt).
						Order("created_at DESC").
						Limit(limit).
						Find(&messages).Error

					if err != nil {
						httpresponder.SendErrorResponse(w, r, "Failed to fetch messages!", http.StatusInternalServerError)
						return
					}

					// reverse to get chronological order
					for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
						messages[i], messages[j] = messages[j], messages[i]
					}

				} else if after != "" {
					// after pagination: get messages newer than the given ID
					afterUUID, err := uuid.FromString(after)
					if err != nil {
						httpresponder.SendErrorResponse(w, r, "Invalid 'after' message ID format!", http.StatusBadRequest)
						return
					}

					// get the reference message
					var refMessage database.DirectMessage
					err = database.DB.Where("id = ? AND conversation_id = ?", afterUUID, convUUID).First(&refMessage).Error
					if err != nil {
						httpresponder.SendErrorResponse(w, r, "Reference message not found!", http.StatusNotFound)
						return
					}

					err = query.
						Where("created_at > ?", refMessage.CreatedAt).
						Order("created_at ASC").
						Limit(limit).
						Find(&messages).Error

					if err != nil {
						httpresponder.SendErrorResponse(w, r, "Failed to fetch messages!", http.StatusInternalServerError)
						return
					}

				} else {
					// no pagination: get most recent messages
					err = query.
						Order("created_at DESC").
						Limit(limit).
						Find(&messages).Error

					if err != nil {
						httpresponder.SendErrorResponse(w, r, "Failed to fetch messages!", http.StatusInternalServerError)
						return
					}

					// reverse to get chronological order
					for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
						messages[i], messages[j] = messages[j], messages[i]
					}
				}

				// build response
				response := make([]messageResponse, 0, len(messages))
				for _, msg := range messages {
					msgResp := messageResponse{
						ID:          msg.ID.String(),
						Content:     msg.Content,
						Attachments: msg.Attachments,
						Author: authorBrief{
							ID:       msg.Author.ID.String(),
							Username: msg.Author.Username,
							Domain:   msg.Author.Domain,
						},
						CreatedAt: msg.CreatedAt,
						EditedAt:  msg.EditedAt,
					}

					if msg.ReplyToID != nil {
						replyID := msg.ReplyToID.String()
						msgResp.ReplyToID = &replyID
					}

					response = append(response, msgResp)
				}

				httpresponder.SendSuccessResponse(w, r, response)
			})
		})
	})
}

// notifyNewGroupDM notifies all participants of a new group DM and subscribes them to the conversation
func notifyNewGroupDM(conv *database.DMConversation, participants []database.DMParticipant, creator *database.User) {
	hub := websocket.GetHub()
	print("Notifying new group DM to participants: ", len(participants))
	if hub == nil {
		print("no hub?")
		return
	}

	// fetch all participant users for the response payload
	var participantUserIDs []uuid.UUID
	for _, p := range participants {
		participantUserIDs = append(participantUserIDs, p.UserID)
	}

	var participantUsers []database.User
	database.DB.Where("id IN ?", participantUserIDs).Find(&participantUsers)

	// build participants list for the payload
	participantsList := make([]map[string]any, 0, len(participantUsers))
	for _, u := range participantUsers {
		print("Adding participant to payload: ", u.Username)
		participantsList = append(participantsList, map[string]any{
			"id":       u.ID.String(),
			"username": u.Username,
			"domain":   u.Domain,
		})
	}

	print("all users fetched for payload: ", len(participantsList))

	payload := map[string]any{
		"conversation_id": conv.ID.String(),
		"name":            conv.Name,
		"is_group":        conv.IsGroup,
		"participants":    participantsList,
		"created_by": map[string]any{
			"id":       creator.ID.String(),
			"username": creator.Username,
			"domain":   creator.Domain,
		},
	}

	// notify each participant and subscribe them to the conversation
	for _, participant := range participants {
		hub.DispatchToUser(participant.UserID, websocket.EventDMCreate, payload)

		// subscribe all of the user's clients to the new conversation
		for _, client := range hub.GetUserClients(participant.UserID) {
			hub.SubscribeToConversation(client, conv.ID)
		}
	}
}
