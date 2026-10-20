package watch

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/LiangNing7/goutils/pkg/distlock"
)

// recordLogger 记录日志消息，便于断言日志语义.
type recordLogger struct {
	mu     sync.Mutex
	infos  []string
	errs   []string
	debugs []string
}

func (l *recordLogger) Info(msg string, kvs ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.infos = append(l.infos, msg)
}

func (l *recordLogger) Error(err error, msg string, kvs ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.errs = append(l.errs, msg)
}

func (l *recordLogger) Debug(msg string, kvs ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.debugs = append(l.debugs, msg)
}

func (l *recordLogger) hasInfo(msg string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, m := range l.infos {
		if m == msg {
			return true
		}
	}
	return false
}

func (l *recordLogger) hasError(msg string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, m := range l.errs {
		if m == msg {
			return true
		}
	}
	return false
}

// deniedLocker 永远获取不到锁.
type deniedLocker struct{}

func (deniedLocker) Lock(context.Context) error   { return errors.New("lock denied") }
func (deniedLocker) Unlock(context.Context) error { return nil }
func (deniedLocker) Renew(context.Context) error  { return nil }

// grantedLocker 立即获取到锁，并记录是否被释放.
type grantedLocker struct {
	mu       sync.Mutex
	unlocked bool
}

func (l *grantedLocker) Lock(context.Context) error { return nil }
func (l *grantedLocker) Unlock(context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.unlocked = true
	return nil
}
func (l *grantedLocker) Renew(context.Context) error { return nil }

func (l *grantedLocker) isUnlocked() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.unlocked
}

// stubNewLocker 临时替换 newLocker，返回给定 locker 或错误.
func stubNewLocker(t *testing.T, locker distlock.Locker, err error) {
	t.Helper()

	old := newLocker
	newLocker = func(*gorm.DB, ...distlock.Option) (distlock.Locker, error) {
		return locker, err
	}
	t.Cleanup(func() { newLocker = old })
}

func newTestWatch(t *testing.T, logger Logger) *Watch {
	t.Helper()

	opts := NewOptions()
	// 关掉 healthz 端口，避免测试之间互相抢端口.
	opts.HealthzPort = 0

	w, err := NewWatch(opts, nil, WithLogger(logger))
	if err != nil {
		t.Fatalf("new watch: %v", err)
	}

	return w
}

// TestStartDoesNotPanicWhenLockerCreationFails 回归：AutoMigrate 失败时 locker 为 nil，
// 原实现会吞掉错误并在 Lock 时空指针 panic.
func TestStartDoesNotPanicWhenLockerCreationFails(t *testing.T) {
	logger := &recordLogger{}
	stubNewLocker(t, nil, errors.New("automigrate failed"))

	w := newTestWatch(t, logger)

	stopCh := make(chan struct{})
	done := make(chan struct{})

	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Start panicked when locker creation failed: %v", r)
			}
			close(done)
		}()
		w.Start(stopCh)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after locker creation failed")
	}

	if !logger.hasError("Failed to create distributed locker, watch server will not start") {
		t.Error("expected an error log about the failed locker creation")
	}
	if logger.hasInfo("Successfully started watch server") {
		t.Error("must not report a successful start when the locker could not be created")
	}
}

// TestStartReturnsWhenStoppedWhileWaitingForLock 回归：重试循环必须能响应停机信号.
func TestStartReturnsWhenStoppedWhileWaitingForLock(t *testing.T) {
	logger := &recordLogger{}
	stubNewLocker(t, deniedLocker{}, nil)

	w := newTestWatch(t, logger)

	stopCh := make(chan struct{})
	done := make(chan struct{})
	go func() {
		w.Start(stopCh)
		close(done)
	}()

	// 让 Start 至少完成一次获取锁的尝试.
	time.Sleep(200 * time.Millisecond)

	close(stopCh)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return after stopCh was closed while waiting for the lock")
	}

	if logger.hasInfo("Successfully started watch server") {
		t.Error("must not start the watch server when stopped while waiting for the lock")
	}
}

// TestStartReleasesLockWhenStoppedBeforeStart 回归：停机后才拿到锁时，必须释放锁且不启动任务.
func TestStartReleasesLockWhenStoppedBeforeStart(t *testing.T) {
	logger := &recordLogger{}
	locker := &grantedLocker{}
	stubNewLocker(t, locker, nil)

	w := newTestWatch(t, logger)

	stopCh := make(chan struct{})
	close(stopCh) // 已经请求停机

	done := make(chan struct{})
	go func() {
		w.Start(stopCh)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return")
	}

	if !locker.isUnlocked() {
		t.Error("lock acquired after shutdown was requested must be released")
	}
	if logger.hasInfo("Successfully started watch server") {
		t.Error("must not start the watch server when shutdown happened first")
	}
}

// TestStopWithoutLockerDoesNotPanic 回归：Start 未成功创建 locker 时 Stop 不能 panic.
func TestStopWithoutLockerDoesNotPanic(t *testing.T) {
	logger := &recordLogger{}
	w := newTestWatch(t, logger)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Stop panicked without a locker: %v", r)
		}
	}()

	w.Stop()
}

// TestHealthzLogsErrorInsteadOfSuccessOnBindFailure 回归：监听失败时不能打印
// "Successfully started health check server".
func TestHealthzLogsErrorInsteadOfSuccessOnBindFailure(t *testing.T) {
	// 占住通配地址，watch 的 serveHealthz 也绑定 0.0.0.0:port，必然失败.
	lis, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = lis.Close() }()

	port := lis.Addr().(*net.TCPAddr).Port

	opts := NewOptions()
	opts.HealthzPort = port

	logger := &recordLogger{}
	w, err := NewWatch(opts, nil, WithLogger(logger))
	if err != nil {
		t.Fatalf("new watch: %v", err)
	}

	done := make(chan struct{})
	go func() {
		w.serveHealthz(make(chan struct{}))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("serveHealthz did not return after the bind failure")
	}

	if logger.hasInfo("Successfully started health check server") {
		t.Error("must not report a successful start when the port could not be bound")
	}
	if !logger.hasError("Error serving health check endpoint") {
		t.Error("expected an error log about the failed bind")
	}
}

// TestHealthzServerStopsOnStopCh 回归：healthz 服务器可以随 stopCh 关停.
func TestHealthzServerStopsOnStopCh(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := lis.Addr().(*net.TCPAddr).Port
	_ = lis.Close()

	opts := NewOptions()
	opts.HealthzPort = port

	logger := &recordLogger{}
	w, err := NewWatch(opts, nil, WithLogger(logger))
	if err != nil {
		t.Fatalf("new watch: %v", err)
	}

	stopCh := make(chan struct{})
	go w.serveHealthz(stopCh)

	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", port)
	if err := waitForHTTP(t, url); err != nil {
		t.Fatalf("healthz never became available: %v", err)
	}

	close(stopCh)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		client := &http.Client{Timeout: 200 * time.Millisecond}
		resp, err := client.Get(url)
		if err != nil {
			return // 端口已经不再接受连接
		}
		_ = resp.Body.Close()
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatal("healthz server still accepting connections after stopCh was closed")
}

func waitForHTTP(t *testing.T, url string) error {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		client := &http.Client{Timeout: 200 * time.Millisecond}
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}

	return errors.New("timed out waiting for the endpoint")
}
