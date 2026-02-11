package serverroutes

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hindsightchat/backend/src/lib/authhelper"
	database "github.com/hindsightchat/backend/src/lib/dbs/tidb"
	"github.com/hindsightchat/backend/src/lib/httpresponder"
	"github.com/hindsightchat/backend/src/middleware"
	uuid "github.com/satori/go.uuid"
)

type serverResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Icon        string    `json:"icon,omitempty"`
	OwnerID     string    `json:"owner_id"`
	JoinedAt    time.Time `json:"joined_at"`
}

func RegisterRoutes(r chi.Router) {
	r.Route("/servers", func(r chi.Router) {
		r.Use(middleware.RouteRequiresAuthentication)

		r.Route("/{id}", func(r chi.Router) {

			// get channels
			r.Get("/channels", GetServerChannels)

			// get specific server info
			r.Get("/", func(w http.ResponseWriter, r *http.Request) {
				user, err := authhelper.GetUserFromRequest(r)
				if err != nil || user == nil {
					httpresponder.SendErrorResponse(w, r, "unauthorized", http.StatusUnauthorized)
					return
				}

				serverID, err := uuid.FromString(chi.URLParam(r, "id"))
				if err != nil {
					httpresponder.SendErrorResponse(w, r, "invalid server id", http.StatusBadRequest)
					return
				}

				// verify membership
				var membership database.ServerMember
				err = database.DB.Where("server_id = ? AND user_id = ?", serverID, user.ID).First(&membership).Error

				if err != nil {
					httpresponder.SendErrorResponse(w, r, "not a member of this server", http.StatusForbidden)
					return
				}

				var server database.Server
				err = database.DB.Where("id = ?", serverID).First(&server).Error

				if err != nil {
					httpresponder.SendErrorResponse(w, r, "server not found", http.StatusNotFound)
					return
				}

				httpresponder.SendSuccessResponse(w, r, serverResponse{
					ID:          server.ID.String(),
					Name:        server.Name,
					Description: server.Description,
					Icon:        server.Icon,
					OwnerID:     server.OwnerID.String(),
					JoinedAt:    membership.CreatedAt,
				})
			})
		})
	})

}

type channelResponse struct {
	ID          string `json:"id"`
	ServerID    string `json:"server_id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Type        int    `json:"type"`
	Position    int    `json:"position"`
}

// get specific server's channels
func GetServerChannels(w http.ResponseWriter, r *http.Request) {
	user, err := authhelper.GetUserFromRequest(r)
	if err != nil || user == nil {
		httpresponder.SendErrorResponse(w, r, "unauthorized", http.StatusUnauthorized)
		return
	}

	serverID, err := uuid.FromString(chi.URLParam(r, "id"))
	if err != nil {
		httpresponder.SendErrorResponse(w, r, "invalid server id", http.StatusBadRequest)
		return
	}

	// verify membership
	var membership database.ServerMember
	err = database.DB.Where("server_id = ? AND user_id = ?", serverID, user.ID).First(&membership).Error
	if err != nil {
		httpresponder.SendErrorResponse(w, r, "not a member of this server", http.StatusForbidden)
		return
	}

	var channels []database.Channel
	err = database.DB.
		Where("server_id = ?", serverID).
		Order("position ASC").
		Find(&channels).Error

	if err != nil {
		httpresponder.SendErrorResponse(w, r, "failed to fetch channels", http.StatusInternalServerError)
		return
	}

	response := make([]channelResponse, 0, len(channels))
	for _, c := range channels {
		response = append(response, channelResponse{
			ID:          c.ID.String(),
			ServerID:    c.ServerID.String(),
			Name:        c.Name,
			Description: c.Description,
			Type:        c.Type,
			Position:    c.Position,
		})
	}

	httpresponder.SendSuccessResponse(w, r, response)
}
