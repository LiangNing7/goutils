//nolint:errcheck
package i18n

import (
	"embed"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/BurntSushi/toml"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
	"gopkg.in/yaml.v2"
)

// defaultI18nMu 是兜底互斥锁，用于保护未经 New 构造的 I18n 实例（mu 为 nil）.
var defaultI18nMu sync.RWMutex

// I18n is used to store the options and configurations for internationalization.
type I18n struct {
	ops       Options
	bundle    *i18n.Bundle
	localizer *i18n.Localizer
	lang      language.Tag

	// mu 保护 bundle 的写入（Add/AddFS）与读取（Localize），
	// 因为 go-i18n 的 bundle 在被 Localizer 读取期间被修改不是并发安全的.
	// 使用指针是为了让 I18n 仍可按值拷贝（例如 Select）而不会复制锁.
	mu *sync.RWMutex
}

// lock 返回保护 bundle 的互斥锁.
func (i I18n) lock() *sync.RWMutex {
	if i.mu == nil {
		return &defaultI18nMu
	}
	return i.mu
}

// New creates a new instance of the I18n struct with the given options.
// It takes a variadic parameter of functional options and returns a pointer to the I18n struct.
func New(options ...func(*Options)) (rp *I18n) {
	ops := getOptionsOrSetDefault(nil)
	for _, f := range options {
		f(ops)
	}
	bundle := i18n.NewBundle(ops.language)
	localizer := i18n.NewLocalizer(bundle, ops.language.String())
	switch ops.format {
	case formatTOML:
		bundle.RegisterUnmarshalFunc(formatTOML, toml.Unmarshal)
	case formatJSON:
		bundle.RegisterUnmarshalFunc(formatJSON, json.Unmarshal)
	default:
		// yml / yaml 以及未知 format 均按 yaml 处理（保持历史默认行为，未知 format 降级为 yaml）.
		// go-i18n 按文件扩展名查找 unmarshal 函数，因此 "yaml" 与 "yml" 都必须注册，
		// 否则 .yml 文件会因为找不到 unmarshal 函数而被静默忽略.
		bundle.RegisterUnmarshalFunc(formatYAML, yaml.Unmarshal)
		bundle.RegisterUnmarshalFunc(formatYML, yaml.Unmarshal)
	}
	rp = &I18n{
		ops:       *ops,
		bundle:    bundle,
		localizer: localizer,
		lang:      ops.language,
		mu:        new(sync.RWMutex),
	}
	for _, item := range ops.files {
		rp.Add(item)
	}
	rp.AddFS(ops.fs)
	return
}

// Select can change language.
func (i I18n) Select(lang language.Tag) *I18n {
	if lang.String() == "und" {
		lang = i.ops.language
	}
	return &I18n{
		ops:       i.ops,
		bundle:    i.bundle,
		localizer: i18n.NewLocalizer(i.bundle, lang.String()),
		lang:      lang,
		mu:        i.lock(),
	}
}

// Language get current language.
func (i I18n) Language() language.Tag {
	return i.lang
}

// LocalizeT localizes the given message and returns the localized string.
// If unable to translate, it returns the message ID as the default message.
func (i I18n) LocalizeT(message *i18n.Message) (rp string) {
	if message == nil {
		return ""
	}

	// 与 Add/AddFS 的写入互斥，避免 bundle 在读取期间被修改.
	mu := i.lock()
	mu.RLock()
	defer mu.RUnlock()

	var err error
	rp, err = i.localizer.Localize(&i18n.LocalizeConfig{
		DefaultMessage: message,
	})
	if err != nil {
		// use id as default message when unable to translate
		rp = message.ID
	}
	return
}

// LocalizeE is a wrapper for LocalizeT method that converts the localized string to an error type and returns it.
func (i I18n) LocalizeE(message *i18n.Message) error {
	return errors.New(i.LocalizeT(message))
}

// T localizes the message with the given ID and returns the localized string.
// It uses the LocalizeT method to perform the translation.
func (i I18n) T(id string) (rp string) {
	return i.LocalizeT(&i18n.Message{ID: id})
}

// E is a wrapper for T that converts the localized string to an error type and returns it.
func (i I18n) E(id string) error {
	return errors.New(i.T(id))
}

// Add is add language file or dir(auto get language by filename).
// Add 可在构造后被并发调用，内部会与 Localize 的读取互斥.
func (i *I18n) Add(f string) {
	info, err := os.Stat(f)
	if err != nil {
		return
	}

	mu := i.lock()
	mu.Lock()
	defer mu.Unlock()

	if info.IsDir() {
		filepath.Walk(f, func(path string, fi os.FileInfo, errBack error) (err error) {
			if !fi.IsDir() {
				i.bundle.LoadMessageFile(path)
			}
			return
		})
	} else {
		i.bundle.LoadMessageFile(f)
	}
}

// AddFS is add language embed files.
// AddFS 可在构造后被并发调用，内部会与 Localize 的读取互斥.
func (i *I18n) AddFS(fs embed.FS) {
	files := readFS(fs, ".")
	if len(files) == 0 {
		return
	}

	mu := i.lock()
	mu.Lock()
	defer mu.Unlock()

	for _, name := range files {
		i.bundle.LoadMessageFileFS(fs, name)
	}
}

func readFS(fs embed.FS, dir string) (rp []string) {
	rp = make([]string, 0)
	dirs, err := fs.ReadDir(dir)
	if err != nil {
		return
	}
	for _, item := range dirs {
		name := dir + string(os.PathSeparator) + item.Name()
		if dir == "." {
			name = item.Name()
		}
		if item.IsDir() {
			rp = append(rp, readFS(fs, name)...)
		} else {
			rp = append(rp, name)
		}
	}
	return
}
