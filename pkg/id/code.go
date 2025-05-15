package id

// NewCode can get a unique code by id(You need to ensure that id is unique).
func NewCode(id uint64, options ...func(*CodeOptions)) string {
	opts := getCodeOptionsOrSetDefault(nil)
	for _, f := range options {
		f(opts)
	}

	// enlarge and add salt.
	id = id*uint64(opts.n1) + opts.salt

	var code []rune
	slIdx := make([]byte, opts.l)

	charLen := len(opts.chars)
	charLenUI := uint64(charLen)

	// diffusion.
	for i := range opts.l {
		slIdx[i] = byte(id % charLenUI)                          // get each number
		slIdx[i] = (slIdx[i] + byte(i)*slIdx[0]) % byte(charLen) // let units digit affect other digit
		id /= charLenUI                                          // right shift
	}

	// confusion(https://en.wikipedia.org/wiki/Permutation_box)
	for i := range opts.l {
		idx := (byte(i) * byte(opts.n2) % byte(opts.l))
		code = append(code, opts.chars[slIdx[idx]])
	}
	return string(code)
}
