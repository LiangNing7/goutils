package file

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/h2non/filetype"
)

// pngSignature 是 PNG 文件的魔数，用于构造一个可被 filetype 识别的真实文件.
var pngSignature = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

// TestFileTypeNotExist 断言对不存在的文件返回错误（旧实现吞掉 os.Open 的错误并返回 (unknown, nil)）.
func TestFileTypeNotExist(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.bin")

	ty, err := FileType(missing)
	if err == nil {
		t.Fatalf("FileType(%q) err = nil, want non-nil error (type=%v)", missing, ty)
	}
	if !os.IsNotExist(err) {
		t.Fatalf("FileType(%q) err = %v, want a not-exist error", missing, err)
	}
}

// TestFileTypeValid 断言对真实文件能正确识别类型.
func TestFileTypeValid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.png")
	if err := os.WriteFile(path, pngSignature, 0o600); err != nil {
		t.Fatal(err)
	}

	want, err := filetype.Match(pngSignature)
	if err != nil {
		t.Fatal(err)
	}

	ty, err := FileType(path)
	if err != nil {
		t.Fatalf("FileType(%q) unexpected error: %v", path, err)
	}
	if ty != want {
		t.Fatalf("FileType(%q) = %v, want %v", path, ty, want)
	}
	if ty == filetype.Unknown {
		t.Fatalf("FileType(%q) = unknown, want a recognized png type", path)
	}
}

// TestFileTypeNoFDLeak 断言对已存在文件反复调用 FileType 不会泄漏文件描述符.
func TestFileTypeNoFDLeak(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("/dev/fd is not available on windows")
	}
	path := filepath.Join(t.TempDir(), "sample.png")
	if err := os.WriteFile(path, pngSignature, 0o600); err != nil {
		t.Fatal(err)
	}

	// 预热一次，排除首次惰性初始化带来的偏差.
	if _, err := FileType(path); err != nil {
		t.Fatal(err)
	}
	before, err := openFDCount()
	if err != nil {
		t.Skipf("cannot inspect open fds: %v", err)
	}

	const n = 200
	for i := 0; i < n; i++ {
		if _, err := FileType(path); err != nil {
			t.Fatal(err)
		}
	}
	// 刻意不调用 runtime.GC()：如果 FileType 没有显式 Close，
	// os.File 的 runtime finalizer 只会在 GC 时才回收 fd，
	// 这里要验证的是「不依赖 GC 也不泄漏」。
	after, err := openFDCount()
	if err != nil {
		t.Skipf("cannot inspect open fds: %v", err)
	}

	if after-before > 16 {
		t.Fatalf("FileType leaked file descriptors: before=%d after=%d (called %d times)", before, after, n)
	}
}

func openFDCount() (int, error) {
	// 注意：不能用 os.ReadDir，它会对每个条目做 lstat，
	// 而 /dev/fd 下的条目无法被 lstat（会返回 bad file descriptor）。
	d, err := os.Open("/dev/fd")
	if err != nil {
		return 0, err
	}
	defer d.Close()
	names, err := d.Readdirnames(-1)
	if err != nil {
		return 0, err
	}
	return len(names), nil
}

// TestEnsureDirOnRegularFile 断言当目标路径已存在且是普通文件时返回错误.
func TestEnsureDirOnRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := EnsureDir(path); err == nil {
		t.Fatalf("EnsureDir(%q) err = nil, want non-nil error (target is a file, not a directory)", path)
	}
}

// TestEnsureDirCreates 断言目录不存在时会创建，且重复调用幂等.
func TestEnsureDirCreates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "dir")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := EnsureDir(path); err != nil {
		t.Fatalf("EnsureDir(%q) unexpected error: %v", path, err)
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		t.Fatalf("EnsureDir(%q) did not create a directory: %v", path, err)
	}
	if err := EnsureDir(path); err != nil {
		t.Fatalf("EnsureDir(%q) second call unexpected error: %v", path, err)
	}
}

// TestGetParent 断言 GetParent 的边界行为，尤其是空串不再 panic.
func TestGetParent(t *testing.T) {
	if got := GetParent(""); got != nil {
		t.Fatalf("GetParent(\"\") = %v, want nil", *got)
	}

	if got := GetParent("/a/b"); got == nil || *got != "/a" {
		t.Fatalf("GetParent(\"/a/b\") = %v, want \"/a\"", got)
	}

	// 以分隔符结尾视为 root，返回 nil.
	if got := GetParent("/a/"); got != nil {
		t.Fatalf("GetParent(\"/a/\") = %v, want nil", *got)
	}
}
