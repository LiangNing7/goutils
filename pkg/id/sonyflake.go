package id

import (
	"context"
	"fmt"
	"time"

	"github.com/sony/sonyflake"
)

type Sonyflake struct {
	ops   SonyflakeOptions     // 配置项，机器 ID，起始时间.
	sf    *sonyflake.Sonyflake // Sonyflake 库内部的实例.
	Error error                // 构造或首次调用时如果出错，会记录错误状态，后续直接返回空 ID.
}

// NewSonyflake can get a unique code by id(You need to ensure that id is unique).
func NewSonyflake(options ...func(*SonyflakeOptions)) *Sonyflake {
	// 使用配置函数修改默认参数.
	opts := getSonyflakeOptionsOrSetDefault(nil)
	for _, f := range options {
		f(opts)
	}
	sf := &Sonyflake{
		ops: *opts,
	}

	// 使用配置项构造 sonyflake.Settings.
	st := sonyflake.Settings{
		StartTime: opts.startTime,
	}
	// 如果用户指定了 machineId,
	// 则给 Settings.MachineID 赋值一个固定返回此 ID 的函数.
	if opts.machineId > 0 {
		st.MachineID = func() (uint16, error) {
			return opts.machineId, nil
		}
	}

	// 创建 Sonyflake 实例.
	ins := sonyflake.NewSonyflake(st)
	if ins == nil {
		sf.Error = fmt.Errorf("create sonyflake failed")
	}

	// 立即调用 ins.NextID() 试生成一次，校验 StartTime 是否有效.
	_, err := ins.NextID()
	if err != nil {
		sf.Error = fmt.Errorf("invalid start time")
	}
	// 将 ins 存入 sf.sf，并返回 *Sonyflake.
	sf.sf = ins
	return sf
}

func (s *Sonyflake) Id(ctx context.Context) (id uint64) {
	// 若构造阶段有误，则直接返回默认值 0.
	if s.Error != nil {
		return
	}

	// 一次性尝试，调用 s.sf.NextID() 生成新 ID.
	var err error
	id, err = s.sf.NextID()
	if err == nil {
		return
	}

	// 指数级退避重试.
	sleep := 1
	for {
		time.Sleep(time.Duration(sleep) * time.Millisecond)
		id, err = s.sf.NextID()
		if err != nil {
			return
		}
		sleep *= 2
	}
}
