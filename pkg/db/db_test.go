package db

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type logCapture struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (c *logCapture) Printf(format string, args ...interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fmt.Fprintf(&c.buf, format, args...)
}

func (c *logCapture) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

type dbSecret struct {
	ID    uint `gorm:"primaryKey"`
	Token string
}

const secretValue = "hunter2-super-secret-token"

func openSQLiteWithLogger(t *testing.T, name string, l logger.Interface) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", name)), &gorm.Config{Logger: l})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&dbSecret{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return db
}

// TestDefaultGormLoggerDoesNotLeakParameters 默认 logger 必须启用参数化 SQL，不把参数插值进日志.
func TestDefaultGormLoggerDoesNotLeakParameters(t *testing.T) {
	cap := &logCapture{}
	db := openSQLiteWithLogger(t, "param_logger", newGormLogger(cap, logger.Info))

	if err := db.Create(&dbSecret{Token: secretValue}).Error; err != nil {
		t.Fatal(err)
	}

	out := cap.String()
	if !strings.Contains(out, "?") {
		t.Fatalf("日志中应保留参数占位符 ?, 实际: %q", out)
	}
	if strings.Contains(out, secretValue) {
		t.Fatalf("日志泄露了参数值 %q: %q", secretValue, out)
	}
}

// TestNonParameterizedLoggerLeaksParameters 反证：未开启 ParameterizedQueries 时参数确实会被插值，
// 说明上面的断言是有效的（不是恒真的）.
func TestNonParameterizedLoggerLeaksParameters(t *testing.T) {
	cap := &logCapture{}
	// 直接构造一个未参数化的 logger（模拟旧的 logger.Default 行为）.
	db := openSQLiteWithLogger(t, "raw_logger", logger.New(cap, logger.Config{LogLevel: logger.Info}))

	if err := db.Create(&dbSecret{Token: secretValue}).Error; err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(cap.String(), secretValue) {
		t.Fatalf("预期未参数化的 logger 会把参数插值进日志，实际: %q", cap.String())
	}
}

func assertParameterized(t *testing.T, name string, l logger.Interface) {
	t.Helper()
	if l == nil {
		t.Fatalf("%s: 默认 Logger 为 nil", name)
	}
	pf, ok := l.(gorm.ParamsFilter)
	if !ok {
		t.Fatalf("%s: 默认 Logger 未实现 gorm.ParamsFilter", name)
	}
	if _, vars := pf.ParamsFilter(context.Background(), "SELECT * FROM t WHERE p = ?", secretValue); vars != nil {
		t.Fatalf("%s: 默认 Logger 未启用 ParameterizedQueries，参数会被插值进日志", name)
	}
}

// TestMySQLDefaultsUseParameterizedLogger 默认 Logger 必须是参数化的.
func TestMySQLDefaultsUseParameterizedLogger(t *testing.T) {
	opts := &MySQLOptions{}
	setMySQLDefaults(opts)
	assertParameterized(t, "mysql", opts.Logger)
}

// TestPostgreSQLDefaultsUseParameterizedLogger 默认 Logger 必须是参数化的.
func TestPostgreSQLDefaultsUseParameterizedLogger(t *testing.T) {
	opts := &PostgreSQLOptions{}
	setPostgreSQLDefaults(opts)
	assertParameterized(t, "postgresql", opts.Logger)
}

// TestUserProvidedLoggerIsRespected 调用方显式传入的 Logger 不会被默认值覆盖.
func TestUserProvidedLoggerIsRespected(t *testing.T) {
	cap := &logCapture{}
	userLogger := logger.New(cap, logger.Config{LogLevel: logger.Info})

	myOpts := &MySQLOptions{Logger: userLogger}
	setMySQLDefaults(myOpts)
	if myOpts.Logger != userLogger {
		t.Fatal("mysql: 调用方提供的 Logger 被覆盖")
	}

	pgOpts := &PostgreSQLOptions{Logger: userLogger}
	setPostgreSQLDefaults(pgOpts)
	if pgOpts.Logger != userLogger {
		t.Fatal("postgresql: 调用方提供的 Logger 被覆盖")
	}
}

func openSQLite(t *testing.T, name string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", name)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return db
}

// TestTracePluginInitialize 插件注册路径可达，且前后回调都注册成功.
func TestTracePluginInitialize(t *testing.T) {
	db := openSQLite(t, "plugin")

	if err := db.Use(&TracePlugin{}); err != nil {
		t.Fatalf("Use(&TracePlugin{}): %v", err)
	}

	for name, p := range map[string]interface{ Get(string) func(*gorm.DB) }{
		"create": db.Callback().Create(),
		"query":  db.Callback().Query(),
		"delete": db.Callback().Delete(),
		"update": db.Callback().Update(),
		"row":    db.Callback().Row(),
		"raw":    db.Callback().Raw(),
	} {
		if p.Get(callBackBeforeName) == nil {
			t.Errorf("%s: before 回调未注册", name)
		}
		if p.Get(callBackAfterName) == nil {
			t.Errorf("%s: after 回调未注册", name)
		}
	}
}

// TestTracePluginInitializeDuplicateRegistration 重复注册同名回调时，错误必须被返回而不是被吞掉.
func TestTracePluginInitializeDuplicateRegistration(t *testing.T) {
	db := openSQLite(t, "plugin_dup")
	if err := db.Use(&TracePlugin{}); err != nil {
		t.Fatalf("first Use: %v", err)
	}

	err := db.Use(&TracePlugin{})
	if err == nil {
		t.Fatal("重复注册同名回调应返回错误（旧实现会静默吞掉）")
	}
	t.Logf("duplicate registration error (expected): %v", err)
}

// TestNewRedisPingFailure Redis 不可用时 NewRedis 返回错误且不返回 client.
func TestNewRedisPingFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	rdb, err := NewRedis(&RedisOptions{
		Addr:         ln.Addr().String(),
		MaxRetries:   -1, // 不重试，快速失败
		DialTimeout:  200 * time.Millisecond,
		ReadTimeout:  200 * time.Millisecond,
		WriteTimeout: 200 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("预期 Ping 失败返回错误")
	}
	if rdb != nil {
		t.Fatalf("失败时必须返回 nil client, 实际 %v", rdb)
	}
}
