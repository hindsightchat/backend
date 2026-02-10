package authhelper

import (
	"context"
	"net/http"
	"time"

	database "github.com/rmcord/backend/src/dbs/tidb"
	usercache "github.com/rmcord/backend/src/lib/cache/user"
	"gorm.io/gorm"
)

func GetUserIDFromToken(token string) (string, error) {
	if token == "" {
		return "", nil
	}

	found, err := gorm.G[database.UserToken](database.DB).Where("token = ? AND expires_at > ?", token, time.Now().Unix()).First(context.Background())

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", nil
		}
		return "", err
	}

	return found.UserID.String(), nil
}

func GetUserFromRequest(r *http.Request) (*database.User, error) {
	ctx := r.Context()
	userID, ok := ctx.Value("userID").(string)

	if !ok || userID == "" {
		// try get userID from token if not in context
		authToken := ctx.Value("authToken").(string)

		if authToken == "" {
			return nil, nil
		}

		var err error
		userID, err = GetUserIDFromToken(authToken)
		if err != nil || userID == "" {
			return nil, err
		}
	}

	cachedUser, exists := usercache.UserCacheInstance.Get(userID)

	if exists {
		return &cachedUser, nil
	}

	user, err := gorm.G[database.User](database.DB).Where("id = ?", userID).First(context.Background())

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}

	// save to cache
	usercache.UserCacheInstance.Set(userID, user)

	return &user, nil
}
