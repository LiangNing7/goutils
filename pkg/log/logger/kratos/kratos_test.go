package kratos

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	krtlog "github.com/go-kratos/kratos/v2/log"

	"github.com/LiangNing7/goutils/pkg/log"
)

// TestNewLoggerResolvesGlobalLoggerLazily 回归：在 log.Init 之前构造的 kratos logger
// 必须把日志交给 Init 之后的全局 logger，而不是构造期捕获的旧 logger.
func TestNewLoggerResolvesGlobalLoggerLazily(t *testing.T) {
	// 先构造 logger（此时还是默认的全局 logger）.
	logger := NewLogger("svc-id", "svc-name", "v1")

	logFile := filepath.Join(t.TempDir(), "kratos.log")
	opts := log.NewOptions()
	opts.Level = "info"
	opts.Format = "json"
	opts.DisableCaller = true
	opts.OutputPaths = []string{logFile}

	log.Init(opts)
	t.Cleanup(func() { log.Init(log.NewOptions()) })

	if err := logger.Log(krtlog.LevelInfo, "msg", "hello", "k", "v"); err != nil {
		t.Fatalf("log: %v", err)
	}
	log.Sync()

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "service.name") || !strings.Contains(got, "svc-name") {
		t.Fatalf("expected service.name in freshly initialized logger output, got: %s", got)
	}
	if !strings.Contains(got, "\"k\":\"v\"") {
		t.Fatalf("expected key/value in output, got: %s", got)
	}
}
