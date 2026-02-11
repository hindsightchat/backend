package conversationroutes

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hindsightchat/backend/src/lib/authhelper"
	database "github.com/hindsightchat/backend/src/lib/dbs/tidb"
	"github.com/hindsightchat/backend/src/lib/httpresponder"
	"github.com/hindsightchat/backend/src/middleware"
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

func RegisterRoutes(r chi.Router) {
	r.Route("/conversation", func(r chi.Router) {
		r.Use(middleware.RouteRequiresAuthentication)

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
