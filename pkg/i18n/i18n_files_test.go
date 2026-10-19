package i18n

import (
	"embed"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/text/language"
)

// emptyFS 用于覆盖 AddFS 在空 embed.FS 下的行为.
var emptyFS embed.FS

// writeFile 在 dir 下写入 name 文件并返回完整路径.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	assert.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// TestNew_YmlFilesLoaded 覆盖默认 format "yml" 时 .yml 文件被静默忽略的问题.
func TestNew_YmlFilesLoaded(t *testing.T) {
	dir := t.TempDir()
	en := writeFile(t, dir, "en.yml", "hello.world: Hello world!\n")
	zh := writeFile(t, dir, "zh.yml", "hello.world: 你好, 世界!\n")

	i := New(
		WithFormat("yml"),
		WithFile(en),
		WithFile(zh),
		WithLanguage(language.English),
	)

	assert.Equal(t, "Hello world!", i.T("hello.world"))
	assert.Equal(t, "你好, 世界!", i.Select(language.Chinese).T("hello.world"))
	assert.Equal(t, "Hello world!", i.E("hello.world").Error())
}

// TestNew_YamlFilesLoaded 覆盖 .yaml 扩展名.
func TestNew_YamlFilesLoaded(t *testing.T) {
	dir := t.TempDir()
	en := writeFile(t, dir, "en.yaml", "hello.world: Hello world!\n")
	zh := writeFile(t, dir, "zh.yaml", "hello.world: 你好, 世界!\n")

	i := New(
		WithFormat("yaml"),
		WithFile(en),
		WithFile(zh),
		WithLanguage(language.English),
	)

	assert.Equal(t, "Hello world!", i.T("hello.world"))
	assert.Equal(t, "你好, 世界!", i.Select(language.Chinese).T("hello.world"))
}

// TestNew_DefaultFormatLoadsYml 覆盖默认 format（不传 WithFormat）时 .yml 文件可加载.
func TestNew_DefaultFormatLoadsYml(t *testing.T) {
	dir := t.TempDir()
	en := writeFile(t, dir, "en.yml", "hello.world: Hello world!\n")

	i := New(WithFile(en))
	assert.Equal(t, "Hello world!", i.T("hello.world"))
}

// TestNew_UnknownFormatFallback 覆盖未知 format 的降级行为：按 yaml 注册（含 yml 扩展名）.
func TestNew_UnknownFormatFallback(t *testing.T) {
	dir := t.TempDir()
	enYml := writeFile(t, dir, "en.yml", "hello.world: Hello world!\n")

	i := New(
		WithFormat("not-a-format"),
		WithFile(enYml),
		WithLanguage(language.English),
	)

	assert.Equal(t, "Hello world!", i.T("hello.world"))
}

// TestNew_TomlAndJSON 覆盖 toml / json 两种行为.
func TestNew_TomlAndJSON(t *testing.T) {
	t.Run("toml", func(t *testing.T) {
		dir := t.TempDir()
		en := writeFile(t, dir, "en.toml", "[hello]\nworld = \"Hello world!\"\n")

		i := New(WithFormat("toml"), WithFile(en), WithLanguage(language.English))
		assert.Equal(t, "Hello world!", i.T("hello.world"))
	})

	t.Run("json", func(t *testing.T) {
		dir := t.TempDir()
		en := writeFile(t, dir, "en.json", `{"hello.world": "Hello world!"}`)

		i := New(WithFormat("json"), WithFile(en), WithLanguage(language.English))
		assert.Equal(t, "Hello world!", i.T("hello.world"))
	})
}

// TestAdd_Dir 覆盖 Add 目录的方式加载 .yml 文件.
func TestAdd_Dir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "en.yml", "hello.world: Hello world!\n")
	writeFile(t, dir, "zh.yml", "hello.world: 你好, 世界!\n")

	i := New(WithFormat("yml"), WithLanguage(language.English))
	i.Add(dir)

	assert.Equal(t, "Hello world!", i.T("hello.world"))
	assert.Equal(t, "你好, 世界!", i.Select(language.Chinese).T("hello.world"))
}

// TestI18n_ConcurrentAddAndT 覆盖运行期热加载（Add）与并发读取（T/LocalizeT）的并发安全.
// 需要配合 -race 运行.
func TestI18n_ConcurrentAddAndT(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		writeFile(t, dir, "en.yaml", "hello.world: Hello world!\n"),
		writeFile(t, dir, "de.yaml", "hello.world: Hallo Welt!\n"),
		writeFile(t, dir, "fr.yaml", "hello.world: Bonjour le monde!\n"),
		writeFile(t, dir, "es.yaml", "hello.world: ¡Hola Mundo!\n"),
		writeFile(t, dir, "it.yaml", "hello.world: Ciao mondo!\n"),
	}

	i := New(WithFormat("yml"), WithFile(files[0]), WithLanguage(language.English))

	var wg sync.WaitGroup

	// 并发读取
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				_ = i.T("hello.world")
				_ = i.Select(language.Chinese).T("hello.world")
				_ = i.LocalizeE(nil)
			}
		}()
	}

	// 并发热加载（含重复加载同一文件，触发 bundle 内部 map 的写）
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				for _, f := range files {
					i.Add(f)
				}
				i.AddFS(emptyFS)
			}
		}()
	}

	wg.Wait()
}

// TestI18n_ConcurrentSelect 覆盖并发 Select 的并发安全（需 -race）.
func TestI18n_ConcurrentSelect(t *testing.T) {
	dir := t.TempDir()
	en := writeFile(t, dir, "en.yml", "hello.world: Hello world!\n")
	writeFile(t, dir, "zh.yml", "hello.world: 你好, 世界!\n")

	i := New(WithFormat("yml"), WithFile(en), WithLanguage(language.English))

	var wg sync.WaitGroup
	for k := range 8 {
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			for range 100 {
				if k%2 == 0 {
					_ = i.Select(language.Chinese).T("hello.world")
					continue
				}
				_ = i.Select(language.English).T("hello.world")
			}
		}(k)
	}
	wg.Wait()
}
