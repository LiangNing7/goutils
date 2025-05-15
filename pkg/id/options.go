package id

import "time"

type CodeOptions struct {
	chars []rune // 用于组成编码的字符集
	n1    int    // 用于步长计算的参数，与 len(chars) 互质
	n2    int    // 用于步长计算的参数，与 l 互质
	l     int    // 最终编码的长度
	salt  uint64 // 随机扰动值
}

// WithCodeChars 设置 chars.
func WithCodeChars(arr []rune) func(*CodeOptions) {
	return func(options *CodeOptions) {
		if len(arr) > 0 {
			getCodeOptionsOrSetDefault(options).chars = arr
		}
	}
}

// WithCodeN1 设置 n1.
func WithCodeN1(n int) func(*CodeOptions) {
	return func(options *CodeOptions) {
		getCodeOptionsOrSetDefault(options).n1 = n
	}
}

// WithCodeN2 设置 n2.
func WithCodeN2(n int) func(*CodeOptions) {
	return func(options *CodeOptions) {
		getCodeOptionsOrSetDefault(options).n2 = n
	}
}

// WithCodeL 设置 l.
func WithCodeL(l int) func(*CodeOptions) {
	return func(options *CodeOptions) {
		if l > 0 {
			getCodeOptionsOrSetDefault(options).l = l
		}
	}
}

// WithCodeSalt 设置 salt.
func WithCodeSalt(salt uint64) func(*CodeOptions) {
	return func(options *CodeOptions) {
		if salt > 0 {
			getCodeOptionsOrSetDefault(options).salt = salt
		}
	}
}

func getCodeOptionsOrSetDefault(options *CodeOptions) *CodeOptions {
	if options == nil {
		return &CodeOptions{
			// base string set, remove 0,1,I,O,U,Z.
			// 消除视觉与语义上的歧义.
			chars: []rune{
				'2', '3', '4', '5', '6',
				'7', '8', '9', 'A', 'B',
				'C', 'D', 'E', 'F', 'G',
				'H', 'J', 'K', 'L', 'M',
				'N', 'P', 'Q', 'R', 'S',
				'T', 'V', 'W', 'X', 'Y',
			},
			// n1 / len(chars)=30 coprime.
			// n1 要与 len(chars) 互质.
			n1: 17,
			// n2 / l cop rime.
			// n2 要与 l 互质.
			n2: 5,
			// code length.
			l: 8,
			// random number.
			// 随机扰动.
			salt: 123567369,
		}
	}
	return options
}

// SonyflakeOptions 生成 Sonyflake 的配置.
type SonyflakeOptions struct {
	machineId uint16    // 机器编号
	startTime time.Time // 生成器的起始时间戳
}

// WithSonyflakeMachineId 设置机器 ID.
func WithSonyflakeMachineId(id uint16) func(*SonyflakeOptions) {
	return func(options *SonyflakeOptions) {
		if id > 0 {
			getSonyflakeOptionsOrSetDefault(options).machineId = id
		}
	}
}

// WithSonyflakeStartTime 设置生成器的起始时间戳.
func WithSonyflakeStartTime(startTime time.Time) func(*SonyflakeOptions) {
	return func(options *SonyflakeOptions) {
		if !startTime.IsZero() {
			getSonyflakeOptionsOrSetDefault(options).startTime = startTime
		}
	}
}

// getSonyflakeOptionsOrSetDefault 获取或设置默认值.
// 默认机器 ID: 1.
// 起始时间 2022-10-10 UTC.
func getSonyflakeOptionsOrSetDefault(options *SonyflakeOptions) *SonyflakeOptions {
	if options == nil {
		return &SonyflakeOptions{
			machineId: 1,
			startTime: time.Date(2022, 10, 10, 0, 0, 0, 0, time.UTC),
		}
	}
	return options
}
