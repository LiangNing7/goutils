package pagination

import "testing"

// TestGetPageOffset 断言 offset 在各种边界输入下都不会为负数，并符合 clamp 语义.
func TestGetPageOffset(t *testing.T) {
	cases := []struct {
		name             string
		pageNum, pageSize, want int64
	}{
		{"normal", 3, 10, 20},
		{"first-page", 1, 10, 0},
		{"page-zero", 0, 10, 0},
		{"page-negative", -5, 10, 0},
		{"size-zero", 2, 0, 0},
		{"size-negative", 2, -1, 0},
		{"both-zero", 0, 0, 0},
		{"page-and-size-negative", -3, -7, 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := GetPageOffset(c.pageNum, c.pageSize)
			if got != c.want {
				t.Fatalf("GetPageOffset(%d, %d) = %d, want %d", c.pageNum, c.pageSize, got, c.want)
			}
			if got < 0 {
				t.Fatalf("GetPageOffset(%d, %d) = %d, offset must never be negative", c.pageNum, c.pageSize, got)
			}
		})
	}
}
