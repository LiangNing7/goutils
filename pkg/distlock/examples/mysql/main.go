// Command mysql demonstrates distlock.NewGORMLocker against MySQL.
//
// Configuration is read from the environment so that no credentials live in the
// repository:
//
//	ONEX_MYSQL_ADDR      host:port of the MySQL server (default 127.0.0.1:3306)
//	ONEX_MYSQL_USERNAME  user name (default root)
//	ONEX_MYSQL_PASSWORD  password (REQUIRED, there is no safe default)
//	ONEX_MYSQL_DATABASE  database name (default onex)
//
// Example:
//
//	ONEX_MYSQL_ADDR=127.0.0.1:3306 ONEX_MYSQL_PASSWORD=secret \
//	    go run ./pkg/distlock/examples/mysql
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/LiangNing7/goutils/pkg/db"
	"github.com/LiangNing7/goutils/pkg/distlock"
)

func main() {
	password := os.Getenv("ONEX_MYSQL_PASSWORD")
	if password == "" {
		log.Fatal("ONEX_MYSQL_PASSWORD is required (this example does not ship credentials)")
	}

	opts := &db.MySQLOptions{
		Addr:     envOr("ONEX_MYSQL_ADDR", "127.0.0.1:3306"),
		Username: envOr("ONEX_MYSQL_USERNAME", "root"),
		Password: password,
		Database: envOr("ONEX_MYSQL_DATABASE", "onex"),
	}

	gormDB, err := db.NewMySQL(opts)
	if err != nil {
		log.Fatalf("failed to connect to database at %s: %v", opts.Addr, err)
	}

	locker, err := distlock.NewGORMLocker(gormDB)
	if err != nil {
		log.Fatalf("failed to create locker: %v", err)
	}
	defer func() {
		// Close stops the renewal goroutine; the *gorm.DB is shared and stays open.
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
