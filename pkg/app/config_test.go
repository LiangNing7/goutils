package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/viper"
)

type reloadTarget struct {
	Level string `mapstructure:"level"`
	Port  int    `mapstructure:"port"`

	done chan struct{}
}

func (r *reloadTarget) NewReloadOptions() any { return &reloadTarget{} }
func (r *reloadTarget) ApplyReloadOptions(candidate any) error {
	next := candidate.(*reloadTarget)
	r.Level, r.Port = next.Level, next.Port
	if r.done != nil {
		r.done <- struct{}{}
	}
	return nil
}
func (r *reloadTarget) Complete() error {
	if r.Level == "incomplete" {
		return fmt.Errorf("cannot complete")
	}
	return nil
}
func (r *reloadTarget) Validate() error {
	if r.Port < 1 {
		return fmt.Errorf("invalid port")
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

	// 发布完成后通过 done 同步，确保读取的是已应用的配置。
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

func TestReloadRejectsInvalidCandidate(t *testing.T) {
	for _, content := range []string{"level: incomplete\nport: 9090\n", "level: debug\nport: -1\n", "port: invalid\n"} {
		t.Run(content, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			path := filepath.Join(t.TempDir(), "app.yaml")
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			viper.SetConfigFile(path)
			target := &reloadTarget{Level: "info", Port: 8080}
			if err := applyConfigReload(target); err == nil {
				t.Fatal("expected rejection")
			}
			if target.Level != "info" || target.Port != 8080 {
				t.Fatalf("active config changed: %+v", target)
			}
		})
	}
}
func TestReloadDoesNotMutateOrdinaryOptions(t *testing.T) {
	target := &struct{ Port int }{8080}
	if err := applyConfigReload(target); err == nil {
		t.Fatal("expected explicit reload interface requirement")
	}
	if target.Port != 8080 {
		t.Fatal("startup options changed")
	}
}

// snapshotReloader demonstrates the publication contract while readers run.
type snapshotReloader struct{ current atomic.Pointer[reloadTarget] }

func (r *snapshotReloader) NewReloadOptions() any { return &reloadTarget{} }
func (r *snapshotReloader) ApplyReloadOptions(candidate any) error {
	r.current.Store(candidate.(*reloadTarget))
	return nil
}
func TestReloadPublishesIndependentSnapshot(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	path := filepath.Join(t.TempDir(), "app.yaml")
	if err := os.WriteFile(path, []byte("level: debug\nport: 9090\n"), 0600); err != nil {
		t.Fatal(err)
	}
	viper.SetConfigFile(path)
	original := &reloadTarget{Level: "info", Port: 8080}
	target := &snapshotReloader{}
	target.current.Store(original)
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			select {
			case <-done:
				return
			default:
			}
			snapshot := target.current.Load()
			if !((snapshot.Level == "info" && snapshot.Port == 8080) || (snapshot.Level == "debug" && snapshot.Port == 9090)) {
				t.Errorf("partial snapshot: %+v", snapshot)
				return
			}
		}
	}()
	for i := 0; i < 50; i++ {
		if err := applyConfigReload(target); err != nil {
			t.Error(err)
			break
		}
	}
	close(done)
	<-stopped
	if original.Level != "info" || original.Port != 8080 {
		t.Fatal("original snapshot mutated")
	}
	if target.current.Load().Port != 9090 {
		t.Fatal("new snapshot was not published")
	}
}
