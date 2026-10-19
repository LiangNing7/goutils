package rid_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/LiangNing7/goutils/pkg/rid"
)

const (
	// UserID 定义用户资源标识符.
	UserID rid.ResourceID = "user"
	// PostID 定义博文资源标识符.
	PostID rid.ResourceID = "post"
)

// Mock Salt function used for testing
func Salt() string {
	return "staticSalt"
}

func TestResourceID_String(t *testing.T) {
	// 测试 UserID 转换为字符串
	userID := UserID
	assert.Equal(t, "user", userID.String(), "UserID.String() should return 'user'")

	// 测试 PostID 转换为字符串
	postID := PostID
	assert.Equal(t, "post", postID.String(), "PostID.String() should return 'post'")
}

func TestResourceID_New(t *testing.T) {
	// 测试生成的ID是否带有正确前缀
	userID := UserID
	uniqueID := userID.New(1)

	assert.True(t, len(uniqueID) > 0, "Generated ID should not be empty")
	assert.Contains(t, uniqueID, "user-", "Generated ID should start with 'user-' prefix")

	// 生成另外一个唯一标识符，确保生成的值不同（唯一性）
	anotherID := userID.New(2)
	assert.NotEqual(t, uniqueID, anotherID, "Generated IDs should be unique")
}

func TestResourceID_New_Deterministic(t *testing.T) {
	// Salt() 依赖机器 ID（/etc/machine-id 或 hostname），同一进程内应保持稳定；
	// 若宿主机既没有 machine-id 也无法拿到 hostname，Salt() 会回退为随机数，
	// 此时确定性无法保证，跳过该断言.
	if rid.Salt() != rid.Salt() {
		t.Skip("no stable machine id available on this host")
	}

	// 相同 prefix + 相同 counter 必须得到完全相同的结果（可重复性）.
	assert.Equal(t, UserID.New(1), UserID.New(1))
	assert.Equal(t, PostID.New(1), PostID.New(1))

	// 不同 counter 必须得到不同的结果.
	seen := make(map[string]struct{})
	for counter := uint64(1); counter <= 9; counter++ {
		generated := UserID.New(counter)
		_, duplicated := seen[generated]
		assert.False(t, duplicated, "generated ids must be unique, got duplicate %s", generated)
		seen[generated] = struct{}{}
	}

	// 不同的 resource id 前缀互不影响.
	assert.True(t, strings.HasPrefix(UserID.New(1), "user-"))
	assert.True(t, strings.HasPrefix(PostID.New(1), "post-"))
	assert.NotEqual(t, UserID.New(1), PostID.New(1))
}

func TestResourceID_New_Structure(t *testing.T) {
	const charset = "abcdefghijklmnopqrstuvwxyz1234567890"

	generated := UserID.New(1)
	parts := strings.SplitN(generated, "-", 2)
	assert.Len(t, parts, 2, "generated id should be <prefix>-<code>")

	// 前缀
	assert.Equal(t, "user", parts[0])
	// code 长度固定为 6
	code := []rune(parts[1])
	assert.Len(t, code, 6)
	// code 字符落在默认字符集内
	for _, r := range code {
		assert.Contains(t, charset, string(r), "code %q contains unexpected char %q", parts[1], r)
	}

	// 自定义前缀
	custom := rid.NewResourceID("order").New(1)
	assert.True(t, strings.HasPrefix(custom, "order-"))
	assert.Len(t, strings.SplitN(custom, "-", 2)[1], 6)
}

func BenchmarkResourceID_New(b *testing.B) {
	// 性能测试
	b.ResetTimer()
	var i uint64
	for b.Loop() {
		userID := UserID
		_ = userID.New(i)
		i++
	}
}

func FuzzResourceID_New(f *testing.F) {
	// 添加预置测试数据
	f.Add(uint64(1))      // 添加一个种子值counter为1
	f.Add(uint64(123456)) // 添加一个较大的种子值

	f.Fuzz(func(t *testing.T, counter uint64) {
		// 测试UserID的New方法
		result := UserID.New(counter)

		// 断言结果不为空
		assert.NotEmpty(t, result, "The generated unique identifier should not be empty")

		// 断言结果必须包含资源标识符前缀
		assert.Contains(t, result, UserID.String()+"-", "The generated unique identifier should contain the correct prefix")

		// 断言前缀不会与uniqueStr部分重叠
		splitParts := strings.SplitN(result, "-", 2)
		assert.Equal(t, UserID.String(), splitParts[0], "The prefix part of the result should correctly match the UserID")

		// 断言生成的ID具有固定长度（基于NewCode的配置）
		if len(splitParts) == 2 {
			assert.Equal(t, 6, len(splitParts[1]), "The unique identifier part should have a length of 6")
		} else {
			t.Errorf("The format of the generated unique identifier does not meet expectation")
		}
	})
}
