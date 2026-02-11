package valkeydb

import (
	"context"
	"os"

	"github.com/redis/go-redis/v9"
)

var rdb *redis.Client


var (
	USER_CACHE_PREFIX = "user_cache:"
	PRESENCE_PREFIX    = "presence:"
)

func GetValkeyClient() *redis.Client {
	return rdb
}

func WaitUntilReady() {

	valkeyURL := os.Getenv("VALKEY_URL")

	rdb = redis.NewClient(&redis.Options{
		Addr:     valkeyURL,
		Password: os.Getenv("VALKEY_PASSWORD"), // no password set
		DB:       0,                            // use default DB
	})

	println("Waiting until valkey is ready...")
	ctx := context.Background()
	for {
		_, err := rdb.Ping(ctx).Result()
		if err == nil {
			break
		}
	}

	println("valkey is ready!")
}
