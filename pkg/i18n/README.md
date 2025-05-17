# i18n

> 基于 [go-i18n](https://github.com/nicksnyder/go-i18n) 的多语种国际化实现。
>
> ```bash
> $ go get -u github.com/LiangNing7/goutils/pkg/i18n
> ```

## Usage

新建语言资源文件。

```bash
mkdir locales

cat <<EOF > locales/en.yml
hello.world: Hello world!
EOF

cat <<EOF > locales/zh.yml
hello.world: 你好, 世界!
EOF
```

```go
package main

import (
	"embed"
	"fmt"
	"golang.org/x/text/language"
	"github.com/onexstack/onexstack/pkg/i18n"
)

//go:embed locales
var locales embed.FS

func main() {
	i := i18n.New(
		i18n.WithFormat("yml"),
		// with absolute files
		i18n.WithFile("locales/en.yml"),
		i18n.WithFile("locales/zh.yml"),
		// with go embed files
		// i18n.WithFs(locales),
		i18n.WithLanguage(language.Chinese),
	)

	// print string
	fmt.Println(i.T("hello.world"))
	// 你好, 世界!

	// print error
	fmt.Println(i.E("hello.world").Error() == "你好, 世界!")
	// true

	// override default language
	fmt.Println(i.Select(language.English).T("hello.world"))
	// Hello world!
}
```

## Options

* `WithFormat` - language file format, default yml
* `WithLanguage` - set default language file format, default en
* `WithFile` - set language files by file system
* `WithFs` - set language files by go embed file
