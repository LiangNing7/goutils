package version

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"

	"github.com/gosuri/uitable"
)

const (
	// gitVersionPlaceholder 是 gitVersion 的占位符默认值（由 git 的 archive 扩展或构建脚本填充）.
	gitVersionPlaceholder = "v0.0.0-master+$Format:%h$"
	// gitCommitPlaceholder 是 gitCommit 的占位符默认值.
	gitCommitPlaceholder = "$Format:%H$"
	// formatPlaceholderTag 是占位符的公共前缀，用于判断版本信息是否已被 -ldflags 注入.
	formatPlaceholderTag = "$Format:"
)

var (
	// gitVersion 是语义化的版本号.
	gitVersion = gitVersionPlaceholder
	// buildDate 是 ISO8601 格式的构建时间, $(date -u +'%Y-%m-%dT%H:%M:%SZ') 命令的输出.
	buildDate = "1970-01-01T00:00:00Z"
	// gitCommit 是 Git 的 SHA1 值，$(git rev-parse HEAD) 命令的输出.
	gitCommit = gitCommitPlaceholder
	// gitTreeState 代表构建时 Git 仓库的状态，可能的值有：clean, dirty.
	gitTreeState = ""
)

// Info 包含了版本信息.
type Info struct {
	GitVersion   string `json:"gitVersion"`
	GitCommit    string `json:"gitCommit"`
	GitTreeState string `json:"gitTreeState"`
	BuildDate    string `json:"buildDate"`
	GoVersion    string `json:"goVersion"`
	Compiler     string `json:"compiler"`
	Platform     string `json:"platform"`
}

// String 返回人性化的版本信息字符串.
func (info Info) String() string {
	return info.GitVersion
}

// ToJSON 以 JSON 格式返回版本信息.
func (info Info) ToJSON() string {
	s, _ := json.Marshal(info)

	return string(s)
}

// Text 将版本信息编码为 UTF-8 格式的文本，并返回.
func (info Info) Text() string {
	table := uitable.New()
	table.RightAlign(0)
	table.MaxColWidth = 80
	table.Separator = " "
	table.AddRow("gitVersion:", info.GitVersion)
	table.AddRow("gitCommit:", info.GitCommit)
	table.AddRow("gitTreeState:", info.GitTreeState)
	table.AddRow("buildDate:", info.BuildDate)
	table.AddRow("goVersion:", info.GoVersion)
	table.AddRow("compiler:", info.Compiler)
	table.AddRow("platform:", info.Platform)

	return table.String()
}

// Get 返回详尽的代码库版本信息，用来标明二进制文件由哪个版本的代码构建.
func Get() Info {
	// 以下变量通常由 -ldflags 进行设置
	return Info{
		// GitVersion 优先使用运行期设置的动态版本（SetDynamicVersion）.
		GitVersion:   dynamicVersion(),
		GitCommit:    currentGitCommit(),
		GitTreeState: gitTreeState,
		BuildDate:    buildDate,
		GoVersion:    runtime.Version(),
		Compiler:     runtime.Compiler,
		Platform:     fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}
}

// currentGitCommit 返回当前生效的 commit：
// 若 gitCommit 仍是未被 -ldflags 注入的占位符，则使用动态版本中携带的 commit 信息.
func currentGitCommit() string {
	if !strings.Contains(gitCommit, formatPlaceholderTag) {
		// 已被 -ldflags 注入真实 commit，直接使用
		return gitCommit
	}
	if commit := dynamicCommit(); commit != "" && !strings.Contains(commit, formatPlaceholderTag) {
		return commit
	}
	return gitCommit
}
