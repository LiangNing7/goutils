# addlicense

如果一个现有的项目，想要开源，免不了要为项目中的文件增加开源协议头信息。虽然很多 IDE 都可以为新创建的文件自动增加头信息，但修改已有的文件还是要麻烦些。好在我们有 `addlicense` 工具可以使用，一行命令就能搞定。并且 `addlicense` 是用 Go 语言开发的，本文不仅教你如何使用，还会对其源码进行分析讲解。

## 安装

使用如下命令安装 `addlicense`：

```bash
$ go install github.com/LiangNing7/goutils/addlicense@latest
```

使用 `-h/--help` 查看帮助信息：

```bash
$ addlicense -h
Usage: addlicense [flags] pattern [pattern ...]

The program ensures source code files have copyright license headers
by scanning directory patterns recursively.

It modifies all source files in place and avoids adding a license header
to any file that already has one.

The pattern argument can be provided multiple times, and may also refer
to single files.

Flags:

      --check                check only mode: verify presence of license headers and exit with non-zero code if missing
  -h, --help                 show this help message
  -c, --holder string        copyright holder (default "Google LLC")
  -l, --license string       license type: apache, bsd, mit, mpl (default "apache")
  -f, --licensef string      custom license file (no default)
      --skip-dirs strings    regexps of directories to skip
      --skip-files strings   regexps of files to skip
  -v, --verbose              verbose mode: print the name of the files that are modified
  -y, --year string          copyright year(s) (default "2025")
```

参数说明：

* `--check` 只检查文件是否存在 License，执行后会打印所有不包含 License 版权头信息的文件名。
* `-h/--help` 查看 `addlicense` 使用帮助信息，我们已经使用过了。
* `-c/--holder` 指定 License 的版权所有者。
* `-l/--license` 指定 License 的协议类型，目前内置支持了 `Apache 2.0`、`BSD`、`MIT` 和 `MPL 2.0` 协议。
* `-f/--licensef` 指定自定义的 License 头文件。
* `--skip-dirs` 跳过指定的目录。
* `--skip-files` 跳过指定的文件。
* `-v/--verbose` 打印被更改的文件名。
* `-y/--year` 指定 License 的版权起始年份。

## 使用

准备实验的目录如下：

```bash
$ tree data -a
data
├── a
│   ├── main.go
│   └── main_test.go
├── b
│   └── c
│       └── keep
├── c
│   └── main.py
├── d.go
└── d_test.go

5 directories, 6 files
```

### 使用内置 License

```bash
$ addlicense --check data
data/a/main_test.go
data/d_test.go
data/d.go
data/c/main.py
data/a/main.go
```

输出了没有 License 头信息的文件。可以发现，这里自动跳过了没有后缀名的文件 `keep`。

为缺失 License 头信息的文件添加 License 头信息：

```bash
$ addlicense -v -l mit -c LiangNing7 --skip-dirs=c data
data/a/main_test.go added license
data/a/main.go added license
data/d.go added license
data/d_test.go added license
```

输出了所有本次命令增加了 License 头信息的文件。

`data/a/main.go` 效果如下：

```bash
// Copyright (c) 2025 LiangNing7
//
// Permission is hereby granted, free of charge, to any person obtaining a copy of
// this software and associated documentation files (the "Software"), to deal in
// the Software without restriction, including without limitation the rights to
// use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of
// the Software, and to permit persons to whom the Software is furnished to do so,
// subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS
// FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
// COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER
// IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN
// CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

package main

import "fmt"

...
```

### 指定自定义 License

我们也可以指定自定义的 License 文件 `boilerplate.txt` 内容如下：

```txt
Copyright 2025 LiangNing7 <liangning2277@gmail.com>. All rights reserved.
Use of this source code is governed by a MIT style
license that can be found in the LICENSE file.
```

为缺失 License 头信息的文件添加 License 头信息：

```bash
$ addlicense -v -f ./boilerplate.txt --skip-dirs=^a$ --skip-files=d.go,d_test.go data
data/c/main.py added license
```

`data/c/main.py` 效果如下：

```py
# Copyright 2025 LiangNing7 <liangning2277@gmail.com>. All rights reserved.
# Use of this source code is governed by a MIT style
# license that can be found in the LICENSE file.

def main():
    print("Hello Python")
...
```

## 源码解读

`addlicense` 项目很小，项目源文件如下：

```bash
$ tree addlicense                        
addlicense
├── Makefile
├── README.md
├── boilerplate.txt
├── go.mod
├── go.sum
└── main.go

1 directory, 6 files
```

`addlicense` 的代码逻辑，其实只有一个 `main.go` 文件，我们来对其代码进行逐行分析。

### 包声明、常量定义

首先就是正常的 Go 包声明和导入信息。

```go
package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
)
```

接下来是几个常量的定义：

```go
// helpText 定义帮助文档
const helpText = `Usage: addlicense [flags] pattern [pattern ...]

The program ensures source code files have copyright license headers
by scanning directory patterns recursively.

It modifies all source files in place and avoids adding a license header
to any file that already has one.

The pattern argument can be provided multiple times, and may also refer
to single files.

Flags:
`

// tmplApache 定义 Apache License 文本.
const tmplApache = `Copyright {{.Year}} {{.Holder}}

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.`

// tmplBSD 定义 BSD License 文本.
const tmplBSD = `Copyright (c) {{.Year}} {{.Holder}} All rights reserved.
Use of this source code is governed by a BSD-style
license that can be found in the LICENSE file.`

// tmplMIT 定义 MID License 文本.
const tmplMIT = `Copyright (c) {{.Year}} {{.Holder}}

Permission is hereby granted, free of charge, to any person obtaining a copy of
this software and associated documentation files (the "Software"), to deal in
the Software without restriction, including without limitation the rights to
use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of
the Software, and to permit persons to whom the Software is furnished to do so,
subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS
FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER
IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN
CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.`

// tmplMPL 定义 Mozilla Public License 文本.
const tmplMPL = `This Source Code Form is subject to the terms of the Mozilla Public
License, v. 2.0. If a copy of the MPL was not distributed with this
file, You can obtain one at https://mozilla.org/MPL/2.0/.`
```

常用 `helpText` 就是使用 `-h/--help` 打印帮助信息最上面的内容。

剩下的几个常量就是内置支持的 License 头信息了，分别是 `Apache`、`BSD`、`MIT`、`MPL` 协议。看到每个 License 头信息中的 `{ {.Year} } { {.Holder} }` 就知道，这是 Go template 的模板语法。

然后，我们能看到定义的所有命令行标志都在这里了：

```go
// 定义命令行标志.
var (
	holder    = pflag.StringP("holder", "c", "Google LLC", "copyright holder")                                                                // 持有者.
	license   = pflag.StringP("license", "l", "apache", "license type: apache, bsd, mit, mpl")                                                // 许可类型.
	licensef  = pflag.StringP("licensef", "f", "", "custom license file (no default)")                                                        // 自定义许可文件.
	year      = pflag.StringP("year", "y", fmt.Sprint(time.Now().Year()), "copyright year(s)")                                                // 年份.
	verbose   = pflag.BoolP("verbose", "v", false, "verbose mode: print the name of the files that are modified")                             // 详细模式.
	checkonly = pflag.BoolP("check", "", false, "check only mode: verify presence of license headers and exit with non-zero code if missing") // 检查模式.
	skipDirs  = pflag.StringSliceP("skip-dirs", "", nil, "regexps of directories to skip")                                                    // 跳过目录.
	skipFiles = pflag.StringSliceP("skip-files", "", nil, "regexps of files to skip")                                                         // 跳过文件.
	help      = pflag.BoolP("help", "h", false, "show this help message")                                                                     // 帮助.
)
```

可以发现 `--skip-dirs` 和 `--skip-files` 两个标志都是 `slice` 类型，传入格式为 `a,b,c`。

### 主逻辑 `main` 函数

```go
// nolint: gocognit // no lint
func main() {
	pflag.Usage = usage // 输出自定义的帮助信息.
	pflag.Parse()

	// 如果传入了 -h / --help 标志，
	// 则打印帮助信息，并直接退出程序.
	if *help {
		pflag.Usage()
		os.Exit(1)
	}

	// 处理完标志之后，剩余的参数个数，
	// 用来指定需要处理的目录.
	if pflag.NArg() == 0 {
		pflag.Usage()
		os.Exit(1)
	}

	if len(*skipDirs) != 0 {
		ps, err := getPatterns(*skipDirs)
		if err != nil {
			fmt.Println(err.Error())
			os.Exit(1)
		}
		patterns.dirs = ps // 存储跳过目录的正则.
	}

	if len(*skipFiles) != 0 {
		ps, err := getPatterns(*skipFiles)
		if err != nil {
			fmt.Println(err.Error())
			os.Exit(1)
		}
		patterns.files = ps // 存储跳过文件的正则.
	}

	// 准备填充模板的数据：年份和持有者.
	data := &copyrightData{
		Year:   *year,
		Holder: *holder,
	}

	var t *template.Template
	if *licensef != "" {
		d, err := os.ReadFile(*licensef)
		if err != nil {
			fmt.Printf("license file: %v\n", err)
			os.Exit(1)
		}
		t, err = template.New("").Parse(string(d))
		if err != nil {
			fmt.Printf("licnese file: %v\n", err)
			os.Exit(1)
		}
	} else {
		t = licenseTemplate[*license]
		if t == nil {
			fmt.Printf("unknown license: %s\n", *license)
			os.Exit(1)
		}
	}

	// process at most 1000 files in parallel.
	ch := make(chan *file, 1000)
	done := make(chan struct{})
	go func() {
		var wg errgroup.Group
		for f := range ch {
			f := f // https://golang.org/doc/faq#closures_and_goroutines.
			wg.Go(func() error {
				// nolint: nestif
				if *checkonly {
					lic, err := licenseHeader(f.path, t, data)
					if err != nil {
						fmt.Printf("%s: %v\n", f.path, err)

						return err
					}
					if lic == nil { // Unknown fileExtension.
						return nil
					}

					// Check if file has a license.
					isMissingLicenseHeader, err := fileHasLicense(f.path)
					if err != nil {
						fmt.Printf("%s: %v\n", f.path, err)

						return err
					}
					if isMissingLicenseHeader {
						fmt.Printf("%s\n", f.path)

						return errors.New("missing license header")
					}
				} else {
					modified, err := addLicense(f.path, f.mode, t, data)
					if err != nil {
						fmt.Printf("%s: %v\n", f.path, err)

						return err
					}
					if *verbose && modified {
						fmt.Printf("%s added license\n", f.path)
					}
				}
				return nil
			})
		}
		err := wg.Wait()
		close(done)
		if err != nil {
			os.Exit(1)
		}
	}()

	for _, d := range pflag.Args() {
		walk(ch, d)
	}
	close(ch)
	<-done
}
```

这段逻辑很长，进行拆解阅读。

映入眼帘的是对命令行标志的处理：

```go
pflag.Usage = usage // 输出自定义的帮助信息.
pflag.Parse()

// 如果传入了 -h / --help 标志，
// 则打印帮助信息，并直接退出程序.
if *help {
    pflag.Usage()
    os.Exit(1)
}

// 处理完标志之后，剩余的参数个数，
// 用来指定需要处理的目录.
if pflag.NArg() == 0 {
    pflag.Usage()
    os.Exit(1)
}
```

`pflag.Usage` 字段是一个函数，用来打印使用帮助信息，变量 `usage` 定义如下：

```go
var (
	...
	usage           = func() {
		fmt.Println(helpText)
		pflag.PrintDefaults()
	}
)
```

`if *help` 就是对 `-h/--help` 标志进行判断，如果用户输入了此标志，就打印帮助信息，并直接退出程序。

`pflag.NArg()` 返回处理完标志后剩余的参数个数，用来指定需要处理的目录。如果用户没传，同样打印帮助信息并退出。

如果执行

```bash
addlicense -v -l mit -c LiangNing7 a b c
```

命令，`pflag.NArg()` 会返回 `a`、`b`、`c` 三个目录。我们至少要传一个搜索路径，不然 `addlicense` 会不知道去找哪些文件。你可能想，这里也可以设置为默认查找当前目录，即默认目录为 `.`。但是我个人不推荐这种设计，因为 `addlicense` 会**修改文件**，最好还是用户明确传了哪个目录，再去操作。不然假如用户不小心在家目录下执行了这个命令，所有文件都被改了。显然，在这个场景中，**显式胜于隐式**。

接下来是对 `--skip-dirs` 和 `--skip-files` 两个命令行标志的处理：

```go
if len(*skipDirs) != 0 {
    ps, err := getPatterns(*skipDirs)
    if err != nil {
        fmt.Println(err.Error())
        os.Exit(1)
    }
    patterns.dirs = ps // 存储跳过目录的正则.
}

if len(*skipFiles) != 0 {
    ps, err := getPatterns(*skipFiles)
    if err != nil {
        fmt.Println(err.Error())
        os.Exit(1)
    }
    patterns.files = ps // 存储跳过文件的正则.
}
```

跳过的目录和文件都通过 `getPatterns` 函数来转换成正则表达式，并赋值给 `patterns` 对象。

`patterns` 和 `getPatterns` 定义如下：

```go
var patterns = struct {
	dirs  []*regexp.Regexp
	files []*regexp.Regexp
}{}

// getPatterns 将字符串列表编译为正则表达式切片.
func getPatterns(patterns []string) ([]*regexp.Regexp, error) {
	patternsRe := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		patternRe, err := regexp.Compile(p)
		if err != nil {
			fmt.Printf("can't compile regexp %q\n", p)

			return nil, fmt.Errorf("compile regexp failed, %w", err)
		}
		patternsRe = append(patternsRe, patternRe)
	}
	return patternsRe, nil
}
```

接着又构建了一个 `copyrightData` 对象：

```go
// 准备填充模板的数据：年份和持有者.
data := &copyrightData{
    Year:   *year,
    Holder: *holder,
}
```

其中 `year` 是通过 `-y--year` 传入的，`year`不传默认值就是当前年份，`holder` 是通过 `-c/--holder` 传入的。`data` 变量稍后将用于渲染模板。

而接下来就是构造模版逻辑：

```go
var t *template.Template
if *licensef != "" {
    d, err := os.ReadFile(*licensef)
    if err != nil {
        fmt.Printf("license file: %v\n", err)
        os.Exit(1)
    }
    t, err = template.New("").Parse(string(d))
    if err != nil {
        fmt.Printf("licnese file: %v\n", err)
        os.Exit(1)
    }
} else {
    t = licenseTemplate[*license]
    if t == nil {
        fmt.Printf("unknown license: %s\n", *license)
        os.Exit(1)
    }
}
```

`if *licensef != ""` 表示如果用户使用`-f/--licensef` 指定了自定义的 License 头文件，则进入此逻辑，读取其中内容作为模板。

否则，使用默认支持的版权内容作为模板。`licenseTemplate` 是一个全局变量，并在 `init`中被初始化：

```go
var (
	licenseTemplate = make(map[string]*template.Template)
	usage           = func() {
		fmt.Print(helpText)
		pflag.PrintDefaults()
	}
)

func init() {
	licenseTemplate["apache"] = template.Must(template.New("").Parse(tmplApache))
	licenseTemplate["mit"] = template.Must(template.New("").Parse(tmplMIT))
	licenseTemplate["bsd"] = template.Must(template.New("").Parse(tmplBSD))
	licenseTemplate["mpl"] = template.Must(template.New("").Parse(tmplMPL))
}
```

无论哪个分支，只要报错，就会调用 `os.Exit(1)` 退出。

接下来就是程序的核心逻辑了：

```go
// process at most 1000 files in parallel.
ch := make(chan *file, 1000)
done := make(chan struct{})
go func() {
    var wg errgroup.Group
    for f := range ch {
        f := f // https://golang.org/doc/faq#closures_and_goroutines.
        wg.Go(func() error {
            // nolint: nestif
            if *checkonly {
                lic, err := licenseHeader(f.path, t, data)
                if err != nil {
                    fmt.Printf("%s: %v\n", f.path, err)

                    return err
                }
                if lic == nil { // Unknown fileExtension.
                    return nil
                }

                // Check if file has a license.
                isMissingLicenseHeader, err := fileHasLicense(f.path)
                if err != nil {
                    fmt.Printf("%s: %v\n", f.path, err)

                    return err
                }
                if isMissingLicenseHeader {
                    fmt.Printf("%s\n", f.path)

                    return errors.New("missing license header")
                }
            } else {
                modified, err := addLicense(f.path, f.mode, t, data)
                if err != nil {
                    fmt.Printf("%s: %v\n", f.path, err)

                    return err
                }
                if *verbose && modified {
                    fmt.Printf("%s added license\n", f.path)
                }
            }
            return nil
        })
    }
    err := wg.Wait()
    close(done)
    if err != nil {
        os.Exit(1)
    }
}()

for _, d := range pflag.Args() {
    walk(ch, d)
}
close(ch)
<-done
```

虽然看着很多，但是理清思路后还是比较好理解的。

我们先来理清代码的整体思路：

```go
// process at most 1000 files in parallel.
ch := make(chan *file, 1000)
done := make(chan struct{})
go func() {
    var wg errgroup.Group
    for f := range ch {
        f := f // 规避闭包陷阱，但 go1.22版本之后就不需要了.
        wg.Go(func() error {
			...
            return nil
        })
    }
    err := wg.Wait()
    close(done)
    if err != nil {
        os.Exit(1)
    }
}()

for _, d := range pflag.Args() {
    walk(ch, d)
}
close(ch)
<-done
```

这段代码设计还是比较精妙的，主 `goroutine` 与子 `goroutine` 通过 `ch` 和 `done` 进行协作。这也是典型的生产者消费者模型。

`ch := make(chan *file, 1000)` 创建了一个带缓冲的通道，缓冲大小为 1000，即最大并发为 1000。它用于将遍历到的文件（通过 `walk` 函数找到的文件）发送到消费者 `goroutine` 中。

`done := make(chan struct{})` 创建了一个无缓冲的通道，用于通知主 `goroutine` 所有并发任务（检查或修改文件）已经完成。

生产者 `goroutine` 遍历 `pflag.Args()` 的返回值并调用 `walk(ch, d)` 来将生产的数据传入 `ch`。`pflag.Args()` 调用会返回处理完标志后剩余的参数列表，类型为 `[]string`，即传进来的目录或文件。前面提到的 `pflag.NArg()` 返回几个值，`pflag.Args()` 返回的切片中就有几个值。

当生产者中的 `walk` 函数完成遍历所有目录并发送所有文件后，主 `goroutine` 会调用 `close(ch)` 关闭 `ch` 通道，通知接收方没有更多的文件需要处理。然后调用 `<-done` 阻塞，等待消费者 `goroutine` 发送过来的完成信号。

消费者 `goroutine` 中，`for f := range ch { ... }` 循环从 `ch` 通道接收文件（`*file` 类型），并为每个文件启动一个新的 `goroutine`（通过 `errgroup` 的 `Go` 方法管理并发任务）。当 `ch` 通道被关闭，`for` 循环也就结束了。`wg.Wait()` 会等待所有消费 `goroutine` 处理完成并返回。然后调用 `close(done)` 关闭 `done` 通道。最后根据是否有 `goroutine` 返回 `error` 来决定是否调用 `os.Exit(1)` 进行异常退出。

当消费者 `goroutine` 关闭 `done` 通道时，生产者 `<-done` 会立即收到完成信号，由于这是 `main` 函数的最后一行代码，`<-done` 返回也就意味着整个程序执行完成并退出。

两个 `goroutine` 协同工作的主要逻辑已经解释清楚，我们就来分别看下二者的具体逻辑实现。

生产者 `goroutine` 主要逻辑在 `walk` 函数中：

```go
func walk(ch chan<- *file, start string) {
	_ = filepath.Walk(start, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			fmt.Printf("%s error: %v\n", path, err)

			return nil
		}
		if fi.IsDir() {
			for _, pattern := range patterns.dirs {
				if pattern.MatchString(fi.Name()) {
					return filepath.SkipDir
				}
			}

			return nil
		}

		for _, pattern := range patterns.files {
			if pattern.MatchString(fi.Name()) {
				return nil
			}
		}

		ch <- &file{path, fi.Mode()}

		return nil
	})
}
```

`walk`接收两个参数 `ch` 通道以及遍历的起始目录 `start`。

其中 `ch` 通道中的 `file` 类型定义如下：

```go
type file struct {
	path string
	mode os.FileMode
}
```

`path`表示文件路径，`mode` 表示文件操作模式。

`walk`函数内部使用 `filepath.Walk` 来从 `start` 开始递归的遍历目录，并对其进行处理。

这里处理逻辑也很简单，就是通过正则匹配，来过滤用户通过 `--skip-dirs` 和 `--skip-files` 两个标志传进来需要跳过的目录和文件。然后将需要处理的文件传递给 `ch` 通道等待消费者去处理。

当 `*file` 对象被传入 `ch` 通道，消费者就要开始工作了。

消费 `goroutine` 中主逻辑分两种情况：

1. 用户执行命令时输入了 `--check` 标志，只检查文件是否存在 License。

   ```go
   // nolint: nestif
   if *checkonly {
       lic, err := licenseHeader(f.path, t, data)
       if err != nil {
           fmt.Printf("%s: %v\n", f.path, err)
   
           return err
       }
       if lic == nil { // Unknown fileExtension.
           return nil
       }
   
       // Check if file has a license.
       isMissingLicenseHeader, err := fileHasLicense(f.path)
       if err != nil {
           fmt.Printf("%s: %v\n", f.path, err)
   
           return err
       }
       if isMissingLicenseHeader {
           fmt.Printf("%s\n", f.path)
   
           return errors.New("missing license header")
       }
   }
   ```

   首先调用 `licenseHeader` 函数来检查文件扩展名是否支持，它接收三个参数，分别是文件路径、License 模板、和 `data`。

   `licenseHeader` 函数实现如下：

   ```go
   func licenseHeader(path string, tmpl *template.Template, data *copyrightData) ([]byte, error) {
   	var lic []byte
   	var err error
   	switch fileExtension(path) {
   	default:
   		return nil, nil
   	case ".c", ".h":
   		lic, err = prefix(tmpl, data, "/*", " * ", " */")
   	case ".js", ".mjs", ".cjs", ".jsx", ".tsx", ".css", ".tf", ".ts":
   		lic, err = prefix(tmpl, data, "/**", " * ", " */")
   	case ".cc",
   		".cpp",
   		".cs",
   		".go",
   		".hh",
   		".hpp",
   		".java",
   		".m",
   		".mm",
   		".proto",
   		".rs",
   		".scala",
   		".swift",
   		".dart",
   		".groovy",
   		".kt",
   		".kts":
   		lic, err = prefix(tmpl, data, "", "// ", "")
   	case ".py", ".sh", ".yaml", ".yml", ".dockerfile", "dockerfile", ".rb", "gemfile":
   		lic, err = prefix(tmpl, data, "", "# ", "")
   	case ".el", ".lisp":
   		lic, err = prefix(tmpl, data, "", ";; ", "")
   	case ".erl":
   		lic, err = prefix(tmpl, data, "", "% ", "")
   	case ".hs", ".sql":
   		lic, err = prefix(tmpl, data, "", "-- ", "")
   	case ".html", ".xml", ".vue":
   		lic, err = prefix(tmpl, data, "<!--", " ", "-->")
   	case ".php":
   		lic, err = prefix(tmpl, data, "", "// ", "")
   	case ".ml", ".mli", ".mll", ".mly":
   		lic, err = prefix(tmpl, data, "(**", "   ", "*)")
   	}
   
   	return lic, err
   }
   ```

   里面逻辑看起来 `case` 比较多，不过主要是为了支持各种编程语言的文件。

   函数 `fileExtension` 用来获取文件扩展名：

   ```go
   func fileExtension(name string) string {
   	if v := filepath.Ext(name); v != "" {
   		return strings.ToLower(v)
   	}
   
   	return strings.ToLower(filepath.Base(name))
   }
   ```

   然后根据不同的文件扩展名调用 `prefix` 函数渲染模板。

   `prefix` 函数定义如下：

   ```go
   // prefix will execute a license template t with data d
   // and prefix the result with top, middle and bottom.
   func prefix(t *template.Template, d *copyrightData, top, mid, bot string) ([]byte, error) {
   	var buf bytes.Buffer
   	if err := t.Execute(&buf, d); err != nil {
   		return nil, fmt.Errorf("render template failed, err: %w", err)
   	}
   	var out bytes.Buffer
   	if top != "" {
   		fmt.Fprintln(&out, top)
   	}
   	s := bufio.NewScanner(&buf)
   	for s.Scan() {
   		fmt.Fprintln(&out, strings.TrimRightFunc(mid+s.Text(), unicode.IsSpace))
   	}
   	if bot != "" {
   		fmt.Fprintln(&out, bot)
   	}
   	fmt.Fprintln(&out)
   
   	return out.Bytes(), nil
   }
   ```

   `prefix` 函数会根据不同编程语言的注释风格生成版权声明头信息。它需要传入 License 模板、版权信息（年份、作者）、开头、中间、结尾标识符。

   所以我们调用 `lic, err := licenseHeader(f.path, t, data)`，最终得到的 `lic` 实际上内容根据文件类型是渲染后的 License 信息。

   比如同一个 License 头信息，在不同编程语言文件中都要写成对应的注释形式，所以要入乡随俗。

   接下来判断如果没拿到结果，说明是不支持的文件扩展名，直接返回不做进一步处理，逻辑如下：

   ```go
   if lic == nil { // Unknown fileExtension
       return nil
   }
   ```

   之后调用 `fileHasLicense`检查文件是否包含授权头信息。`fileHasLicense` 函数实现如下：

   ```go
   // fileHasLicense reports whether the file at path contains a license header.
   func fileHasLicense(path string) (bool, error) {
   	b, err := os.ReadFile(path)
   	if err != nil {
   		return false, err
   	}
   
   	if hasLicense(b) {
   		return false, nil
   	}
   
   	return true, nil
   }
   
   func hasLicense(b []byte) bool {
   	n := min(1000, len(b))
   	return bytes.Contains(bytes.ToLower(b[:n]), []byte("copyright")) ||
   		bytes.Contains(bytes.ToLower(b[:n]), []byte("mozilla public"))
   }
   ```

   这里实现比较简单，就是读取文件内容，然后判断前 1000 个字符中是否包含 `copyright` 或 `mozilla public` 关键字。

   `fileHasLicense` 函数返回后，如果其返回值为 `true`，则说明文件中不包含 License 头信息，直接返回一个 `error`：

   ```go
   if isMissingLicenseHeader {
       fmt.Printf("%s\n", f.path)
   
       return errors.New("missing license header")
   }
   ```

   这里返回的 `error` 会被 `err := wg.Wait()` 拿到，最终调用 `os.Exit(1)` 异常退出。

2. 需要添加 License 头信息的逻辑。

   ```go
   else {
       modified, err := addLicense(f.path, f.mode, t, data)
       if err != nil {
           fmt.Printf("%s: %v\n", f.path, err)
   
           return err
       }
       if *verbose && modified {
           fmt.Printf("%s added license\n", f.path)
       }
   }
   ```

   这里调用 `addLicense` 函数为指定文件插入 License 头信息。

   ```go
   func addLicense(path string, fmode os.FileMode, tmpl *template.Template, data *copyrightData) (bool, error) {
   	var lic []byte
   	var err error
   	lic, err = licenseHeader(path, tmpl, data)
   	if err != nil || lic == nil {
   		return false, err
   	}
   
   	b, err := os.ReadFile(path)
   	if err != nil {
   		return false, err
   	}
   	if hasLicense(b) {
   		return false, nil
   	}
   
   	line := hashBang(b)
   	if len(line) > 0 {
   		b = b[len(line):]
   		if line[len(line)-1] != '\n' {
   			line = append(line, '\n')
   		}
   		line = append(line, '\n')
   		lic = append(line, lic...)
   	}
   	b = append(lic, b...)
   
   	return true, os.WriteFile(path, b, fmode)
   }
   ```

   首先这里也调用了 `licenseHeader` 来判断文件扩展名是否被支持，并渲染 License 模板。

   然后调用 `hasLicense` 来判断文件是否已经存在 License 头信息。

   如果文件不存在 License 头信息，接下来的逻辑就是正式准备写入 License 头信息了。

   接下来这段逻辑分两种情况，首先调用 `hashBang` 函数用来判断文件是否存在 [Shebang](https://zh.wikipedia.org/wiki/Shebang) 行，如果有 `Shebang` 行，则源文件内容为 `Shebang` 行 + 代码，新内容为 `Shebang` 行 + License 头信息 + 代码。如果没有 `Shebang` 行存在，则源文件内容只包含代码，新内容为 License 头信息 + 代码。

   `hashBang` 函数内容如下：

   ```go
   var head = []string{
   	"#!",                       // shell script
   	"<?xml",                    // XML declaratioon
   	"<!doctype",                // HTML doctype
   	"# encoding:",              // Ruby encoding
   	"# frozen_string_literal:", // Ruby interpreter instruction
   	"<?php",                    // PHP opening tag
   }
   
   func hashBang(b []byte) []byte {
   	line := make([]byte, 0, len(b))
   	for _, c := range b {
   		line = append(line, c)
   		if c == '\n' {
   			break
   		}
   	}
   	first := strings.ToLower(string(line))
   	for _, h := range head {
   		if strings.HasPrefix(first, h) {
   			return line
   		}
   	}
   
   	return nil
   }
   ```

   最后这段逻辑就简单了：

   ```go
   if *verbose && modified {
       fmt.Printf("%s added license\n", f.path)
   }
   ```

   这里用来处理 `-v/--verbose` 参数。

至此，`addlicense` 所有源码就都解读完成了。


