package core

import (
	"strings"

	"github.com/spf13/viper"
)

// OnInitialize 返回一个初始化函数，用于设置配置文件和环境变量的读取方式。
// - configFile: 指向配置文件路径的指针，可通过命令行参数指定。
// - envPrefix: 环境变量前缀，用于过滤并命名该应用的环境变量。
// - loadDirs: 配置文件搜索目录列表，当未指定configFile时使用。
// - defaultConfigName: 默认配置文件名（不含扩展名）。
func OnInitialize(configFile *string, envPrefix string, loadDirs []string, defaultConfigName string) func() {
	return func() {
		// 如果通过命令行指定了配置文件路径，则优先使用该路径.
		if configFile != nil {
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

		// 读取配置文件。如果指定了配置文件名，则使用指定的配置文件，否则在注册的搜索路径中搜索
		_ = viper.ReadInConfig()
	}
}
