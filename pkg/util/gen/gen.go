package gen

import (
	"fmt"
	"os"
	"path/filepath"
)

// OutDir 接收一个路径字符串，返回该路径的绝对路径并确保路径存在且是目录。
// 如果路径不存在或不是目录，则返回错误。返回的绝对路径始终以 '/' 结尾。
func OutDir(path string) (string, error) {
	// 将传入的相对路径或绝对路径转换为绝对路径.
	outDir, err := filepath.Abs(path)
	if err != nil {
		// 如果获取绝对路径失败，则返回空字符串和错误.
		return "", err
	}

	// 获取该绝对路径对应的文件或目录信息.
	stat, err := os.Stat(outDir)
	if err != nil {
		// 如果路径不存在或不允许访问，则返回空字符串和错误.
		return "", err
	}

	// 检查获取到的文件信息是否表示这是一个目录.
	if !stat.IsDir() {
		// 如果不是目录，则返回格式化后的错误信息.
		return "", fmt.Errorf("output directory %s is not a directory", outDir)
	}

	// 由于要返回的目录路径需要以 '/' 结尾，如果原路径没有以 '/' 结尾，则在末尾追加 '/'.
	outDir = outDir + "/"

	// 返回处理后的绝对路径.
	return outDir, nil
}
