package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/rmcord/backend/src/lib/authhelper"
)

// CaseSensitiveMiddleware is a middleware that makes all URL paths lowercase to ensure case insensitivity.
// its default within the crm
// Preserves case for collection names in /collection/{name} patterns
func CaseSensitiveMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		for i, part := range parts {
			if i > 0 && strings.ToLower(parts[i-1]) == "collection" {
				continue
			}
			parts[i] = strings.ToLower(part)
		}
		r.URL.Path = strings.Join(parts, "/")
		next.ServeHTTP(w, r)
	})
}

// SaveAuthTokenMiddleware is a middleware that saves the auth token from cookies or headers into the request context.
func SaveAuthTokenMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		cookies := r.Cookies()
		ctx := r.Context()

		// find auth cookie called "authToken"

		for _, cookie := range cookies {
			if cookie.Name == "rm_authToken" {
				// save to context for later use
				ctx = context.WithValue(ctx, "authToken", cookie.Value)
				break
			}
		}

		// if no cookie, check authorization header
		if ctx.Value("authToken") == nil {
			authHeader := r.Header.Get("Authorization")
			if authHeader != "" {
				ctx = context.WithValue(ctx, "authToken", strings.Replace(authHeader, "Bearer ", "", 1))
			}
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func RouteRequiresAuthentication(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		authToken := ctx.Value("authToken").(string)

		if authToken == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// check if auth token is valid by looking it up in the database

		userID, err := authhelper.GetUserIDFromToken(authToken)

		if err != nil || userID == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// save userID to context for later use
		ctx = context.WithValue(ctx, "userID", userID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
