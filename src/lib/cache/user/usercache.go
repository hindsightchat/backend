package usercache

// in memory user cache implementation

import (
	"sync"
	"time"

	database "github.com/rmcord/backend/src/lib/dbs/tidb"
)

type UserCache struct {
	cache map[string]database.User
	mu    sync.RWMutex
	ttl   time.Duration
}

var UserCacheInstance *UserCache

func initUserCache(ttl time.Duration) {
	UserCacheInstance = &UserCache{
		cache: make(map[string]database.User),
		ttl:   ttl,
	}
}

func (uc *UserCache) Get(userID string) (database.User, bool) {
	uc.mu.RLock()
	defer uc.mu.RUnlock()
	user, exists := uc.cache[userID]
	return user, exists
}

func (uc *UserCache) Set(userID string, user database.User) {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	uc.cache[userID] = user
	// start a goroutine to delete the user after ttl
	go func() {
		time.Sleep(uc.ttl)
		uc.mu.Lock()
		defer uc.mu.Unlock()
		delete(uc.cache, userID)
	}()
}

func (uc *UserCache) Delete(userID string) {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	delete(uc.cache, userID)
}

func (uc *UserCache) Clear() {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	uc.cache = make(map[string]database.User)
}

func (uc *UserCache) Size() int {
	uc.mu.RLock()
	defer uc.mu.RUnlock()
	return len(uc.cache)
}

func (uc *UserCache) GetAllUsers() []database.User {
	uc.mu.RLock()
	defer uc.mu.RUnlock()
	users := make([]database.User, 0, len(uc.cache))
	for _, user := range uc.cache {
		users = append(users, user)
	}
	return users
}

func init() {
	initUserCache(5 * time.Minute)
}
