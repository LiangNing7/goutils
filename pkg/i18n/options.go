package i18n

import (
	"embed"

	"golang.org/x/text/language"
)

// 支持的语言文件格式.
const (
	// formatYML yaml 格式（.yml 扩展名）.
	formatYML = "yml"
	// formatYAML yaml 格式（.yaml 扩展名）.
	formatYAML = "yaml"
	// formatTOML toml 格式.
	formatTOML = "toml"
	// formatJSON json 格式.
	formatJSON = "json"
)

type Options struct {
	format   string
	language language.Tag
	files    []string
	fs       embed.FS
}

func WithFormat(format string) func(*Options) {
	return func(options *Options) {
		if format != "" {
			getOptionsOrSetDefault(options).format = format
		}
	}
}

func WithLanguage(lang language.Tag) func(*Options) {
	return func(options *Options) {
		if lang.String() != "und" {
			getOptionsOrSetDefault(options).language = lang
		}
	}
}

func WithFile(f string) func(*Options) {
	return func(options *Options) {
		if f != "" {
			getOptionsOrSetDefault(options).files = append(getOptionsOrSetDefault(options).files, f)
		}
	}
}

func WithFS(fs embed.FS) func(*Options) {
	return func(options *Options) {
		getOptionsOrSetDefault(options).fs = fs
	}
}

func getOptionsOrSetDefault(options *Options) *Options {
	if options == nil {
		return &Options{
			format:   formatYML,
			language: language.English,
			files:    []string{},
		}
	}
	return options
}
