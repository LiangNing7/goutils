package log

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	gormlogger "gorm.io/gorm/logger"
)

// newFileLogger 创建输出到临时文件的 logger，便于断言日志内容.
func newTestOptions(t *testing.T, level, format string) (*Options, string) {
	t.Helper()

	logFile := filepath.Join(t.TempDir(), "test.log")
	opts := NewOptions()
	opts.Level = level
	opts.Format = format
	opts.DisableCaller = true
	opts.OutputPaths = []string{logFile}

	return opts, logFile
}

func readLogFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}

	return string(data)
}

// TestInitForwardsOptionsAndAppliesExtractor 回归：Init 必须把 options 转发给 NewLogger，
// 否则 WithContextExtractor 完全失效.
func TestInitForwardsOptionsAndAppliesExtractor(t *testing.T) {
	opts, logFile := newTestOptions(t, "info", "json")

	Init(opts, WithContextExtractor(ContextExtractors{
		"trace_id": func(context.Context) string { return "trace-abc-123" },
	}))
	t.Cleanup(func() { Init(NewOptions()) })

	W(context.Background()).Infow("hello")
	Sync()

	got := readLogFile(t, logFile)
	if !strings.Contains(got, "trace_id") || !strings.Contains(got, "trace-abc-123") {
		t.Fatalf("expected extracted trace_id in log output, got: %s", got)
	}
}

// TestConcurrentInitAndLogIsRaceFree 回归：std 的读写需要并发安全.
func TestConcurrentInitAndLogIsRaceFree(t *testing.T) {
	t.Cleanup(func() { Init(NewOptions()) })

	var wg sync.WaitGroup

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				Infow("concurrent log")
				Debugf("concurrent debug %d", j)
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 100; j++ {
			Init(NewOptions())
		}
	}()

	wg.Wait()
}

// TestPackageLevelCallerPointsToCallSite 回归：包级日志函数的 caller 应指向调用点，
// 而不是 log 包自身.
func TestPackageLevelCallerPointsToCallSite(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "caller.log")
	opts := NewOptions()
	opts.Level = "info"
	opts.Format = "json"
	opts.DisableCaller = false
	opts.OutputPaths = []string{logFile}

	Init(opts)
	t.Cleanup(func() { Init(NewOptions()) })

	Infow("caller check")
	Sync()

	got := readLogFile(t, logFile)
	if !strings.Contains(got, "log_test.go") {
		t.Fatalf("expected caller to point at log_test.go, got: %s", got)
	}
	if strings.Contains(got, "log/log.go") {
		t.Fatalf("caller points at the log package itself: %s", got)
	}
}

// TestGormLevelMapHasDebugKey 回归：levelM 缺少 debug 键会让 log.level=debug 时 SQL 日志静默.
func TestGormLevelMapHasDebugKey(t *testing.T) {
	lvl, ok := levelM["debug"]
	if !ok {
		t.Fatal("levelM is missing the \"debug\" key")
	}
	if lvl != gormlogger.Info {
		t.Fatalf("levelM[\"debug\"] = %v, want %v", lvl, gormlogger.Info)
	}
}

// TestGormTraceEmitsSQLAtDebugLevel 行为级回归：debug 级别下 gorm Trace 必须输出 SQL.
func TestGormTraceEmitsSQLAtDebugLevel(t *testing.T) {
	opts, logFile := newTestOptions(t, "debug", "console")
	logger := NewLogger(opts)

	logger.Trace(context.Background(), time.Now(),
		func() (string, int64) { return "SELECT 1", 1 }, nil)
	logger.Sync()

	if got := readLogFile(t, logFile); !strings.Contains(got, "SELECT 1") {
		t.Fatalf("expected SQL to be logged at debug level, got: %q", got)
	}
}

// TestOptionsAddFlagsAndNewOptionsRoundTrip 保证 log 包 Options 行为未被破坏.
func TestOptionsAddFlagsAndNewOptionsRoundTrip(t *testing.T) {
	opts := NewOptions()
	if opts.Level != "info" || opts.Format != "console" {
		t.Fatalf("unexpected defaults: %+v", opts)
	}
}
