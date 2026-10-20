// Command noop demonstrates distlock.NewNoopLocker.
//
// NoopLocker provides no mutual exclusion across processes: it only models the
// lifecycle locally. It is meant for tests and for wiring up an application
// before a real backend is chosen.
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/LiangNing7/goutils/pkg/distlock"
)

func main() {
	locker := distlock.NewNoopLocker()
	defer func() {
		if err := locker.Close(); err != nil {
			fmt.Printf("failed to close locker: %v\n", err)
		}
	}()

	ctx := context.Background()

	// Retry until the lock is acquired.
	for {
		if err := locker.Lock(ctx); err != nil {
			fmt.Printf("failed to acquire lock: %v, retrying...\n", err)
			// 等待一段时间后重试
			time.Sleep(1 * time.Second) // 可以根据需要调整等待时间
			continue                    // 继续尝试获取锁
		}
		fmt.Println("Lock acquired!")
		break // 成功获取锁后退出循环
	}

	// 模拟业务逻辑
	time.Sleep(10 * time.Second) // 修改为合理的时间，避免长时间阻塞

	if err := locker.Unlock(ctx); err != nil {
		fmt.Printf("failed to release lock: %v\n", err)
	} else {
		fmt.Println("Lock released!")
	}
}
