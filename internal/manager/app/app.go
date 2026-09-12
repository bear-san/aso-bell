// Package app は Manager の組み立てと起動・停止の順序を担う(docs/02-architecture.md §3.5)。
// 起動順序と停止順序は docs/16-grpc.md §6 に従う。
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/manager/bot"
	"github.com/bear-san/aso-bell/internal/manager/config"
	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/manager/providerclient"
	"github.com/bear-san/aso-bell/internal/manager/rpc"
	"github.com/bear-san/aso-bell/internal/manager/scheduler"
	mongostore "github.com/bear-san/aso-bell/internal/manager/store/mongo"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
	"github.com/bear-san/aso-bell/internal/shared/rpcauth"
	"github.com/bear-san/aso-bell/internal/shared/rpcsrv"
)

const (
	// mongoTimeout は起動時の疎通確認と停止時の切断に与える時間。
	mongoTimeout = 10 * time.Second
	// shutdownTimeout は HTTP とジョブワーカーの後片付けを待つ上限。
	// 実行中のジョブは停止要求後も最後まで進むため(docs/16 §6.3)、諦める時間をここで決める。
	shutdownTimeout = 30 * time.Second
	// httpHeaderTimeout はヘッダ読み取りの上限(Slowloris 対策)。
	httpHeaderTimeout = 10 * time.Second
	// readyTimeout は /readyz の Mongo ping に与える時間。
	readyTimeout = 3 * time.Second
)

// App は組み立て済みの Manager。New で作り、Run で起動し、Close で解放する。
type App struct {
	cfg    *config.Config
	logger *slog.Logger

	client  *mongo.Client
	conn    *grpc.ClientConn
	monitor *providerclient.Monitor
	// workspaces は起動直後の ListConnectedWorkspaces による同期に使う。
	workspaces *usecase.WorkspaceService
	worker     *scheduler.Worker
	grpcSrv    *grpc.Server
	grpcLis    net.Listener
	httpSrv    *http.Server
	httpLis    net.Listener
	health     *health.Server
}

// New は設定から Manager を組み立てる。Mongo へは接続するが、Provider へは接続を試みない。
// Provider 未起動でも Manager は起動できる必要があるため(docs/16 §6.1)。
func New(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*App, error) {
	if logger == nil {
		logger = slog.Default()
	}

	client, store, err := connectMongo(ctx, cfg)
	if err != nil {
		return nil, err
	}

	// インデックスが無いと dedupeKey の一意性が効かず、ジョブが二重登録されうる。serve でも必ず作る。
	if err = migrateStore(ctx, store, logger); err != nil {
		disconnect(context.WithoutCancel(ctx), client, logger)

		return nil, err
	}

	app, err := build(cfg, logger, client, store)
	if err != nil {
		disconnect(context.WithoutCancel(ctx), client, logger)

		return nil, err
	}

	// 待ち受けは組み立て時に開始する。ポートが使えないことは起動前に分かったほうがよい。
	if err = app.listen(ctx); err != nil {
		app.Close(context.WithoutCancel(ctx))

		return nil, err
	}

	return app, nil
}

func (a *App) listen(ctx context.Context) error {
	var lc net.ListenConfig

	grpcLis, err := lc.Listen(ctx, "tcp", a.cfg.RPCAddr)
	if err != nil {
		return fmt.Errorf("listen rpc %s: %w", a.cfg.RPCAddr, err)
	}

	httpLis, err := lc.Listen(ctx, "tcp", a.cfg.HTTPAddr)
	if err != nil {
		_ = grpcLis.Close()

		return fmt.Errorf("listen http %s: %w", a.cfg.HTTPAddr, err)
	}

	a.grpcLis = grpcLis
	a.httpLis = httpLis

	return nil
}

func build(cfg *config.Config, logger *slog.Logger, client *mongo.Client, store *mongostore.Store) (*App, error) {
	conn, err := providerclient.Dial(providerclient.DialConfig{
		Address: cfg.ProviderAddr,
		Token:   cfg.RPCToken,
		TLS:     cfg.RPCTLS,
	})
	if err != nil {
		return nil, fmt.Errorf("dial provider: %w", err)
	}

	clock := usecase.SystemClock{}
	provider := providerclient.New(conn, providerclient.WithTimeout(cfg.ProviderTimeout))
	monitor := providerclient.NewMonitor(providerclient.MonitorConfig{
		Port:                 provider,
		Meta:                 store.Meta(),
		Clock:                clock,
		Logger:               logger,
		Address:              cfg.ProviderAddr,
		Interval:             cfg.ProviderPollInterval,
		OfflineAfterFailures: cfg.ProviderOfflineAfterFailures,
	})

	deps := usecase.Deps{
		Repo:     store,
		Provider: provider,
		Status:   monitor,
		Messages: bot.Renderer{},
		Clock:    clock,
		Logger:   logger,
	}

	events := usecase.NewEventService(deps)
	participation := usecase.NewParticipationService(deps)
	workspaces := usecase.NewWorkspaceService(deps)

	grpcSrv, err := newGRPCServer(cfg, logger, bot.NewDispatcher(bot.Services{
		Events:        events,
		Participation: participation,
		Workspaces:    workspaces,
		Identities:    usecase.NewIdentityService(deps),
		Provider:      provider,
		Clock:         clock,
		Logger:        logger,
		ConsoleURL:    cfg.BaseURL,
	}), workspaces, monitor)
	if err != nil {
		_ = conn.Close()

		return nil, err
	}

	healthSrv := health.NewServer()
	healthpb.RegisterHealthServer(grpcSrv, healthSrv)

	handlers := scheduler.NewHandlers(scheduler.Services{
		Events:        events,
		Participation: participation,
		Reminders:     usecase.NewReminderService(deps),
		Clock:         clock,
		Jobs:          store.Jobs(),
	})

	return &App{
		cfg:        cfg,
		logger:     logger,
		client:     client,
		conn:       conn,
		monitor:    monitor,
		workspaces: workspaces,
		worker: scheduler.NewWorker(store.Jobs(), handlers, clock, logger, scheduler.Config{
			PollInterval: cfg.JobsPollInterval,
			BatchSize:    cfg.JobsBatchSize,
			Lease:        cfg.JobsLease,
			Workers:      cfg.JobsWorkers,
		}),
		grpcSrv: grpcSrv,
		httpSrv: newHTTPServer(cfg, client),
		health:  healthSrv,
	}, nil
}

func newGRPCServer(
	cfg *config.Config,
	logger *slog.Logger,
	dispatcher *bot.Dispatcher,
	workspaces *usecase.WorkspaceService,
	status usecase.ProviderStatusPort,
) (*grpc.Server, error) {
	creds, err := cfg.RPCTLS.TransportCredentials("")
	if err != nil {
		return nil, fmt.Errorf("rpc transport credentials: %w", err)
	}

	srv := grpc.NewServer(
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
	)

	asobellv1.RegisterManagerServiceServer(srv, rpc.NewManagerService(dispatcher, workspaces, status, logger))

	return srv, nil
}

// newHTTPServer は現時点ではヘルスチェックだけを提供する(docs/08-api.md §4)。
// grpc-gateway と WebConsole の配信は M6 で同じ mux に載せる(docs/02 §3.3)。
func newHTTPServer(cfg *config.Config, client *mongo.Client) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writePlain(w, http.StatusOK, "ok")
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
		defer cancel()

		if err := client.Ping(ctx, readpref.Primary()); err != nil {
			writePlain(w, http.StatusServiceUnavailable, "mongo unavailable")

			return
		}

		writePlain(w, http.StatusOK, "ready")
	})

	return &http.Server{Addr: cfg.HTTPAddr, Handler: mux, ReadHeaderTimeout: httpHeaderTimeout}
}

func writePlain(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body + "\n"))
}

func connectMongo(ctx context.Context, cfg *config.Config) (*mongo.Client, *mongostore.Store, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(cfg.MongoURI).SetAppName("asobell-manager"))
	if err != nil {
		return nil, nil, fmt.Errorf("connect mongo: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, mongoTimeout)
	defer cancel()

	if err = client.Ping(pingCtx, readpref.Primary()); err != nil {
		_ = client.Disconnect(context.WithoutCancel(ctx))

		return nil, nil, fmt.Errorf("ping mongo: %w", err)
	}

	return client, mongostore.New(client.Database(cfg.MongoDB)), nil
}

func disconnect(ctx context.Context, client *mongo.Client, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(ctx, mongoTimeout)
	defer cancel()

	if err := client.Disconnect(ctx); err != nil {
		logger.ErrorContext(ctx, "disconnect mongo failed", "error", err)
	}
}

// Close は待ち受け・Provider への接続・Mongo を解放する。Run を呼ばなかった場合の後始末にも使う。
func (a *App) Close(ctx context.Context) {
	closeListener(a.grpcLis)
	closeListener(a.httpLis)

	if err := a.conn.Close(); err != nil {
		a.logger.ErrorContext(ctx, "close provider connection failed", "error", err)
	}

	disconnect(ctx, a.client, a.logger)
}

// ProviderState は監視タスクが観測している Provider の状態を返す。
// Provider 未接続のままなら offline で、WebConsole はこの値を表示する(docs/16 §6.2)。
func (a *App) ProviderState() domain.ProviderState { return a.monitor.State() }

// GRPCAddr は gRPC サーバーが実際に待ち受けているアドレスを返す。ポート 0 を指定したテスト向け。
func (a *App) GRPCAddr() string { return addrOf(a.grpcLis) }

// HTTPAddr は HTTP サーバーが実際に待ち受けているアドレスを返す。
func (a *App) HTTPAddr() string { return addrOf(a.httpLis) }

func addrOf(lis net.Listener) string {
	if lis == nil {
		return ""
	}

	return lis.Addr().String()
}

func closeListener(lis net.Listener) {
	if lis != nil {
		_ = lis.Close()
	}
}

func isClosed(err error) bool {
	return err == nil || errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) ||
		errors.Is(err, grpc.ErrServerStopped)
}
