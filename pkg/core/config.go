package core

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

// OnInitialize 返回一个初始化函数，用于设置配置文件和环境变量的读取方式。
// - configFile: 指向配置文件路径的指针，可通过命令行参数指定。仅当它非 nil 且指向非空字符串时生效。
// - envPrefix: 环境变量前缀，用于过滤并命名该应用的环境变量。
// - loadDirs: 配置文件搜索目录列表，当未指定configFile时使用。
// - defaultConfigName: 默认配置文件名（不含扩展名）。
//
// Deprecated: 该函数是配置加载逻辑的早期实现，缺少配置文件格式选择、配置热加载
// 与读取失败的上报能力，pkg/app 中已有更完善的实现（app.AddConfigFlag 等）。
// 新代码请使用 pkg/app 提供的初始化方式，本函数仅为兼容历史调用方保留。
func OnInitialize(configFile *string, envPrefix string, loadDirs []string, defaultConfigName string) func() {
	return func() {
		// 只有当命令行显式指定了「非空」的配置文件路径时才使用该路径.
		// 注意：configFile 通常由 pflag 注入，永远是非 nil 指针，但可能指向空字符串，
		// 因此必须判断 *configFile 是否为空，而不能只判断指针是否为 nil.
		if configFile != nil && *configFile != "" {
			// 从命令行选项指定的配置文件中读取
			viper.SetConfigFile(*configFile)
		} else {
			// 否则，将各个目录加入搜索路径，依次查找配置文件.
			for _, dir := range loadDirs {
				// 将 dir 目录加入到配置文件的搜索路径.
				viper.AddConfigPath(dir)
			}

			// 设置配置文件格式为 YAML.
			viper.SetConfigType("yaml")

			// 配置文件名称（没有文件扩展名）.
			viper.SetConfigName(defaultConfigName)
		}

		// 读取匹配的环境变量.
		viper.AutomaticEnv()

		// 设置环境变量前缀.
		// 例如：envPrefix="MINIBLOG"，则只读取以 MINIBLOG_ 开头的变量.
		viper.SetEnvPrefix(envPrefix)

		// 将 key 字符串中 '.' 和 '-' 替换为 '_'
		replacer := strings.NewReplacer(".", "_", "-", "_")
		viper.SetEnvKeyReplacer(replacer)

		// 读取配置文件。如果指定了配置文件名，则使用指定的配置文件，否则在注册的搜索路径中搜索.
		if err := viper.ReadInConfig(); err != nil {
			// 「找不到配置文件」是可以接受的：配置可以全部来自环境变量.
			var notFound viper.ConfigFileNotFoundError
			if !errors.As(err, &notFound) {
				// 其余错误（如配置文件解析失败）不能静默吞掉；
				// 由于 OnInitialize 返回的 func() 无法返回 error，这里显式输出到 stderr.
				fmt.Fprintf(os.Stderr, "core.OnInitialize: failed to read config file: %v\n", err)
			}
		}
	}
}
