package app

import (
	"context"
	"testing"
)

type ctxKey string

// TestRunFuncReceivesCommandContext 回归：RunFunc 需要拿到命令的 context，
// 以便把取消/超时传递给服务器，实现优雅退出.
func TestRunFuncReceivesCommandContext(t *testing.T) {
	const key ctxKey = "k"

	var got any
	app := NewApp("testapp", "desc",
		WithNoConfig(),
		WithSilence(),
		WithRunFuncWithContext(func(ctx context.Context) error {
			got = ctx.Value(key)
			return nil
		}),
	)

	cmd := app.Command()
	cmd.SetArgs([]string{})
	cmd.SetContext(context.WithValue(context.Background(), key, "value-from-context"))

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if got != "value-from-context" {
		t.Fatalf("RunFuncWithContext did not receive the command context, got %v", got)
	}
}

// TestRunContextReturnsExitCode 回归：RunContext 使用传入的 ctx 并返回退出码.
func TestRunContextReturnsExitCode(t *testing.T) {
	app := NewApp("testapp", "desc", WithNoConfig(), WithSilence())
	app.Command().SetArgs([]string{})

	if code := app.RunContext(context.Background()); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}
