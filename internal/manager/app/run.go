package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/shared/rpcsrv"
)

// Run は各サーバーを起動し、ctx が終わるか致命的な失敗が起きるまで動かし続ける。
// 戻る前に docs/16-grpc.md §6.3 の順序(HTTP → ジョブワーカー → gRPC → Mongo)で停止する。
func (a *App) Run(ctx context.Context) error {
	// 起動時点では Provider 未接続とみなす。到達できたら監視タスクが online へ更新する。
	a.health.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	fatal := make(chan error, 1)

	providerCtx, stopProvider := context.WithCancel(ctx)
	defer stopProvider()

	workerCtx, stopWorker := context.WithCancel(ctx)
	defer stopWorker()

	var (
		servers sync.WaitGroup
		worker  sync.WaitGroup
	)

	servers.Go(func() {
		a.logger.InfoContext(ctx, "grpc listening", "addr", a.GRPCAddr())

		if err := a.grpcSrv.Serve(a.grpcLis); !isClosed(err) {
			report(fatal, fmt.Errorf("serve grpc: %w", err))
		}
	})

	servers.Go(func() {
		a.logger.InfoContext(ctx, "http listening", "addr", a.HTTPAddr())

		if err := a.httpSrv.Serve(a.httpLis); !isClosed(err) {
			report(fatal, fmt.Errorf("serve http: %w", err))
		}
	})

	worker.Go(func() { a.worker.Run(workerCtx) })

	// Provider への疎通は起動をブロックしない。未接続でも API は応答し、状態は offline のままになる。
	servers.Go(func() {
		if err := a.watchProvider(providerCtx); err != nil {
			report(fatal, err)
		}
	})

	var runErr error

	select {
	case <-ctx.Done():
	case runErr = <-fatal:
		a.logger.ErrorContext(ctx, "manager stopping after fatal error", "error", runErr)
	}

	a.shutdown(context.WithoutCancel(ctx), stopWorker, &worker, stopProvider, &servers)

	return runErr
}

// shutdown は docs/16 §6.3 の順序で止める。ジョブを止め切ってから gRPC と Mongo を閉じることで、
// 実行中のジョブが Provider 呼び出しや DB 更新を終えられるようにする。
func (a *App) shutdown(
	ctx context.Context,
	stopWorker context.CancelFunc,
	worker *sync.WaitGroup,
	stopProvider context.CancelFunc,
	servers *sync.WaitGroup,
) {
	ctx, cancel := context.WithTimeout(ctx, shutdownTimeout)
	defer cancel()

	a.health.Shutdown()

	if err := a.httpSrv.Shutdown(ctx); err != nil {
		a.logger.ErrorContext(ctx, "shutdown http failed", "error", err)
	}

	stopWorker()
	waitOrTimeout(ctx, a, worker, "job worker")

	a.stopGRPC(ctx)

	stopProvider()
	servers.Wait()

	a.Close(ctx)
}

// stopGRPC は処理中の RPC を待ってから閉じ、猶予を超えたら強制的に切る(docs/16 §5.3)。
func (a *App) stopGRPC(ctx context.Context) {
	stopped := make(chan struct{})

	go func() {
		defer close(stopped)

		a.grpcSrv.GracefulStop()
	}()

	select {
	case <-stopped:
	case <-time.After(rpcsrv.GracePeriod):
		a.logger.WarnContext(ctx, "grpc graceful stop timed out")
		a.grpcSrv.Stop()
		<-stopped
	}
}

func waitOrTimeout(ctx context.Context, a *App, wg *sync.WaitGroup, what string) {
	done := make(chan struct{})

	go func() {
		defer close(done)

		wg.Wait()
	}()

	select {
	case <-done:
	case <-ctx.Done():
		a.logger.WarnContext(ctx, "shutdown timed out", "component", what)
	}
}

// watchProvider は Provider へ疎通してから種別を照合し、ワークスペースを同期して監視を続ける。
// 種別不一致はデータを壊すため、起動を続けずに Run を終わらせる(docs/16 §6.1)。
func (a *App) watchProvider(ctx context.Context) error {
	if err := a.monitor.Bootstrap(ctx); err != nil {
		if errors.Is(err, domain.ErrProviderKindMismatch) {
			return err
		}

		// 停止要求による中断と疎通の断念は、どちらも Manager を落とす理由にならない。
		a.logger.ErrorContext(ctx, "provider bootstrap stopped", "error", err)

		return nil
	}

	if _, err := a.workspaces.SyncFromProvider(ctx); err != nil {
		// 同期に失敗しても Manager は動ける。次の ReportWorkspaces で埋め合わせられる。
		a.logger.WarnContext(ctx, "sync workspaces failed", "error", err)
	}

	return a.monitor.Run(ctx)
}

func report(ch chan<- error, err error) {
	select {
	case ch <- err:
	default:
	}
}
