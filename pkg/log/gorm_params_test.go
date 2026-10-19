package log_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/LiangNing7/goutils/pkg/log"
)

type gormParamProbe struct {
	ID   uint `gorm:"primarykey"`
	Name string
}

// openProbeDB 用给定的 gorm logger 打开一个内存 sqlite 库.
func openProbeDB(t *testing.T, logger gormlogger.Interface) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	return db
}

// newFileLoggerOptions 构造一个把日志写到 path 的 Options.
func newFileLoggerOptions(path string) *log.Options {
	opts := log.NewOptions()
	opts.Level = "info"
	opts.OutputPaths = []string{path}
	opts.EnableColor = false

	return opts
}

// TestGormLoggerParameterizesSQL 验证自定义 gorm logger 不会把 SQL 参数值写进日志.
//
// gorm 只在 logger 实现 ParamsFilter 时才会传参数化 SQL；否则 callbacks.go 会
// 无条件用 Dialector.Explain 把参数插值进 SQL 再交给 Trace，导致密码哈希/PII
// 直接进应用日志.
func TestGormLoggerParameterizesSQL(t *testing.T) {
	const secret = "SUPER_SECRET_PARAM_VALUE"

	logPath := filepath.Join(t.TempDir(), "param.log")
	logger := log.NewLogger(newFileLoggerOptions(logPath))

	db := openProbeDB(t, logger)
	if err := db.AutoMigrate(&gormParamProbe{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	if err := db.Create(&gormParamProbe{Name: secret}).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	out := string(b)

	if out == "" {
		t.Fatal("expected SQL to be logged, but log file is empty")
	}
	if strings.Contains(out, secret) {
		t.Errorf("SQL parameter value leaked into logs:\n%s", out)
	}
	if !strings.Contains(out, "?") {
		t.Errorf("expected parameterized SQL with placeholders, got:\n%s", out)
	}
}

// TestGormLoggerResolvesGlobalLoggerLazily 验证在 log.Init 之前取到的 gorm logger
// 仍会使用 Init 之后的全局 logger，否则日志级别与输出路径对 GORM 日志不生效.
func TestGormLoggerResolvesGlobalLoggerLazily(t *testing.T) {
	// 先取 logger，稍后才 Init：延迟解析应当生效.
	// 显式指定 Info 级别，否则 AutoMigrate 的语句（Info 级）不会被记录.
	lazy := log.NewGormLogger().LogMode(gormlogger.Info)

	t.Cleanup(func() {
		log.Init(log.NewOptions())
	})

	logPath := filepath.Join(t.TempDir(), "lazy.log")
	log.Init(newFileLoggerOptions(logPath))

	db := openProbeDB(t, lazy)
	if err := db.AutoMigrate(&gormParamProbe{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !strings.Contains(string(b), "CREATE TABLE") {
		t.Errorf("expected SQL in the freshly initialized log file, got:\n%s", string(b))
	}
}
