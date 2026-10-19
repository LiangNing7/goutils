package id

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestNewSonyflake_StartTimeInFuture 覆盖 StartTime 在未来时 NewSonyflake 内部
// ins 为 nil 但仍然调用 ins.NextID() 导致空指针 panic 的问题.
func TestNewSonyflake_StartTimeInFuture(t *testing.T) {
	var sf *Sonyflake
	assert.NotPanics(t, func() {
		sf = NewSonyflake(
			WithSonyflakeMachineId(1),
			WithSonyflakeStartTime(time.Now().Add(24*time.Hour)),
		)
	})

	assert.NotNil(t, sf)
	assert.Error(t, sf.Error, "future start time should be reported as an error")
	assert.NotPanics(t, func() {
		assert.Zero(t, sf.Id(context.Background()))
	})
}

// TestNewSonyflake_OK 覆盖正常的构造路径.
func TestNewSonyflake_OK(t *testing.T) {
	sf := NewSonyflake(WithSonyflakeMachineId(1))
	assert.NoError(t, sf.Error)

	id1 := sf.Id(context.Background())
	assert.NotZero(t, id1)

	id2 := sf.Id(context.Background())
	assert.NotZero(t, id2)
	assert.NotEqual(t, id1, id2)
}

// TestNewSonyflake_DefaultOptions 覆盖默认配置（无任何 option）.
func TestNewSonyflake_DefaultOptions(t *testing.T) {
	sf := NewSonyflake()
	assert.NoError(t, sf.Error)
	assert.NotPanics(t, func() { sf.Id(context.Background()) })
}
