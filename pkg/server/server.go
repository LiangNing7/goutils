package server

import (
	"context"
	"fmt"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/LiangNing7/goutils/pkg/log"
)

// Server 定义所有服务器类型的接口.
type Server interface {
	// RunOrDie 运行服务器，如果运行失败会退出程序（OrDie的含义所在）.
	RunOrDie()
	// GracefulStop 方法用来优雅关停服务器。关停服务器时需要处理 context 的超时时间.
	GracefulStop(ctx context.Context)
}

// Serve starts the server and blocks until the context is canceled.
// It ensures the server is gracefully shut down when the context is done.
func Serve(ctx context.Context, srv Server) error {
	go func() {
		// RunOrDie 内部的 panic 会直接崩溃进程，这里兜底记录.
		defer func() {
			if r := recover(); r != nil {
				log.Errorw(fmt.Errorf("panic: %v", r), "Recovered from panic in server",
					"stack", string(debug.Stack()))
			}
		}()

		srv.RunOrDie()
	}()

	// Block until the context is canceled or terminated.
	<-ctx.Done()

	// Shutdown the server gracefully.
	log.Infow("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Gracefully stop the server.
	srv.GracefulStop(ctx)

	log.Infow("Server exited successfully.")

	// 文件 sink 可能带缓冲，退出前刷新，避免丢日志.
	log.Sync()

	return nil
}

// protocolName 从 http.Server 中获取协议名.
func protocolName(server *http.Server) string {
	if server.TLSConfig != nil {
		return "https"
	}
	return "http"
}
