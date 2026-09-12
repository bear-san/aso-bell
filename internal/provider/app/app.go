// Package app は Provider の組み立てと起動・停止順序を担う(docs/02-architecture.md §3.5)。
// Adapter の選択は各バイナリ(cmd/provider-*)が行い、ここから先は種別に依存しない。
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"google.golang.org/grpc"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/provider/adapter"
	"github.com/bear-san/aso-bell/internal/provider/config"
	provruntime "github.com/bear-san/aso-bell/internal/provider/runtime"
	"github.com/bear-san/aso-bell/internal/shared/rpcauth"
	"github.com/bear-san/aso-bell/internal/shared/rpcsrv"
)

// shutdownTimeout は停止時に受信中の処理を待つ上限(docs/16-grpc.md §6.3)。
const shutdownTimeout = 10 * time.Second

// App は組み立て済みの Provider。
type App struct {
	cfg     *config.Common
	logger  *slog.Logger
	conn    *grpc.ClientConn
	runtime *provruntime.Runtime
	server  *grpc.Server
	lis     net.Listener
}

// New は設定と Adapter から Provider を組み立てる。Manager へは接続を試みない。
// どちらが先に起動しても動くよう、疎通は Run の中で再試行する(docs/16 §6.1)。
func New(
	ctx context.Context,
	cfg *config.Common,
	chat adapter.Adapter,
	logger *slog.Logger,
	version string,
) (*App, error) {
	if logger == nil {
		logger = slog.Default()
	}

	conn, err := provruntime.Dial(provruntime.DialConfig{
		Address: cfg.ManagerAddr,
		Token:   cfg.RPCToken,
		TLS:     cfg.RPCTLS,
	})
	if err != nil {
		return nil, err
	}

	rt := provruntime.New(provruntime.Config{
		Adapter: chat,
		Manager: asobellv1.NewManagerServiceClient(conn),
		Logger:  logger,
		Version: version,
		Timeout: cfg.ManagerTimeout,
	})

	server, err := newServer(cfg, logger)
	if err != nil {
		_ = conn.Close()

		return nil, err
	}

	rt.Register(server)

	var lc net.ListenConfig

	lis, err := lc.Listen(ctx, "tcp", cfg.ListenAddr)
	if err != nil {
		_ = conn.Close()

		return nil, fmt.Errorf("listen %s: %w", cfg.ListenAddr, err)
	}

	return &App{cfg: cfg, logger: logger, conn: conn, runtime: rt, server: server, lis: lis}, nil
}

func newServer(cfg *config.Common, logger *slog.Logger) (*grpc.Server, error) {
	creds, err := cfg.RPCTLS.TransportCredentials("")
	if err != nil {
		return nil, fmt.Errorf("rpc transport credentials: %w", err)
	}

	return grpc.NewServer(
		grpc.Creds(creds),
		grpc.KeepaliveEnforcementPolicy(rpcsrv.EnforcementPolicy()),
		grpc.ChainUnaryInterceptor(
			rpcsrv.UnaryRecovery(logger),
			rpcauth.UnaryServerInterceptor(cfg.RPCToken, rpcauth.SkipInfra),
			rpcsrv.UnaryLogging(logger),
		),
		grpc.ChainStreamInterceptor(
			rpcsrv.StreamRecovery(logger),
			rpcauth.StreamServerInterceptor(cfg.RPCToken, rpcauth.SkipInfra),
		),
	), nil
}

// Addr は実際に待ち受けているアドレスを返す。ポート 0 を指定したテスト向け。
func (a *App) Addr() string { return a.lis.Addr().String() }

// Run は gRPC サーバーを起動し、Manager との疎通が取れたらチャットツールへ接続する。
// ctx が終わるか致命的な失敗が起きるまで動き続ける。
func (a *App) Run(ctx context.Context) error {
	serveErr := make(chan error, 1)

	go func() {
		a.logger.InfoContext(ctx, "provider listening", "addr", a.Addr())

		if err := a.server.Serve(a.lis); err != nil && !isClosed(err) {
			serveErr <- fmt.Errorf("serve grpc: %w", err)
		}

		close(serveErr)
	}()

	startErr := make(chan error, 1)

	startCtx, stopStart := context.WithCancel(ctx)
	defer stopStart()

	// 成功したら何も送らない。Start が戻るのは接続が完了したときで、そこから常駐に入る。
	go func() {
		if err := a.runtime.Start(startCtx); err != nil && startCtx.Err() == nil {
			startErr <- err
		}
	}()

	var runErr error

	select {
	case <-ctx.Done():
	case err := <-serveErr:
		runErr = err
	case runErr = <-startErr:
	}

	a.shutdown(context.WithoutCancel(ctx), stopStart)

	return runErr
}

// shutdown はチャット接続を閉じてから gRPC を止める(docs/16 §6.3)。
// 逆順にすると、閉じている最中のインバウンドを Manager へ転送できない。
func (a *App) shutdown(ctx context.Context, stopStart context.CancelFunc) {
	stopStart()

	if err := a.runtime.Close(ctx); err != nil {
		a.logger.ErrorContext(ctx, "close adapter failed", "error", err)
	}

	a.runtime.Shutdown()
	a.stopServer(ctx)

	if err := a.conn.Close(); err != nil {
		a.logger.ErrorContext(ctx, "close manager connection failed", "error", err)
	}
}

// stopServer は処理中の RPC を待ってから閉じ、猶予を超えたら強制的に切る。
func (a *App) stopServer(ctx context.Context) {
	stopped := make(chan struct{})

	go func() {
		defer close(stopped)

		a.server.GracefulStop()
	}()

	select {
	case <-stopped:
	case <-time.After(shutdownTimeout):
		a.logger.WarnContext(ctx, "grpc graceful stop timed out")
		a.server.Stop()
		<-stopped
	}
}

func isClosed(err error) bool {
	return errors.Is(err, grpc.ErrServerStopped) || errors.Is(err, net.ErrClosed) ||
		errors.Is(err, http.ErrServerClosed)
}
