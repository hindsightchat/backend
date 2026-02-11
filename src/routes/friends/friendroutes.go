package friendroutes

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hindsightchat/backend/src/lib/authhelper"
	database "github.com/hindsightchat/backend/src/lib/dbs/tidb"
	"github.com/hindsightchat/backend/src/lib/httpresponder"
	websocket "github.com/hindsightchat/backend/src/routes/websocket"
	uuid "github.com/satori/go.uuid"
)

type sendRequestBody struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"` // alternative: username@domain
}

type friendRequestResponse struct {
	ID        string    `json:"id"`
	Sender    userBrief `json:"sender"`
	Receiver  userBrief `json:"receiver"`
	Status    int       `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type friendshipResponse struct {
	ID             string    `json:"id"`
	User           userBrief `json:"user"`
	ConversationID string    `json:"conversation_id"`
	Since          time.Time `json:"since"`
}

type userBrief struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Domain   string `json:"domain"`
}

func RegisterRoutes(r chi.Router) {
	r.Route("/friends", func(r chi.Router) {
		// get all friends
		r.Get("/", getFriends)

		// get pending requests (incoming)
		r.Get("/requests", getPendingRequests)

		// get outgoing requests
		r.Get("/requests/outgoing", getOutgoingRequests)

		// send friend request
		r.Post("/requests", sendFriendRequest)

		// accept friend request
		r.Post("/requests/{id}/accept", acceptFriendRequest)

		// decline friend request
		r.Post("/requests/{id}/decline", declineFriendRequest)

		// cancel outgoing request
		r.Delete("/requests/{id}", cancelFriendRequest)

		// remove friend
		r.Delete("/{id}", removeFriend)
	})
}

func getFriends(w http.ResponseWriter, r *http.Request) {
	user, err := authhelper.GetUserFromRequest(r)
	if err != nil || user == nil {
		httpresponder.SendErrorResponse(w, r, "unauthorized", http.StatusUnauthorized)
		return
	}

	var friendships []database.Friendship
	err = database.DB.
		Preload("User1").
		Preload("User2").
		Where("user1_id = ? OR user2_id = ?", user.ID, user.ID).
		Find(&friendships).Error

	if err != nil {
		httpresponder.SendErrorResponse(w, r, "failed to fetch friends", http.StatusInternalServerError)
		return
	}

	friends := make([]friendshipResponse, 0, len(friendships))
	for _, f := range friendships {
		var friend database.User
		if f.User1ID == user.ID {
			friend = f.User2
		} else {
			friend = f.User1
		}

		friends = append(friends, friendshipResponse{
			ID:             f.ID.String(),
			ConversationID: f.ConversationID.String(),
			Since:          f.CreatedAt,
			User: userBrief{
				ID:       friend.ID.String(),
				Username: friend.Username,
				Domain:   friend.Domain,
			},
		})
	}

	httpresponder.SendSuccessResponse(w, r, friends)
}

func getPendingRequests(w http.ResponseWriter, r *http.Request) {
	user, err := authhelper.GetUserFromRequest(r)
	if err != nil || user == nil {
		httpresponder.SendErrorResponse(w, r, "unauthorized", http.StatusUnauthorized)
		return
	}

	var requests []database.FriendRequest
	err = database.DB.
		Preload("Sender").
		Preload("Receiver").
		Where("receiver_id = ? AND status = ?", user.ID, database.FriendRequestPending).
		Find(&requests).Error

	if err != nil {
		httpresponder.SendErrorResponse(w, r, "failed to fetch requests", http.StatusInternalServerError)
		return
	}

	response := make([]friendRequestResponse, 0, len(requests))
	for _, req := range requests {
		response = append(response, friendRequestResponse{
			ID:        req.ID.String(),
			Status:    int(req.Status),
			CreatedAt: req.CreatedAt,
			Sender: userBrief{
				ID:       req.Sender.ID.String(),
				Username: req.Sender.Username,
				Domain:   req.Sender.Domain,
			},
			Receiver: userBrief{
				ID:       req.Receiver.ID.String(),
				Username: req.Receiver.Username,
				Domain:   req.Receiver.Domain,
			},
		})
	}

	httpresponder.SendSuccessResponse(w, r, response)
}

func getOutgoingRequests(w http.ResponseWriter, r *http.Request) {
	user, err := authhelper.GetUserFromRequest(r)
	if err != nil || user == nil {
		httpresponder.SendErrorResponse(w, r, "unauthorized", http.StatusUnauthorized)
		return
	}

	var requests []database.FriendRequest
	err = database.DB.
		Preload("Sender").
		Preload("Receiver").
		Where("sender_id = ? AND status = ?", user.ID, database.FriendRequestPending).
		Find(&requests).Error

	if err != nil {
		httpresponder.SendErrorResponse(w, r, "failed to fetch requests", http.StatusInternalServerError)
		return
	}

	response := make([]friendRequestResponse, 0, len(requests))
	for _, req := range requests {
		response = append(response, friendRequestResponse{
			ID:        req.ID.String(),
			Status:    int(req.Status),
			CreatedAt: req.CreatedAt,
			Sender: userBrief{
				ID:       req.Sender.ID.String(),
				Username: req.Sender.Username,
				Domain:   req.Sender.Domain,
			},
			Receiver: userBrief{
				ID:       req.Receiver.ID.String(),
				Username: req.Receiver.Username,
				Domain:   req.Receiver.Domain,
			},
		})
	}

	httpresponder.SendSuccessResponse(w, r, response)
}

func sendFriendRequest(w http.ResponseWriter, r *http.Request) {
	user, err := authhelper.GetUserFromRequest(r)
	if err != nil || user == nil {
		httpresponder.SendErrorResponse(w, r, "unauthorized", http.StatusUnauthorized)
		return
	}

	var body sendRequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpresponder.SendErrorResponse(w, r, "invalid request body", http.StatusBadRequest)
		return
	}

	var targetUser database.User

	// find target user by id or username
	if body.UserID != "" {
		targetID, err := uuid.FromString(body.UserID)
		if err != nil {
			httpresponder.SendErrorResponse(w, r, "invalid user id", http.StatusBadRequest)
			return
		}
		if err := database.DB.Where("id = ?", targetID).First(&targetUser).Error; err != nil {
			httpresponder.SendErrorResponse(w, r, "user not found", http.StatusNotFound)
			return
		}
	} else if body.Username != "" {
		// username can be "user.domain" or "user@domain"
		// depends what we choose at the end lol
		username := strings.Replace(body.Username, "@", ".", 1)
		if err := database.DB.Where("username = ?", username).First(&targetUser).Error; err != nil {
			httpresponder.SendErrorResponse(w, r, "user not found", http.StatusNotFound)
			return
		}
	} else {
		httpresponder.SendErrorResponse(w, r, "user_id or username required", http.StatusBadRequest)
		return
	}

	// cant friend yourself
	if targetUser.ID == user.ID {
		httpresponder.SendErrorResponse(w, r, "cannot send friend request to yourself", http.StatusBadRequest)
		return
	}

	fmt.Printf("User %s (%s) is sending friend request to %s (%s)\n", user.Username, user.ID.String(), targetUser.Username, targetUser.ID.String())

	// check if already friends
	var existingFriendship database.Friendship
	user1ID, user2ID := orderUserIDs(user.ID, targetUser.ID)
	err = database.DB.Where("user1_id = ? AND user2_id = ?", user1ID, user2ID).First(&existingFriendship).Error
	if err == nil {
		httpresponder.SendErrorResponse(w, r, "already friends", http.StatusBadRequest)
		return
	}

	// check if request already exists (either direction)
	var existingRequest database.FriendRequest
	err = database.DB.Where(
		"((sender_id = ? AND receiver_id = ?) OR (sender_id = ? AND receiver_id = ?)) AND status = ?",
		user.ID, targetUser.ID, targetUser.ID, user.ID, database.FriendRequestPending,
	).First(&existingRequest).Error

	if err == nil {
		// if they sent us a request, auto-accept it
		if existingRequest.SenderID == targetUser.ID {
			acceptRequest(w, r, user, &existingRequest, &targetUser)
			return
		}
		httpresponder.SendErrorResponse(w, r, "friend request already sent", http.StatusBadRequest)
		return
	}

	// create new request
	request := database.FriendRequest{
		SenderID:   user.ID,
		ReceiverID: targetUser.ID,
		Status:     database.FriendRequestPending,
	}

	if err := database.DB.Create(&request).Error; err != nil {
		httpresponder.SendErrorResponse(w, r, "failed to create request", http.StatusInternalServerError)
		return
	}

	// notify target via websocket
	notifyFriendRequest(&request, user, &targetUser)

	httpresponder.SendSuccessResponse(w, r, friendRequestResponse{
		ID:        request.ID.String(),
		Status:    int(request.Status),
		CreatedAt: request.CreatedAt,
		Sender: userBrief{
			ID:       user.ID.String(),
			Username: user.Username,
			Domain:   user.Domain,
		},
		Receiver: userBrief{
			ID:       targetUser.ID.String(),
			Username: targetUser.Username,
			Domain:   targetUser.Domain,
		},
	})
}

func acceptFriendRequest(w http.ResponseWriter, r *http.Request) {
	user, err := authhelper.GetUserFromRequest(r)
	if err != nil || user == nil {
		httpresponder.SendErrorResponse(w, r, "unauthorized", http.StatusUnauthorized)
		return
	}

	requestID, err := uuid.FromString(chi.URLParam(r, "id"))
	if err != nil {
		httpresponder.SendErrorResponse(w, r, "invalid request id", http.StatusBadRequest)
		return
	}

	var request database.FriendRequest
	err = database.DB.Preload("Sender").Where("id = ? AND receiver_id = ? AND status = ?",
		requestID, user.ID, database.FriendRequestPending).First(&request).Error

	if err != nil {
		httpresponder.SendErrorResponse(w, r, "request not found", http.StatusNotFound)
		return
	}

	acceptRequest(w, r, user, &request, &request.Sender)
}

func acceptRequest(w http.ResponseWriter, r *http.Request, user *database.User, request *database.FriendRequest, otherUser *database.User) {
	// re-fetch both users to ensure they exist and have correct data
	var verifiedUser database.User
	if err := database.DB.Where("id = ?", user.ID).First(&verifiedUser).Error; err != nil {
		httpresponder.SendErrorResponse(w, r, "user not found", http.StatusBadRequest)
		return
	}

	var verifiedOther database.User
	if err := database.DB.Where("id = ?", otherUser.ID).First(&verifiedOther).Error; err != nil {
		httpresponder.SendErrorResponse(w, r, "other user not found", http.StatusBadRequest)
		return
	}

	fmt.Printf("User %s (%s) is accepting a friend request from User 2 %s (%s)\n", verifiedUser.Username, verifiedUser.ID.String(), verifiedOther.Username, verifiedOther.ID.String())

	tx := database.DB.Begin()

	// update request status
	if err := tx.Model(request).Update("status", database.FriendRequestAccepted).Error; err != nil {
		tx.Rollback()
		httpresponder.SendErrorResponse(w, r, "failed to accept request", http.StatusInternalServerError)
		return
	}

	// create dm conversation
	conversation := database.DMConversation{
		IsGroup: false,
	}
	if err := tx.Create(&conversation).Error; err != nil {
		tx.Rollback()
		httpresponder.SendErrorResponse(w, r, "failed to create conversation", http.StatusInternalServerError)
		return
	}

	// add participants using verified user ids
	now := time.Now()
	participants := []database.DMParticipant{
		{ConversationID: conversation.ID, UserID: verifiedUser.ID, JoinedAt: now},
		{ConversationID: conversation.ID, UserID: verifiedOther.ID, JoinedAt: now},
	}
	if err := tx.Create(&participants).Error; err != nil {
		tx.Rollback()
		httpresponder.SendErrorResponse(w, r, "failed to add participants", http.StatusInternalServerError)
		return
	}

	fmt.Printf("Created conversation %s with participients: %s (%s) & %s (%s)\n", conversation.ID, verifiedUser.Username, verifiedUser.ID.String(), verifiedOther.Username, verifiedOther.ID.String())

	// create friendship
	user1ID, user2ID := orderUserIDs(verifiedUser.ID, verifiedOther.ID)
	friendship := database.Friendship{
		User1ID:        user1ID,
		User2ID:        user2ID,
		ConversationID: conversation.ID,
	}
	if err := tx.Create(&friendship).Error; err != nil {
		tx.Rollback()
		httpresponder.SendErrorResponse(w, r, "failed to create friendship", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit().Error; err != nil {
		httpresponder.SendErrorResponse(w, r, "failed to complete", http.StatusInternalServerError)
		return
	}

	// notify both users via websocket
	notifyFriendAccepted(&verifiedUser, &verifiedOther, &friendship, &conversation)

	httpresponder.SendSuccessResponse(w, r, friendshipResponse{
		ID:             friendship.ID.String(),
		ConversationID: conversation.ID.String(),
		Since:          friendship.CreatedAt,
		User: userBrief{
			ID:       verifiedOther.ID.String(),
			Username: verifiedOther.Username,
			Domain:   verifiedOther.Domain,
		},
	})
}

func declineFriendRequest(w http.ResponseWriter, r *http.Request) {
	user, err := authhelper.GetUserFromRequest(r)
	if err != nil || user == nil {
		httpresponder.SendErrorResponse(w, r, "unauthorized", http.StatusUnauthorized)
		return
	}

	requestID, err := uuid.FromString(chi.URLParam(r, "id"))
	if err != nil {
		httpresponder.SendErrorResponse(w, r, "invalid request id", http.StatusBadRequest)
		return
	}

	result := database.DB.Model(&database.FriendRequest{}).
		Where("id = ? AND receiver_id = ? AND status = ?", requestID, user.ID, database.FriendRequestPending).
		Update("status", database.FriendRequestDeclined)

	if result.RowsAffected == 0 {
		httpresponder.SendErrorResponse(w, r, "request not found", http.StatusNotFound)
		return
	}

	httpresponder.SendSuccessResponse(w, r, map[string]bool{"declined": true})
}

func cancelFriendRequest(w http.ResponseWriter, r *http.Request) {
	user, err := authhelper.GetUserFromRequest(r)
	if err != nil || user == nil {
		httpresponder.SendErrorResponse(w, r, "unauthorized", http.StatusUnauthorized)
		return
	}

	requestID, err := uuid.FromString(chi.URLParam(r, "id"))
	if err != nil {
		httpresponder.SendErrorResponse(w, r, "invalid request id", http.StatusBadRequest)
		return
	}

	result := database.DB.Where("id = ? AND sender_id = ? AND status = ?",
		requestID, user.ID, database.FriendRequestPending).
		Delete(&database.FriendRequest{})

	if result.RowsAffected == 0 {
		httpresponder.SendErrorResponse(w, r, "request not found", http.StatusNotFound)
		return
	}

	httpresponder.SendSuccessResponse(w, r, map[string]bool{"cancelled": true})
}

func removeFriend(w http.ResponseWriter, r *http.Request) {
	user, err := authhelper.GetUserFromRequest(r)
	if err != nil || user == nil {
		httpresponder.SendErrorResponse(w, r, "unauthorized", http.StatusUnauthorized)
		return
	}

	friendID, err := uuid.FromString(chi.URLParam(r, "id"))
	if err != nil {
		httpresponder.SendErrorResponse(w, r, "invalid friend id", http.StatusBadRequest)
		return
	}

	user1ID, user2ID := orderUserIDs(user.ID, friendID)

	var friendship database.Friendship
	err = database.DB.Where("user1_id = ? AND user2_id = ?", user1ID, user2ID).First(&friendship).Error
	if err != nil {
		httpresponder.SendErrorResponse(w, r, "friendship not found", http.StatusNotFound)
		return
	}

	// delete friendship (keep the dm conversation)
	if err := database.DB.Delete(&friendship).Error; err != nil {
		httpresponder.SendErrorResponse(w, r, "failed to remove friend", http.StatusInternalServerError)
		return
	}

	// notify both users
	notifyFriendRemoved(user.ID, friendID)

	httpresponder.SendSuccessResponse(w, r, map[string]bool{"removed": true})
}

// helpers

func orderUserIDs(a, b uuid.UUID) (uuid.UUID, uuid.UUID) {
	if a.String() < b.String() {
		return a, b
	}
	return b, a
}

func notifyFriendRequest(request *database.FriendRequest, sender, receiver *database.User) {
	hub := websocket.GetHub()
	if hub == nil {
		return
	}

	hub.DispatchToUser(receiver.ID, websocket.EventFriendRequestCreate, map[string]any{
		"id":         request.ID,
		"sender_id":  sender.ID,
		"created_at": request.CreatedAt,
		"sender": map[string]any{
			"id":       sender.ID,
			"username": sender.Username,
			"domain":   sender.Domain,
		},
	})
}

func notifyFriendAccepted(user, friend *database.User, friendship *database.Friendship, conversation *database.DMConversation) {
	hub := websocket.GetHub()
	if hub == nil {
		return
	}

	// notify both users about new friendship and dm
	payload := map[string]any{
		"friendship_id":   friendship.ID,
		"conversation_id": conversation.ID,
	}

	// notify the other user (who sent the request)
	hub.DispatchToUser(friend.ID, websocket.EventFriendRequestAccepted, map[string]any{
		"friendship_id":   friendship.ID,
		"conversation_id": conversation.ID,
		"user": map[string]any{
			"id":       user.ID,
			"username": user.Username,
			"domain":   user.Domain,
		},
	})

	// also dispatch dm create to both
	hub.DispatchToUser(user.ID, websocket.EventDMCreate, payload)
	hub.DispatchToUser(friend.ID, websocket.EventDMCreate, payload)

	// subscribe both to the new conversation
	for _, client := range hub.GetUserClients(user.ID) {
		hub.SubscribeToConversation(client, conversation.ID)
	}
	for _, client := range hub.GetUserClients(friend.ID) {
		hub.SubscribeToConversation(client, conversation.ID)
	}
}

func notifyFriendRemoved(userID, friendID uuid.UUID) {
	hub := websocket.GetHub()
	if hub == nil {
		return
	}

	hub.DispatchToUser(userID, websocket.EventFriendRemove, map[string]any{"user_id": friendID})
	hub.DispatchToUser(friendID, websocket.EventFriendRemove, map[string]any{"user_id": userID})
}
