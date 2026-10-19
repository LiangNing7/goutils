package file

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"regexp"

	"github.com/h2non/filetype"
	"github.com/h2non/filetype/types"
)

// FileType uses the filetype package to determine the given file path's type.
//
// 与旧实现的区别：os.Open 的错误不再被吞掉（旧实现文件不存在时返回 (unknown, nil)），
// 且文件句柄会被显式关闭，句柄的 Close 错误也会向上返回。
func FileType(filePath string) (types.Type, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return types.Unknown, err
	}

	// We only have to pass the file header = first 261 bytes
	head := make([]byte, 261)
	n, readErr := file.Read(head)
	if readErr != nil && readErr != io.EOF {
		// 读失败时也别忘了关闭句柄.
		_ = file.Close()
		return types.Unknown, readErr
	}

	// 先完成类型匹配，再关闭句柄.
	matched, matchErr := filetype.Match(head[:n])
	closeErr := file.Close()

	if matchErr != nil {
		return matched, matchErr
	}
	if closeErr != nil {
		return matched, closeErr
	}
	return matched, nil
}

// FileExists returns true if the given path exists.
func FileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	return false, err
}

// DirExists returns true if the given path exists and is a directory.
func DirExists(path string) (bool, error) {
	exists, _ := FileExists(path)
	fileInfo, _ := os.Stat(path)
	if !exists || !fileInfo.IsDir() {
		return false, fmt.Errorf("path either doesn't exist, or is not a directory <%s>", path)
	}
	return true, nil
}

// Touch creates an empty file at the given path if it doesn't already exist.
func Touch(path string) error {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		file, err := os.Create(path)
		if err != nil {
			return err
		}
		defer file.Close()
	}
	return nil
}

// EnsureDir will create a directory at the given path if it doesn't already exist.
//
// 语义：
//   - 路径不存在：创建目录（os.Mkdir），返回创建结果；
//   - 路径已存在且是目录：返回 nil（幂等）；
//   - 路径已存在但不是目录（例如普通文件）：返回错误（旧实现会错误地返回 nil）。
func EnsureDir(path string) error {
	info, err := os.Stat(path)
	switch {
	case err == nil:
		if !info.IsDir() {
			return fmt.Errorf("path %s already exists and is not a directory", path)
		}
		return nil
	case os.IsNotExist(err):
		return os.Mkdir(path, 0o755)
	default:
		return err
	}
}

// EnsureDirAll will create a directory at the given path along with any necessary parents if they don't already exist.
func EnsureDirAll(path string) error {
	return os.MkdirAll(path, 0o755)
}

// RemoveDir removes the given dir (if it exists) along with all of its contents.
func RemoveDir(path string) error {
	return os.RemoveAll(path)
}

// EmptyDir will recursively remove the contents of a directory at the given path.
func EmptyDir(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()

	names, err := d.Readdirnames(-1)
	if err != nil {
		return err
	}

	for _, name := range names {
		err = os.RemoveAll(filepath.Join(path, name))
		if err != nil {
			return err
		}
	}

	return nil
}

// ListDir will return the contents of a given directory path as a string slice.
func ListDir(path string) []string {
	files, err := os.ReadDir(path)
	if err != nil {
		path = filepath.Dir(path)
		files, _ = os.ReadDir(path)
	}

	//nolint: prealloc
	var dirPaths []string
	for _, file := range files {
		if !file.IsDir() {
			continue
		}
		dirPaths = append(dirPaths, filepath.Join(path, file.Name()))
	}
	return dirPaths
}

// GetHomeDirectory returns the path of the user's home directory.  ~ on Unix and C:\Users\UserName on Windows.
func GetHomeDirectory() string {
	currentUser, err := user.Current()
	if err != nil {
		panic(err)
	}
	return currentUser.HomeDir
}

// SafeMove move src to dst in safe mode.
func SafeMove(src, dst string) error {
	err := os.Rename(src, dst)
	//nolint: nestif
	if err != nil {
		fmt.Printf("[fileutil] unable to rename: \"%s\" due to %s. Falling back to copying.", src, err.Error())

		in, err := os.Open(src)
		if err != nil {
			return err
		}
		defer in.Close()

		out, err := os.Create(dst)
		if err != nil {
			return err
		}
		defer out.Close()

		_, err = io.Copy(out, in)
		if err != nil {
			return err
		}

		err = out.Close()
		if err != nil {
			return err
		}

		err = os.Remove(src)
		if err != nil {
			return err
		}
	}

	return nil
}

// IsZipFileUncompressed returns true if zip file in path is using 0 compression level.
func IsZipFileUncompressed(path string) (bool, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		fmt.Printf("Error reading zip file %s: %s\n", path, err)
		return false, err
	}
	defer r.Close()
	for _, f := range r.File {
		if f.FileInfo().IsDir() { // skip dirs, they always get store level compression
			continue
		}
		return f.Method == 0, nil // check compression level of first actual  file
	}
	return false, nil
}

// WriteFile writes file to path creating parent directories if needed.
func WriteFile(path string, file []byte) error {
	pathErr := EnsureDirAll(filepath.Dir(path))
	if pathErr != nil {
		return fmt.Errorf("cannot ensure path %s", pathErr)
	}

	err := os.WriteFile(path, file, 0o600)
	if err != nil {
		return fmt.Errorf("write error for thumbnail %s: %s ", path, err)
	}
	return nil
}

// GetIntraDir returns a string that can be added to filepath.Join to implement directory depth, "" on error
// eg given a pattern of 0af63ce3c99162e9df23a997f62621c5 and a depth of 2 length of 3
// returns 0af/63c or 0af\63c ( dependin on os)  that can be later used like this  filepath.Join(directory, intradir,
// basename).
func GetIntraDir(pattern string, depth, length int) string {
	if depth < 1 || length < 1 || (depth*length > len(pattern)) {
		return ""
	}
	intraDir := pattern[0:length] // depth 1 , get length number of characters from pattern
	for i := 1; i < depth; i++ {  // for every extra depth: move to the right of the pattern length positions, get length number of chars
		intraDir = filepath.Join(
			intraDir,
			pattern[length*i:length*(i+1)],
		) //  adding each time to intradir the extra characters with a filepath join
	}
	return intraDir
}

// GetParent returns the parent directory of the given path.
//
// 语义：路径以路径分隔符结尾（视为 root）或为空串时返回 nil，否则返回清理后的父目录。
// 旧实现对空串会 panic（切片越界），现已修复。
func GetParent(path string) *string {
	if path == "" {
		return nil
	}
	// 使用 os.IsPathSeparator 判断，兼容 Windows 的 '\'.
	if os.IsPathSeparator(path[len(path)-1]) {
		return nil
	}
	parentPath := filepath.Clean(path + string(os.PathSeparator) + "..")
	return &parentPath
}

// ServeFileNoCache serves the provided file, ensuring that the response
// contains headers to prevent caching.
func ServeFileNoCache(w http.ResponseWriter, r *http.Request, filepath string) {
	w.Header().Add("Cache-Control", "no-cache")

	http.ServeFile(w, r, filepath)
}

// MatchEntries returns a string slice of the entries in directory dir which
// match the regexp pattern. On error an empty slice is returned
// MatchEntries isn't recursive, only the specific 'dir' is searched
// without being expanded.
func MatchEntries(dir, pattern string) ([]string, error) {
	var res []string
	var err error

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	files, err := f.Readdirnames(-1)
	if err != nil {
		return nil, err
	}

	for _, file := range files {
		if re.Match([]byte(file)) {
			res = append(res, filepath.Join(dir, file))
		}
	}
	return res, err
}
