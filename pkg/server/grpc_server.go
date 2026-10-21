package server

import (
	"context"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/LiangNing7/goutils/pkg/log"
	genericoptions "github.com/LiangNing7/goutils/pkg/options"
)

// GRPCServer 代表一个 GRPC 服务器.
type GRPCServer struct {
	srv *grpc.Server
	lis net.Listener
}

// NewGRPCServer 创建一个新的 GRPC 服务器实例.
func NewGRPCServer(
	grpcOptions *genericoptions.GRPCOptions,
	tlsOptions *genericoptions.TLSOptions,
	serverOptions []grpc.ServerOption,
	registerServer func(grpc.ServiceRegistrar),
) (*GRPCServer, error) {
	lis, err := net.Listen("tcp", grpcOptions.Addr)
	if err != nil {
		log.Errorw(err, "Failed to listen")
		return nil, err
	}

	if tlsOptions != nil && tlsOptions.UseTLS {
		tlsConfig := tlsOptions.MustTLSConfig()
		serverOptions = append(serverOptions, grpc.Creds(credentials.NewTLS(tlsConfig)))
	}

	grpcsrv := grpc.NewServer(serverOptions...)

	registerServer(grpcsrv)
	registerHealthServer(grpcsrv)
	reflection.Register(grpcsrv)

	return &GRPCServer{
		srv: grpcsrv,
		lis: lis,
	}, nil
}

// RunOrDie 启动 GRPC 服务器并在出错时记录致命错误.
func (s *GRPCServer) RunOrDie() {
	log.Infow("Start to listening the incoming requests", "protocol", "grpc", "addr", s.lis.Addr().String())
	if err := s.srv.Serve(s.lis); err != nil {
		log.Fatalw("Failed to serve grpc server", "err", err)
	}
}

// DefaultGRPCHealthServiceName 是 gRPC 健康检查上报的服务名.
// 应用可在创建服务器之前覆盖它，从而不再硬编码某个具体应用名.
var DefaultGRPCHealthServiceName = "MiniBlog"

// GracefulStop 优雅地关闭 GRPC 服务器.
// 有活跃的流式 RPC 时 GracefulStop 会一直等待，因此这里在 ctx 超时后强制 Stop，
// 避免进程永久挂起.
func (s *GRPCServer) GracefulStop(ctx context.Context) {
	log.Infow("Gracefully stop grpc server")

	stopped := make(chan struct{})
	go func() {
		s.srv.GracefulStop()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-ctx.Done():
		log.Warnw("Graceful stop timed out, forcing grpc server to stop", "err", ctx.Err())
		// Stop 与 GracefulStop 共享内部锁；GracefulStop 等待不响应取消的
		// handler 时也可能阻塞 Stop。因此异步触发强制停止，避免再次超出
		// ctx 的截止时间；底层停止流程在 handler 退出后自行完成。
		go s.srv.Stop()
	}
}

// newHealthServer 创建并配置健康检查服务.
func newHealthServer() *health.Server {
	healthServer := health.NewServer()

	// 设定服务的健康状态.
	healthServer.SetServingStatus(DefaultGRPCHealthServiceName, grpc_health_v1.HealthCheckResponse_SERVING)

	return healthServer
}

// registerHealthServer 注册健康检查服务.
func registerHealthServer(grpcsrv *grpc.Server) {
	// 注册健康检查服务
	grpc_health_v1.RegisterHealthServer(grpcsrv, newHealthServer())
}
