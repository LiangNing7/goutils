package server

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"

	genericoptions "github.com/LiangNing7/goutils/pkg/options"
)

type hangService interface{ Hang() }

type hangImpl struct {
	started chan struct{}
	release chan struct{}
}

func (*hangImpl) Hang() {}

func (h *hangImpl) handler(_ any, _ grpc.ServerStream) error {
	close(h.started)
	<-h.release
	return nil
}

// TestGRPCServerGracefulStopDoesNotHang 回归：存在活跃流式 RPC 时 GracefulStop 不能永久挂起.
func TestGRPCServerGracefulStopDoesNotHang(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	impl := &hangImpl{started: make(chan struct{}), release: make(chan struct{})}
	srv := grpc.NewServer()
	srv.RegisterService(&grpc.ServiceDesc{
		ServiceName: "test.Hang",
		HandlerType: (*hangService)(nil),
		Streams: []grpc.StreamDesc{{
			StreamName:    "Hang",
			Handler:       impl.handler,
			ServerStreams: true,
			ClientStreams: true,
		}},
	}, impl)

	g := &GRPCServer{srv: srv, lis: lis}
	go func() { _ = srv.Serve(lis) }()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	stream, err := conn.NewStream(context.Background(),
		&grpc.StreamDesc{ServerStreams: true, ClientStreams: true}, "/test.Hang/Hang")
	if err != nil {
		t.Fatalf("new stream: %v", err)
	}

	select {
	case <-impl.started:
	case <-time.After(5 * time.Second):
		t.Fatal("stream handler never started")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		g.GracefulStop(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		close(impl.release)
		t.Fatal("GracefulStop hung while a streaming RPC was active")
	}

	_ = stream.CloseSend()
	close(impl.release)
}

// TestHealthServiceNameIsConfigurable 回归：健康检查服务名不再硬编码为 "MiniBlog".
func TestHealthServiceNameIsConfigurable(t *testing.T) {
	old := DefaultGRPCHealthServiceName
	t.Cleanup(func() { DefaultGRPCHealthServiceName = old })

	DefaultGRPCHealthServiceName = "my-application"

	hs := newHealthServer()
	resp, err := hs.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{Service: "my-application"})
	if err != nil {
		t.Fatalf("health check: %v", err)
	}
	if resp.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("serving status = %v, want SERVING", resp.GetStatus())
	}
}

// TestHTTPServersSetTimeouts 回归：HTTP server 必须补齐超时，避免 Slowloris.
func TestHTTPServersSetTimeouts(t *testing.T) {
	opts := genericoptions.NewHTTPOptions()
	opts.Timeout = 7 * time.Second

	s := NewHTTPServer(opts, nil, http.NewServeMux())
	assertTimeouts(t, "http", s.srv)

	gw, err := NewGRPCGatewayServer(
		opts,
		genericoptions.NewGRPCOptions(),
		nil,
		func(*runtime.ServeMux, *grpc.ClientConn) error { return nil },
	)
	if err != nil {
		t.Fatalf("new gateway: %v", err)
	}
	assertTimeouts(t, "gateway", gw.srv)
}

func assertTimeouts(t *testing.T, name string, srv *http.Server) {
	t.Helper()

	if srv.ReadHeaderTimeout != 7*time.Second {
		t.Errorf("%s: ReadHeaderTimeout = %v, want 7s", name, srv.ReadHeaderTimeout)
	}
	if srv.ReadTimeout != 7*time.Second {
		t.Errorf("%s: ReadTimeout = %v, want 7s", name, srv.ReadTimeout)
	}
	if srv.WriteTimeout != 7*time.Second {
		t.Errorf("%s: WriteTimeout = %v, want 7s", name, srv.WriteTimeout)
	}
	if srv.IdleTimeout == 0 {
		t.Errorf("%s: IdleTimeout is not set", name)
	}
}

// TestHTTPServerGracefulStopForcesCloseAfterTimeout 回归：Shutdown 超时后必须强制 Close.
func TestHTTPServerGracefulStopForcesCloseAfterTimeout(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
	})

	s := NewHTTPServer(&genericoptions.HTTPOptions{Addr: "127.0.0.1:0", Timeout: 30 * time.Second}, nil, handler)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s.srv.Addr = lis.Addr().String()
	go func() { _ = s.srv.Serve(lis) }()

	reqErr := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + lis.Addr().String() + "/")
		if resp != nil {
			_ = resp.Body.Close()
		}
		reqErr <- err
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("handler never started")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		s.GracefulStop(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("GracefulStop hung after shutdown timeout")
	}

	select {
	case err := <-reqErr:
		if err == nil {
			t.Error("expected the in-flight request to be aborted after forced close")
		}
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("in-flight request was not aborted by forced close")
	}

	close(release)
}

type panicServer struct{}

func (panicServer) RunOrDie()                    { panic("boom") }
func (panicServer) GracefulStop(context.Context) {}

// TestServeRecoversFromRunPanic 回归：RunOrDie 所在协程 panic 不应直接崩溃进程.
func TestServeRecoversFromRunPanic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- Serve(ctx, panicServer{}) }()

	// 给 panic 一点时间发生.
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return")
	}
}
