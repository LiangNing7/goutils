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
