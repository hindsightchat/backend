package authroutes

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hindsightchat/backend/src/lib/authhelper"
	database "github.com/hindsightchat/backend/src/lib/dbs/tidb"
	"github.com/hindsightchat/backend/src/lib/httpresponder"
	uuid "github.com/satori/go.uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type RegisterRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email"`
}

type simpleUser struct {
	ID               string `json:"id"`
	Username         string `json:"username"`
	Domain           string `json:"domain"`
	Email            string `json:"email"`
	IsDomainVerified bool   `json:"isDomainVerified"`
	Token            string `json:"token,omitempty"`
	ProfilePicURL    string `json:"profilePicURL,omitempty"`
}

func isValidDomain(domain string) bool {
	// check if valid domain format e.g has no spaces and only contains letters, numbers, and hyphens and a .
	for _, char := range domain {
		if !(char >= 'a' && char <= 'z') && !(char >= '0' && char <= '9') && char != '-' && char != '.' {
			return false
		}
	}

	// ensure domain has at least one dot and the last part is at least 2 characters
	lastDot := -1
	for i, char := range domain {
		if char == '.' {
			lastDot = i
		}
	}

	if lastDot == -1 || len(domain)-lastDot-1 < 2 {
		return false
	}

	// ensure domain has no spaces
	for _, char := range domain {
		if char == ' ' {
			return false
		}
	}

	// ensure domain is not too long
	if len(domain) > 253 {
		return false
	}

	return true
}

func RegisterRoutes(r chi.Router) {
	r.Route("/auth", func(r chi.Router) {
		r.Get("/me", func(w http.ResponseWriter, r *http.Request) {
			user, err := authhelper.GetUserFromRequest(r)

			if err != nil {
				httpresponder.SendErrorResponse(w, r, "Unauthorized", http.StatusUnauthorized)
				return
			}

			httpresponder.SendSuccessResponse(w, r, simpleUser{
				ID:               user.ID.String(),
				Username:         user.Username,
				Domain:           user.Domain,
				Email:            user.Email,
				IsDomainVerified: user.IsDomainVerified,
				ProfilePicURL:    user.ProfilePicURL,
			})

		})

		r.Post("/login", func(w http.ResponseWriter, r *http.Request) {
			authToken, ok := r.Context().Value("authToken").(string)

			if ok && authToken != "" {
				httpresponder.SendErrorResponse(w, r, "Already logged in", http.StatusBadRequest)
				return
			}

			var body loginRequest
			err := json.NewDecoder(r.Body).Decode(&body)

			if err != nil {
				httpresponder.SendErrorResponse(w, r, "Invalid request body", http.StatusBadRequest)
				return
			}

			if body.Email == "" || body.Password == "" {
				httpresponder.SendErrorResponse(w, r, "Email and password are required", http.StatusBadRequest)
				return
			}

			user, err := gorm.G[database.User](database.DB).Where("email = ?", body.Email).First(r.Context())

			if err != nil {
				// invalid email
				httpresponder.SendErrorResponse(w, r, "Invalid email or password", http.StatusUnauthorized)
				return
			}

			err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(body.Password))

			if err != nil {
				// invalid password
				httpresponder.SendErrorResponse(w, r, "Invalid email or password", http.StatusUnauthorized)
				return
			}

			// create auth token and save to database

			token := uuid.NewV4()

			userToken := database.UserToken{
				UserID:    user.ID,
				Token:     token.String(),
				ExpiresAt: time.Now().Add(7 * 24 * time.Hour).Unix(), // expires in 7 days
			}

			err = gorm.G[database.UserToken](database.DB).Create(r.Context(), &userToken)

			if err != nil {
				httpresponder.SendErrorResponse(w, r, "Failed to create auth token", http.StatusInternalServerError)
				return
			}

			// set cookie

			http.SetCookie(w, &http.Cookie{
				Name:     "rm_authToken",
				Value:    token.String(),
				Expires:  time.Unix(userToken.ExpiresAt, 0),
				HttpOnly: false,
				// Path as root
				Path: "/",
			})

			returnUser := simpleUser{
				ID:               user.ID.String(),
				Username:         user.Username,
				Domain:           user.Domain,
				Email:            user.Email,
				IsDomainVerified: user.IsDomainVerified,
				Token:            token.String(),
			}

			httpresponder.SendSuccessResponse(w, r, returnUser)
		})

		r.Post("/register", func(w http.ResponseWriter, r *http.Request) {
			// check authToken
			authToken, ok := r.Context().Value("authToken").(string)

			if ok && authToken != "" {
				httpresponder.SendErrorResponse(w, r, "Already logged in", http.StatusBadRequest)
				return
			}

			var body RegisterRequest
			err := json.NewDecoder(r.Body).Decode(&body)

			if err != nil {
				httpresponder.SendErrorResponse(w, r, "Invalid request body", http.StatusBadRequest)
				return
			}

			// domain will be defaulted to hindsight.chat for now
			domain := "hindsight.chat"

			if body.Username == "" || body.Password == "" || body.Email == "" {
				httpresponder.SendErrorResponse(w, r, "Username, password, and email are required", http.StatusBadRequest)
				return
			}

			// check if valid domain format e.g has no spaces and only contains letters, numbers, and hyphens and a .

			if !isValidDomain(domain) {
				httpresponder.SendErrorResponse(w, r, "Invalid domain format", http.StatusBadRequest)
				return
			}

			// check if email already exists

			realuser, err := gorm.G[database.User](database.DB).Where("email = ? OR username = ?", body.Email, body.Username+"."+domain).First(r.Context())
			if err == nil && realuser.ID != uuid.Nil {
				httpresponder.SendErrorResponse(w, r, "Email or username already in use", http.StatusBadRequest)
				return
			}

			hashedPassword, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)

			if err != nil {
				httpresponder.SendErrorResponse(w, r, "Failed to hash password", http.StatusInternalServerError)
				return
			}

			user := database.User{
				Username:         body.Username + "." + domain, // username will be username.domain so e.g "robbie.hindsig.ht"
				Password:         string(hashedPassword),
				Email:            body.Email,
				Domain:           domain,
				IsDomainVerified: true, // default true since this is our domain
			}

			err = gorm.G[database.User](database.DB).Create(r.Context(), &user)

			if err != nil {
				httpresponder.SendErrorResponse(w, r, "Failed to create user: "+err.Error(), http.StatusInternalServerError)
				return
			}

			// create token

			token := uuid.NewV4()

			userToken := database.UserToken{
				UserID:    user.ID,
				Token:     token.String(),
				ExpiresAt: time.Now().Add(7 * 24 * time.Hour).Unix(), // expires in 7 days
			}

			err = gorm.G[database.UserToken](database.DB).Create(r.Context(), &userToken)

			if err != nil {
				httpresponder.SendErrorResponse(w, r, "Failed to create auth token", http.StatusInternalServerError)
				return
			}

			// set cookie

			http.SetCookie(w, &http.Cookie{
				Name:     "rm_authToken",
				Value:    token.String(),
				Expires:  time.Unix(userToken.ExpiresAt, 0),
				HttpOnly: true,
			})

			returnUser := simpleUser{
				ID:               user.ID.String(),
				Username:         user.Username,
				Domain:           user.Domain,
				Email:            user.Email,
				IsDomainVerified: user.IsDomainVerified,
				Token:            token.String(),
				ProfilePicURL:    user.ProfilePicURL,
			}

			httpresponder.SendSuccessResponse(w, r, returnUser)

		})
	})
}
