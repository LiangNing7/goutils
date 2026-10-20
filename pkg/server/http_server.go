package server

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"time"

	"github.com/LiangNing7/goutils/pkg/log"
	genericoptions "github.com/LiangNing7/goutils/pkg/options"
)

// HTTPServer 代表一个 HTTP 服务器.
type HTTPServer struct {
	srv *http.Server
}

// NewHTTPServer 创建一个新的 HTTP 服务器实例.
func NewHTTPServer(httpOptions *genericoptions.HTTPOptions, tlsOptions *genericoptions.TLSOptions, handler http.Handler) *HTTPServer {
	var tlsConfig *tls.Config
	if tlsOptions != nil && tlsOptions.UseTLS {
		tlsConfig = tlsOptions.MustTLSConfig()
	}

	return &HTTPServer{
		srv: &http.Server{
			Addr:      httpOptions.Addr,
			Handler:   handler,
			TLSConfig: tlsConfig,
			// 补齐超时，避免连接被长时间占用（Slowloris 类攻击）.
			ReadHeaderTimeout: httpTimeout(httpOptions),
			ReadTimeout:       httpTimeout(httpOptions),
			WriteTimeout:      httpTimeout(httpOptions),
			IdleTimeout:       2 * httpTimeout(httpOptions),
		},
	}
}

// httpTimeout 返回 HTTP 服务器使用的超时时间，未配置时退化为 30s.
func httpTimeout(httpOptions *genericoptions.HTTPOptions) time.Duration {
	if httpOptions != nil && httpOptions.Timeout > 0 {
		return httpOptions.Timeout
	}

	return 30 * time.Second
}

// RunOrDie 启动 HTTP 服务器并在出错时记录致命错误.
func (s *HTTPServer) RunOrDie() {
	log.Infow("Start to listening the incoming requests", "protocol", protocolName(s.srv), "addr", s.srv.Addr)
	// 默认启动 HTTP 服务器
	serveFn := func() error { return s.srv.ListenAndServe() }
	if s.srv.TLSConfig != nil {
		serveFn = func() error { return s.srv.ListenAndServeTLS("", "") }
	}

	if err := serveFn(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalw("Failed to server HTTP(s) server", "err", err)
	}
}

// GracefulStop 优雅地关闭 HTTP 服务器.
// Shutdown 超时后会强制 Close，保证进程能够退出.
func (s *HTTPServer) GracefulStop(ctx context.Context) {
	log.Infow("Gracefully stop HTTP(s) server")
	if err := s.srv.Shutdown(ctx); err != nil {
		log.Errorw(err, "HTTP(s) server forced to shutdown")
		// 超时后仍有活跃连接时，Shutdown 不会关闭它们，必须 Close 兜底.
		if closeErr := s.srv.Close(); closeErr != nil {
			log.Errorw(closeErr, "Failed to close HTTP(s) server")
		}
	}
}
