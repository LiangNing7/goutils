package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"k8s.io/client-go/util/homedir"

	"github.com/LiangNing7/goutils/pkg/log"
)

const configFlagName = "config"

// configReloadDebounce 用来合并编辑器保存动作产生的连续文件事件.
const configReloadDebounce = 150 * time.Millisecond

var cfgFile string

// AddConfigFlag adds flags for a specific server to the specified FlagSet object.
// It also sets a passed functions to read values from configuration file into viper
// when each cobra command's Execute method is called.
func AddConfigFlag(fs *pflag.FlagSet, name string, watch bool) {
	addConfigFlag(fs, name, watch, nil)
}

// addConfigFlag 与 AddConfigFlag 行为一致，额外支持在热加载后把新的配置
// 重新应用到 options.
func addConfigFlag(fs *pflag.FlagSet, name string, watch bool, options any) {
	fs.AddFlag(pflag.Lookup(configFlagName))

	// Enable viper's automatic environment variable parsing. This means
	// that viper will automatically read values corresponding to viper
	// variables from environment variables.
	viper.AutomaticEnv()
	// Set the environment variable prefix. Use the strings.ReplaceAll function
	// to replace hyphens with underscores in the name, and use strings.ToUpper
	// to convert the name to uppercase, then set it as the prefix for environment variables.
	viper.SetEnvPrefix(strings.ReplaceAll(strings.ToUpper(name), "-", "_"))
	// Set the replacement rules for environment variable keys. Use the
	// strings.NewReplacer function to specify replacing periods and hyphens with underscores.
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))

	cobra.OnInitialize(func() {
		if cfgFile != "" {
			viper.SetConfigFile(cfgFile)
		} else {
			viper.AddConfigPath(".")

			if names := strings.Split(name, "-"); len(names) > 1 {
				viper.AddConfigPath(filepath.Join(homedir.HomeDir(), "."+names[0]))
				viper.AddConfigPath(filepath.Join("/etc", names[0]))
			}

			viper.SetConfigName(name)
		}

		if err := viper.ReadInConfig(); err != nil {
			log.Debugw("Failed to read configuration file", "file", cfgFile, "err", err)
		}
		log.Debugw("Success to read configuration file", "file", viper.ConfigFileUsed())

		if watch {
			watchConfigWithReload(options)
		}
	})
}

// watchConfigWithReload 监听配置文件所在目录，在文件发生变更后重新读取配置并
// 重新应用到 options，返回一个停止监听的函数.
//
// 这里没有使用 viper.WatchConfig()：viper 1.20.1 在收到目标文件的 REMOVE 事件
// 时会直接退出其事件循环，导致 watcher 永久失效；而 macOS 上的编辑器/VSCode/
// GoLand 保存文件采用的是「写临时文件 + rename 覆盖」的原子保存方式，会产生
// REMOVE 事件. 目录级监听不存在这个问题，可以持续恢复.
func watchConfigWithReload(options any) func() {
	configFile := viper.ConfigFileUsed()
	if configFile == "" {
		log.Warnw("Cannot watch configuration file: no config file was loaded")
		return func() {}
	}

	dir := filepath.Dir(configFile)
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Errorw(err, "Failed to create config file watcher")
		return func() {}
	}

	if err := watcher.Add(dir); err != nil {
		log.Errorw(err, "Failed to watch config directory", "dir", dir)
		_ = watcher.Close()
		return func() {}
	}

	stopCh := make(chan struct{})
	exited := make(chan struct{})
	var once sync.Once

	go func() {
		defer close(exited)

		var (
			timer  *time.Timer
			timerC <-chan time.Time
		)
		defer func() {
			if timer != nil {
				timer.Stop()
			}
		}()

		// 去抖：把编辑器保存产生的连续事件合并成一次重载.
		resetTimer := func() {
			if timer == nil {
				timer = time.NewTimer(configReloadDebounce)
				timerC = timer.C
				return
			}

			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(configReloadDebounce)
		}

		for {
			select {
			case <-stopCh:
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if !isWatchedConfigEvent(event, configFile) {
					continue
				}
				resetTimer()
			case <-timerC:
				reloadConfig(options)
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Errorw(err, "Config file watcher error")
			}
		}
	}()

	return func() {
		once.Do(func() {
			close(stopCh)
			_ = watcher.Close()
			// 等待监听 goroutine 退出，确保 stop 返回后不再有重载动作.
			<-exited
		})
	}
}

// isWatchedConfigEvent 判断文件事件是否与目标配置文件相关.
func isWatchedConfigEvent(event fsnotify.Event, configFile string) bool {
	if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove) == 0 {
		return false
	}

	eventName := filepath.Clean(event.Name)
	target := filepath.Clean(configFile)
	if eventName == target {
		return true
	}

	// 原子保存的临时文件通常以目标文件名作为前缀，例如 app.yaml.tmp.
	return strings.HasPrefix(filepath.Base(eventName), filepath.Base(target))
}

// reloadConfig 重新读取配置文件并应用到 options.
func reloadConfig(options any) {
	if err := viper.ReadInConfig(); err != nil {
		log.Errorw(err, "Failed to reload configuration file")
		return
	}

	if options != nil {
		if err := viper.Unmarshal(options); err != nil {
			log.Errorw(err, "Failed to apply reloaded configuration")
			return
		}

		if complete, ok := options.(interface{ Complete() error }); ok {
			if err := complete.Complete(); err != nil {
				log.Errorw(err, "Failed to complete reloaded options")
			}
		}
	}

	log.Infow("Configuration file reloaded", "file", viper.ConfigFileUsed())
}

// sensitiveKeyPatterns 命中这些子串的配置键在做打印时会脱敏.
var sensitiveKeyPatterns = []string{"password", "secret", "token", "key"}

// maskConfigValue 对敏感配置值做脱敏，避免密码等写入日志.
func maskConfigValue(key string, value any) any {
	lower := strings.ToLower(key)
	for _, pattern := range sensitiveKeyPatterns {
		if strings.Contains(lower, pattern) {
			return "***"
		}
	}

	return value
}

func PrintConfig() {
	for _, key := range viper.AllKeys() {
		log.Debugw(fmt.Sprintf("CFG: %s=%v", key, maskConfigValue(key, viper.Get(key))))
	}
}

func init() {
	pflag.StringVarP(&cfgFile, configFlagName, "c", cfgFile, "Read configuration from specified `FILE`, "+
		"support JSON, TOML, YAML, HCL, or Java properties formats.")
}
