package version

import (
	"fmt"
	"strings"
	"sync/atomic"

	utilversion "github.com/LiangNing7/goutils/pkg/util/version"
)

var dynamicGitVersion atomic.Value

func init() {
	// initialize to static gitVersion
	dynamicGitVersion.Store(gitVersion)
}

// dynamicVersion 返回当前生效的版本号（默认为静态 gitVersion）.
func dynamicVersion() string {
	return dynamicGitVersion.Load().(string)
}

// dynamicCommit 从动态版本中提取 commit（semver 的 build metadata 部分）.
// 例如 "v0.0.0-master+abc1234" 返回 "abc1234"；没有 build metadata 时返回空字符串.
func dynamicCommit() string {
	v := dynamicVersion()
	if v == gitVersion {
		// 未设置动态版本
		return ""
	}
	if i := strings.LastIndex(v, "+"); i >= 0 && i+1 < len(v) {
		return v[i+1:]
	}
	return ""
}

// SetDynamicVersion overrides the version returned as the GitVersion from Get().
// The specified version must be non-empty, a valid semantic version, and must
// match the major/minor/patch version of the default gitVersion.
func SetDynamicVersion(dynamicVersion string) error {
	if err := ValidateDynamicVersion(dynamicVersion); err != nil {
		return err
	}
	dynamicGitVersion.Store(dynamicVersion)
	return nil
}

// ValidateDynamicVersion ensures the given version is non-empty, a valid semantic version,
// and matched the major/minor/patch version of the default gitVersion.
func ValidateDynamicVersion(dynamicVersion string) error {
	return validateDynamicVersion(dynamicVersion, gitVersion)
}

func validateDynamicVersion(dynamicVersion, defaultVersion string) error {
	if len(dynamicVersion) == 0 {
		return fmt.Errorf("version must not be empty")
	}
	if dynamicVersion == defaultVersion {
		// allow no-op
		return nil
	}
	vRuntime, err := utilversion.ParseSemantic(dynamicVersion)
	if err != nil {
		return err
	}
	// must match major/minor/patch of default version
	var vDefault *utilversion.Version
	if strings.Contains(defaultVersion, formatPlaceholderTag) {
		// 占位符（例如 v0.0.0-master+$Format:%h$ / $Format:%H$）无法作为语义化版本解析，
		// 统一退化为 v0.0.0-master 进行比较.
		vDefault, err = utilversion.ParseSemantic("v0.0.0-master")
	} else {
		vDefault, err = utilversion.ParseSemantic(defaultVersion)
	}
	if err != nil {
		return err
	}
	if vRuntime.Major() != vDefault.Major() || vRuntime.Minor() != vDefault.Minor() || vRuntime.Patch() != vDefault.Patch() {
		return fmt.Errorf("version %q must match major/minor/patch of default version %q", dynamicVersion, defaultVersion)
	}
	return nil
}
