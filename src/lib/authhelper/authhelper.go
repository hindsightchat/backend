package authhelper

import (
	"context"
	"errors"
	"net/http"
	"time"

	usercache "github.com/hindsightchat/backend/src/lib/cache/user"
	database "github.com/hindsightchat/backend/src/lib/dbs/tidb"
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
		authToken, ok := ctx.Value("authToken").(string)
		if !ok {
			return nil, errors.New("auth token not found in context")
		}

		if authToken == "" {
			return nil, errors.New("auth token not found in context")
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
			return nil, errors.New("user not found")
		}
		return nil, err
	}

	// save to cache
	usercache.UserCacheInstance.Set(userID, user)

	return &user, nil
}
