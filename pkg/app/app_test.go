package app

import (
	"testing"

	"github.com/spf13/pflag"
	cliflag "k8s.io/component-base/cli/flag"
)

// fakeNamedFlagSetOptions 实现 NamedFlagSetOptions 接口，用于回归测试 C7b.
type fakeNamedFlagSetOptions struct {
	fss cliflag.NamedFlagSets
}

func (o *fakeNamedFlagSetOptions) Flags() cliflag.NamedFlagSets { return o.fss }
func (o *fakeNamedFlagSetOptions) Complete() error              { return nil }
func (o *fakeNamedFlagSetOptions) Validate() error              { return nil }

// fakeFlagSetOptions 实现 FlagSetOptions 接口.
type fakeFlagSetOptions struct{}

func (o *fakeFlagSetOptions) AddFlags(fs *pflag.FlagSet) {
	fs.String("fake-flag", "", "fake flag")
}
func (o *fakeFlagSetOptions) Complete() error { return nil }
func (o *fakeFlagSetOptions) Validate() error { return nil }

// TestNewAppWithoutOptionsDoesNotPanic 回归 C7：
// 不传 WithOptions 时 NewApp 必须能构建命令，而不是 panic.
func TestNewAppWithoutOptionsDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("NewApp panicked without options: %v", r)
		}
	}()

	app := NewApp("x", "d")
	if app.Command() == nil {
		t.Fatal("command is nil")
	}
}

// TestNamedFlagSetOptionsRegistersConfigAndVersionFlags 回归 C7b：
// NamedFlagSetOptions 分支下 --config/-c/--version 必须挂在命令自身的 FlagSet 上.
func TestNamedFlagSetOptionsRegistersConfigAndVersionFlags(t *testing.T) {
	opts := &fakeNamedFlagSetOptions{}
	fss := cliflag.NamedFlagSets{}
	global := fss.FlagSet("global")
	global.String("demo-flag", "", "demo flag")
	opts.fss = fss

	app := NewApp("myapp", "desc", WithOptions(opts))
	flags := app.Command().Flags()

	if flags.Lookup("config") == nil {
		t.Error("flag --config not registered on command flags")
	}
	if flags.ShorthandLookup("c") == nil {
		t.Error("shorthand -c not registered on command flags")
	}
	if flags.Lookup("version") == nil {
		t.Error("flag --version not registered on command flags")
	}
	if flags.Lookup("demo-flag") == nil {
		t.Error("flag --demo-flag not registered on command flags")
	}
}

// TestFlagSetOptionsRegistersConfigAndVersionFlags 确保 FlagSetOptions 分支未回归.
func TestFlagSetOptionsRegistersConfigAndVersionFlags(t *testing.T) {
	app := NewApp("myapp", "desc", WithOptions(&fakeFlagSetOptions{}))
	// FlagSetOptions 分支把 flag 挂到 PersistentFlags，cobra 在解析时会合并到 Flags()，
	// 这里用 LocalFlags() 触发合并后断言.
	flags := app.Command().LocalFlags()

	for _, name := range []string{"config", "version", "fake-flag"} {
		if flags.Lookup(name) == nil {
			t.Errorf("flag --%s not registered", name)
		}
	}
	if flags.ShorthandLookup("c") == nil {
		t.Error("shorthand -c not registered")
	}
}
