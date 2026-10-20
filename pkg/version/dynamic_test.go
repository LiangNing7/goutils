package version

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// resetDynamicVersion 在测试结束后恢复动态版本，避免污染其它用例.
func resetDynamicVersion(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { dynamicGitVersion.Store(gitVersion) })
}

// TestSetDynamicVersion_DefaultPlaceholder 覆盖默认 gitVersion 含 $Format:%h$ 占位符时
// SetDynamicVersion 恒失败（占位符大小写不一致导致解析失败）的问题.
func TestSetDynamicVersion_DefaultPlaceholder(t *testing.T) {
	resetDynamicVersion(t)

	assert.Contains(t, gitVersion, "$Format:", "前置条件：默认 gitVersion 是占位符")

	assert.NoError(t, SetDynamicVersion("v0.0.0-master+abc1234"),
		"SetDynamicVersion should accept a version matching the placeholder default")
	assert.Equal(t, "v0.0.0-master+abc1234", Get().GitVersion,
		"Get() should return the dynamic version")

	// 动态版本中携带的 commit 会覆盖仍是占位符的 gitCommit
	assert.Equal(t, "abc1234", Get().GitCommit)

	// 其它字段不受影响
	assert.Equal(t, buildDate, Get().BuildDate)
	assert.Equal(t, gitTreeState, Get().GitTreeState)
}

// TestGet_DynamicVersionWithoutCommit 覆盖动态版本不含 commit 信息时 gitCommit 保持原值.
func TestGet_DynamicVersionWithoutCommit(t *testing.T) {
	resetDynamicVersion(t)

	assert.NoError(t, SetDynamicVersion("v0.0.0-master"))
	assert.Equal(t, "v0.0.0-master", Get().GitVersion)
	assert.Equal(t, gitCommit, Get().GitCommit, "placeholder gitCommit should stay untouched")
}

// TestGet_DynamicVersionDoesNotOverrideRealCommit 覆盖已有真实 gitCommit 时不被覆盖.
func TestGet_DynamicVersionDoesNotOverrideRealCommit(t *testing.T) {
	resetDynamicVersion(t)

	old := gitCommit
	gitCommit = "deadbeef" // 模拟 -ldflags 注入的真实 commit
	t.Cleanup(func() { gitCommit = old })

	assert.NoError(t, SetDynamicVersion("v0.0.0-master+abc1234"))
	assert.Equal(t, "v0.0.0-master+abc1234", Get().GitVersion)
	assert.Equal(t, "deadbeef", Get().GitCommit, "a real commit must win over dynamic metadata")
}

// TestSetDynamicVersion_Invalid 覆盖非法动态版本被拒绝且不改变当前有效版本.
func TestSetDynamicVersion_Invalid(t *testing.T) {
	resetDynamicVersion(t)

	assert.Error(t, SetDynamicVersion(""), "empty version must be rejected")
	assert.Error(t, SetDynamicVersion("not-a-version"))
	// major/minor/patch 必须与默认版本一致（默认 v0.0.0）
	assert.Error(t, SetDynamicVersion("v1.2.3"))
	assert.Error(t, SetDynamicVersion("v0.0.1"))
	assert.Error(t, SetDynamicVersion("v0.1.0"))

	// 全部失败后，Get() 仍返回默认版本
	assert.Equal(t, gitVersion, Get().GitVersion)

	// no-op：与默认版本相同是允许的
	assert.NoError(t, SetDynamicVersion(gitVersion))
	assert.Equal(t, gitVersion, Get().GitVersion)

	// 设置成功后，再设置非法版本不应改变已生效的动态版本
	assert.NoError(t, SetDynamicVersion("v0.0.0-master+ok123"))
	assert.Error(t, SetDynamicVersion("v1.0.0"))
	assert.Equal(t, "v0.0.0-master+ok123", Get().GitVersion)
}

// TestValidateDynamicVersion 覆盖校验函数本身.
func TestValidateDynamicVersion(t *testing.T) {
	assert.Error(t, ValidateDynamicVersion(""))
	assert.Error(t, ValidateDynamicVersion("abc"))
	assert.NoError(t, ValidateDynamicVersion("v0.0.0-master+abc"))
	assert.Error(t, ValidateDynamicVersion("v1.0.0"))
}

// TestValidateDynamicVersion_CustomPlaceholder 覆盖自定义占位符默认版本.
func TestValidateDynamicVersion_CustomPlaceholder(t *testing.T) {
	// 占位符出现在非默认版本中时也要按占位符处理
	assert.NoError(t, validateDynamicVersion("v0.0.0-master+abc", "v0.0.0-master+$Format:%H$"))
	assert.NoError(t, validateDynamicVersion("v0.0.0-master+abc", "v0.0.0-master+$Format:%h$"))
	assert.Error(t, validateDynamicVersion("v1.0.0", "v0.0.0-master+$Format:%H$"))
}
