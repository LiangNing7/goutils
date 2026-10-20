package id

import "fmt"

const (
	// DefaultCodeLength 是 code 的默认长度.
	DefaultCodeLength = 8

	// MaxCodeLength 是 code 长度的上限.
	// 超过该上限的配置被视为非法，避免非法配置导致超大的内存分配.
	MaxCodeLength = 1024
)

// NewCode can get a unique code by id(You need to ensure that id is unique).
//
// 当 options 非法时（例如字符集少于 2 个字符、code 长度超过 MaxCodeLength），
// NewCode 会退回默认配置并返回可用的 code，不会 panic；
// 若需要显式感知非法配置，请使用 NewCodeE.
func NewCode(id uint64, options ...func(*CodeOptions)) string {
	code, err := NewCodeE(id, options...)
	if err != nil {
		// 配置非法：退回默认配置，保证返回可用的 code 且不 panic.
		code, _ = NewCodeE(id)
	}
	return code
}

// NewCodeE 与 NewCode 行为一致，但当 options 非法时返回错误而不是退回默认配置.
func NewCodeE(id uint64, options ...func(*CodeOptions)) (string, error) {
	opts := getCodeOptionsOrSetDefault(nil)
	for _, f := range options {
		f(opts)
	}

	if err := opts.validate(); err != nil {
		return "", err
	}

	return encodeCode(id, opts), nil
}

// validate 校验配置的合法性.
func (opts *CodeOptions) validate() error {
	charLen := len(opts.chars)
	if charLen < 2 {
		return fmt.Errorf("id: code charset must contain at least 2 runes, got %d", charLen)
	}
	if opts.l < 1 || opts.l > MaxCodeLength {
		return fmt.Errorf("id: code length must be within [1, %d], got %d", MaxCodeLength, opts.l)
	}
	return nil
}

// encodeCode 根据配置把 id 编码为 code.
// 所有中间计算均使用 uint64/int，避免出现宽字符集（>= 256 个 rune）时的溢出与除零 panic.
func encodeCode(id uint64, opts *CodeOptions) string {
	// enlarge and add salt.
	id = id*uint64(opts.n1) + opts.salt

	l := opts.l
	charLen := len(opts.chars)
	charLenUI := uint64(charLen)

	slIdx := make([]uint64, l)

	// diffusion.
	for i := range l {
		slIdx[i] = id % charLenUI                              // get each number
		slIdx[i] = (slIdx[i] + uint64(i)*slIdx[0]) % charLenUI // let units digit affect other digit
		id /= charLenUI                                        // right shift
	}

	// confusion(https://en.wikipedia.org/wiki/Permutation_box)
	n2 := opts.n2 % l
	if n2 < 0 {
		n2 += l
	}
	code := make([]rune, 0, l)
	for i := range l {
		idx := (i * n2) % l
		code = append(code, opts.chars[int(slIdx[idx])])
	}
	return string(code)
}
