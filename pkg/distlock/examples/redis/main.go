// Command redis demonstrates distlock.NewRedisLocker against a Redis server.
//
// Configuration is read from the environment so that no credentials live in the
// repository:
//
//	ONEX_REDIS_ADDR      host:port of the Redis server (default 127.0.0.1:6379)
//	ONEX_REDIS_USERNAME  optional ACL user
//	ONEX_REDIS_PASSWORD  optional password
//	ONEX_REDIS_DATABASE  optional database number (default 0)
//
// Example:
//
//	ONEX_REDIS_ADDR=127.0.0.1:6379 go run ./pkg/distlock/examples/redis
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/LiangNing7/goutils/pkg/db"
	"github.com/LiangNing7/goutils/pkg/distlock"
)

func main() {
	database := 0
	if raw := os.Getenv("ONEX_REDIS_DATABASE"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			log.Fatalf("ONEX_REDIS_DATABASE=%q is not a number: %v", raw, err)
		}
		database = parsed
	}

	opts := &db.RedisOptions{
		Addr:     envOr("ONEX_REDIS_ADDR", "127.0.0.1:6379"),
		Username: os.Getenv("ONEX_REDIS_USERNAME"),
		Password: os.Getenv("ONEX_REDIS_PASSWORD"),
		Database: database,
	}

	client, err := db.NewRedis(opts)
	if err != nil {
		log.Fatalf("failed to connect to redis at %s: %v", opts.Addr, err)
	}

	locker := distlock.NewRedisLocker(client)
	defer func() {
		// Close stops the renewal goroutine; the redis client belongs to us here
		// and is closed by the application.
		if err := locker.Close(); err != nil {
			fmt.Printf("failed to close locker: %v\n", err)
		}
	}()

	ctx := context.Background()

	// Retry until the lock is acquired.
	for {
		if err := locker.Lock(ctx); err != nil {
			fmt.Printf("failed to acquire lock: %v, retrying...\n", err)
			time.Sleep(1 * time.Second)
			continue
		}
		fmt.Println("Lock acquired!")
		break
	}

	// Simulate the work that needs the lock.
	time.Sleep(10 * time.Second)

	if err := locker.Unlock(ctx); err != nil {
		fmt.Printf("failed to release lock: %v\n", err)
	} else {
		fmt.Println("Lock released!")
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
