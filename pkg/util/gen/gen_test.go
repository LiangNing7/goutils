package gen

import (
	"os"
	"strings"
	"testing"
)

func TestValidDir(t *testing.T) {
	_, err := OutDir("./")
	if err != nil {
		t.Fatal(err)
	}
}

func TestInvalidDir(t *testing.T) {
	_, err := OutDir("./nondir")
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestNotDir(t *testing.T) {
	_, err := OutDir("./genutils_test.go")
	if err == nil {
		t.Fatal("expected an error")
	}
}

// TestOutDirTrailingSeparator 断言返回值以「平台路径分隔符」结尾，而不是硬编码的 '/'。
// 在 Unix 上两者相同；在 Windows 上旧实现返回 '/' 会与本断言不符（跨平台回归）。
func TestOutDirTrailingSeparator(t *testing.T) {
	dir, err := OutDir("./")
	if err != nil {
		t.Fatal(err)
	}
	sep := string(os.PathSeparator)
	if !strings.HasSuffix(dir, sep) {
		t.Fatalf("OutDir result %q must end with the platform separator %q", dir, sep)
	}
}
