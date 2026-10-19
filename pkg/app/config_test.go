package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/viper"
)

type reloadTarget struct {
	Level string `mapstructure:"level"`
	Port  int    `mapstructure:"port"`

	done chan struct{}
}

// Complete 在配置被重新应用之后被调用，测试用它同步，避免与重载 goroutine 竞争.
func (r *reloadTarget) Complete() error {
	select {
	case r.done <- struct{}{}:
	default:
	}

	return nil
}

// atomicWrite 模拟编辑器/IDE 的原子保存：写临时文件再 rename 覆盖目标.
func atomicWrite(t *testing.T, path, content string) {
	t.Helper()

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		t.Fatalf("write tmp: %v", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatalf("rename: %v", err)
	}
}

// TestWatchConfigReloadOnAtomicSave 回归 C8：
// macOS 编辑器保存 = 写 tmp + rename，会对目标文件产生 REMOVE 事件；
// 自实现的目录级 watcher 必须能在这种情况下继续重载配置.
func TestWatchConfigReloadOnAtomicSave(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "app.yaml")

	if err := os.WriteFile(cfg, []byte("level: info\nport: 8080\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	viper.Reset()
	viper.SetConfigFile(cfg)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatalf("read config: %v", err)
	}

	target := &reloadTarget{done: make(chan struct{}, 1)}
	if err := viper.Unmarshal(target); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if target.Level != "info" || target.Port != 8080 {
		t.Fatalf("unexpected initial config: %+v", target)
	}

	stop := watchConfigWithReload(target)
	defer stop()

	// 关键：使用原子保存，模拟 macOS 上编辑器/VSCode/GoLand 的行为.
	atomicWrite(t, cfg, "level: debug\nport: 9090\n")

	// Complete 在重载完成（并已应用新配置）之后被调用，用它做同步.
	select {
	case <-target.done:
	case <-time.After(5 * time.Second):
		t.Fatal("config was not reloaded after atomic save")
	}

	if target.Level != "debug" || target.Port != 9090 {
		t.Fatalf("reloaded config was not applied, got: %+v", target)
	}
}

func TestMaskSensitiveValue(t *testing.T) {
	cases := map[string]bool{
		"mysql.password":    true,
		"jwt.key":           true,
		"redis.secret":      true,
		"some.access-token": true,
		"some.api-key":      true,
		"log.level":         false,
		"http.addr":         false,
		"mysql.username":    false,
		"postgresql.":       false,
	}
	for key, wantMasked := range cases {
		got := maskConfigValue(key, "super-secret")
		_, isMasked := got.(string)
		if wantMasked {
			if !isMasked || got.(string) == "super-secret" {
				t.Errorf("key %q should be masked, got %v", key, got)
			}
			continue
		}
		if got != "super-secret" {
			t.Errorf("key %q should not be masked, got %v", key, got)
		}
	}
}
