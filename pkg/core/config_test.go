package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeYAML(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

// configFile 通常由 cobra/pflag 注入，永远是非 nil 指针，但可能指向空字符串；
// 此时应当回退到 loadDirs 搜索配置文件，而不是用空路径去 SetConfigFile.
func TestOnInitialize_FallsBackToSearchDirsWhenConfigFileIsEmpty(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	dir := t.TempDir()
	writeYAML(t, filepath.Join(dir, "config.yaml"), "foo: bar\n")

	empty := ""
	OnInitialize(&empty, "GOUTILS_CORE_TEST", []string{dir}, "config")()

	assert.Equal(t, "bar", viper.GetString("foo"))
}

func TestOnInitialize_UsesExplicitConfigFile(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	dir := t.TempDir()
	path := filepath.Join(dir, "custom.yaml")
	writeYAML(t, path, "foo: baz\n")

	OnInitialize(&path, "GOUTILS_CORE_TEST", nil, "config")()

	assert.Equal(t, "baz", viper.GetString("foo"))
}

func TestOnInitialize_NilConfigFileAndNoConfigFileFound(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	dir := t.TempDir()

	// 找不到配置文件属于可接受的情况：配置可以全部来自环境变量，不应 panic
	assert.NotPanics(t, func() {
		OnInitialize(nil, "GOUTILS_CORE_TEST", []string{dir}, "not-exists")()
	})
	assert.Equal(t, "", viper.GetString("foo"))
}

func TestOnInitialize_EnvKeyReplacer(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	t.Setenv("GOUTILS_CORE_TEST_LOG_LEVEL", "debug")

	OnInitialize(nil, "GOUTILS_CORE_TEST", []string{t.TempDir()}, "not-exists")()

	// '.' / '-' 会被替换为 '_'
	assert.Equal(t, "debug", viper.GetString("log.level"))
}
