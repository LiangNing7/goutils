package watch

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/LiangNing7/goutils/pkg/distlock"
	stringsutil "github.com/LiangNing7/goutils/pkg/util/strings"
	"github.com/LiangNing7/goutils/pkg/watch/initializer"
	"github.com/LiangNing7/goutils/pkg/watch/logger/empty"
	"github.com/LiangNing7/goutils/pkg/watch/manager"
	"github.com/LiangNing7/goutils/pkg/watch/registry"
)

var (
	// Timeout duration for stopping jobs.
	jobStopTimeout = 3 * time.Minute
	// Default expiration time for locks.
	defaultExpiration = 10 * time.Second
)

// newLocker 是创建分布式锁构造函数，抽成变量便于测试注入.
var newLocker = func(db *gorm.DB, opts ...distlock.Option) (distlock.Locker, error) {
	return distlock.NewGORMLocker(db, opts...)
}

// Option configures a Watch instance with customizable settings.
type Option func(w *Watch)

// Watch represents a monitoring system that schedules and runs tasks at specified intervals.
type Watch struct {
	// Used to implement dist lock.
	db *gorm.DB
	// Job manager to handle scheduling and execution of jobs.
	jm *manager.JobManager
	// Maximum number of concurrent workers for each watcher.
	maxWorkers int64
	// Logger for logging events and errors.
	logger Logger
	// Distributed lock name to be used across instances.
	lockName string
	// Distributed lock instance.
	locker distlock.Locker
	// healthzPort is the port number for the health check endpoint.
	healthzPort int
	// List of watcher names that should be disabled.
	disableWatchers []string
	// Function for internal initialization of watchers.
	initializer initializer.WatcherInitializer
	// Function for external initialization of watchers.
	externalInitializer initializer.WatcherInitializer
}

// WithInitialize returns an Option function that sets the provided WatcherInitializer
// function to initialize the Watch during its creation.
func WithInitialize(initialize initializer.WatcherInitializer) Option {
	return func(w *Watch) {
		w.externalInitializer = initialize
	}
}

// WithLogger returns an Option function that sets the provided Logger to the Watch for logging purposes.
func WithLogger(logger Logger) Option {
	return func(w *Watch) {
		w.logger = logger
	}
}

// NewWatch creates a new Watch monitoring system with the provided options.
func NewWatch(opts *Options, db *gorm.DB, withOptions ...Option) (*Watch, error) {
	logger := empty.NewLogger()

	// Create a new Watch with default settings.
	w := &Watch{
		lockName:        opts.LockName,
		healthzPort:     opts.HealthzPort,
		logger:          logger,
		disableWatchers: opts.DisableWatchers,
		db:              db,
		maxWorkers:      opts.MaxWorkers,
	}

	// Apply user-defined options to the Watch.
	for _, opt := range withOptions {
		opt(w)
	}

	runner := cron.New(
		cron.WithSeconds(),
		cron.WithLogger(w.logger),
		cron.WithChain(cron.DelayIfStillRunning(w.logger), cron.Recover(w.logger)),
	)

	// Initialize the job manager and the watcher initializer.
	w.jm = manager.NewJobManager(manager.WithCron(runner))
	w.initializer = initializer.NewInitializer(w.jm, w.maxWorkers)

	if err := w.addWatchers(); err != nil {
		return nil, err
	}

	return w, nil
}

// addWatchers initializes all registered watchers and adds them as Cron jobs.
// It skips the watchers that are specified in the disableWatchers slice.
func (w *Watch) addWatchers() error {
	for jobName, watcher := range registry.ListWatchers() {
		if stringsutil.StringIn(jobName, w.disableWatchers) {
			continue
		}

		w.initializer.Initialize(watcher)
		if w.externalInitializer != nil {
			w.externalInitializer.Initialize(watcher)
		}

		spec := registry.Every3Seconds
		if obj, ok := watcher.(registry.ISpec); ok {
			spec = obj.Spec()
		}

		if _, err := w.jm.AddJob(jobName, spec, watcher); err != nil {
			w.logger.Error(err, "Failed to add job to the cron", "watcher", jobName)
			return err
		}
	}

	return nil
}

// Start attempts to acquire a distributed lock and starts the Cron job scheduler.
// It retries acquiring the lock until successful, or until stopCh is closed.
func (w *Watch) Start(stopCh <-chan struct{}) {
	if w.healthzPort != 0 {
		go w.serveHealthz(stopCh)
	}

	opts := []distlock.Option{
		distlock.WithLockTimeout(defaultExpiration),
		distlock.WithLockName(w.lockName),
	}

	locker, err := newLocker(w.db, opts...)
	if err != nil {
		// 不能吞掉错误：AutoMigrate 失败时 locker 为 nil，继续走下去会在
		// w.locker.Lock 时空指针 panic.
		w.logger.Error(err, "Failed to create distributed locker, watch server will not start", "lockName", w.lockName)
		return
	}
	w.locker = locker

	ctx := wait.ContextForChannel(stopCh)
	if !w.acquireLock(ctx, stopCh) {
		w.logger.Info("Watch server was stopped before it started")
		return
	}

	w.jm.Start()

	w.logger.Info("Successfully started watch server")
}

// acquireLock 阻塞等待获取分布式锁：
//   - 成功获取返回 true；
//   - 在等待期间收到停机信号返回 false（且不会启动任务）.
func (w *Watch) acquireLock(ctx context.Context, stopCh <-chan struct{}) bool {
	ticker := time.NewTicker(defaultExpiration + (5 * time.Second))
	defer ticker.Stop()

	for {
		if err := w.locker.Lock(ctx); err == nil {
			// 拿到锁之后要再确认是否已经请求停机：
			// 否则会在停机后启动 job 并遗留一条锁记录.
			select {
			case <-stopCh:
				w.releaseLock()
				return false
			default:
			}

			w.logger.Debug("Successfully acquired lock", "lockName", w.lockName)
			return true
		} else {
			w.logger.Debug("Failed to acquire lock.", "lockName", w.lockName, "err", err)
		}

		// 重试间隔内也要能响应停机信号（GORMLocker.Lock 本身不阻塞）.
		select {
		case <-stopCh:
			return false
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

// releaseLock 释放分布式锁（如果持有）.
func (w *Watch) releaseLock() {
	if w.locker == nil {
		return
	}

	if err := w.locker.Unlock(context.Background()); err != nil {
		w.logger.Debug("Failed to release lock", "err", err)
	}
}

// Stop blocks until all jobs are completed and releases the distributed lock.
func (w *Watch) Stop() {
	ctx := w.jm.Stop()
	select {
	case <-ctx.Done():
	case <-time.After(jobStopTimeout):
		w.logger.Error(errors.New("context was not done immediately"), "timeout", jobStopTimeout.String())
	}

	if w.locker == nil {
		w.logger.Info("Successfully stopped watch server")
		return
	}

	if err := w.locker.Unlock(ctx); err != nil {
		w.logger.Debug("Failed to release lock", "err", err)
	}

	// 释放底层资源（例如续租 goroutine、数据库连接）.
	if closer, ok := w.locker.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil {
			w.logger.Debug("Failed to close locker", "err", err)
		}
	}

	w.logger.Info("Successfully stopped watch server")
}

// serveHealthz starts the health check server for the Watch instance.
// 健康检查服务器会在 stopCh 关闭后优雅关停.
func (w *Watch) serveHealthz(stopCh <-chan struct{}) {
	defer func() {
		if r := recover(); r != nil {
			w.logger.Error(fmt.Errorf("panic: %v", r), "Recovered from panic in health check server")
		}
	}()

	r := mux.NewRouter()
	r.HandleFunc("/healthz", healthzHandler).Methods(http.MethodGet)

	address := fmt.Sprintf("0.0.0.0:%d", w.healthzPort)

	lis, err := net.Listen("tcp", address)
	if err != nil {
		// 只有真正监听失败时才报错，且不再打印 "Successfully started".
		w.logger.Error(err, "Error serving health check endpoint", "address", address)
		return
	}

	srv := &http.Server{
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-stopCh

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			w.logger.Error(err, "Failed to shut down health check server")
		}
	}()

	w.logger.Info("Successfully started health check server", "address", address)

	if err := srv.Serve(lis); err != nil && !errors.Is(err, http.ErrServerClosed) {
		w.logger.Error(err, "Error serving health check endpoint", "address", address)
	}
}

// healthzHandler handles the health check requests for the service.
func healthzHandler(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-type", "application/json")
	rw.WriteHeader(http.StatusOK)
	rw.Write([]byte(`{"status": "ok"}`))
}
