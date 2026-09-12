package app_test

import (
	"context"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/provider/app"
	"github.com/bear-san/aso-bell/internal/provider/config"
	"github.com/bear-san/aso-bell/internal/provider/fake"
	"github.com/bear-san/aso-bell/internal/shared/rpcauth"
)

// rpcToken は rpcauth.MinTokenLength を満たすテスト用の共有トークン。
const rpcToken = "0123456789abcdef0123456789abcdef"

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

// startManager は fake.ManagerServer を TCP で起動し、アドレスを返す。
// Provider は設定のアドレスへ自分でダイヤルするため、bufconn ではなく実ポートを使う。
func startManager(t *testing.T) (*fake.ManagerServer, string) {
	t.Helper()

	manager := fake.NewManagerServer()
	server := grpc.NewServer(
		grpc.ChainUnaryInterceptor(rpcauth.UnaryServerInterceptor(rpcToken, rpcauth.SkipInfra)),
	)
	asobellv1.RegisterManagerServiceServer(server, manager)

	var lc net.ListenConfig

	lis, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() { _ = server.Serve(lis) }()

	t.Cleanup(server.Stop)

	return manager, lis.Addr().String()
}

func testConfig(t *testing.T, managerAddr string) *config.Discord {
	t.Helper()

	cfg, err := config.LoadDiscord(map[string]string{
		"ASOBELL_MANAGER_ADDR":           managerAddr,
		"ASOBELL_RPC_TOKEN":              rpcToken,
		"ASOBELL_PROVIDER_LISTEN_ADDR":   "127.0.0.1:0",
		"ASOBELL_DISCORD_BOT_TOKEN":      "token",
		"ASOBELL_DISCORD_APPLICATION_ID": "100000000000000001",
		"ASOBELL_LOG_LEVEL":              "error",
	}, nil)
	require.NoError(t, err)

	return cfg
}

// start は Provider を起動し、Run が終わるのを待てるようにして返す。
func start(t *testing.T, cfg *config.Common, chat *fake.Adapter) *app.App {
	t.Helper()

	provider, err := app.New(t.Context(), cfg, chat, discardLogger(), "test")
	require.NoError(t, err)

	ctx, stop := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() { done <- provider.Run(ctx) }()

	t.Cleanup(func() {
		stop()

		select {
		case runErr := <-done:
			require.NoError(t, runErr)
		case <-time.After(15 * time.Second):
			t.Error("provider did not stop")
		}
	})

	return provider
}

func dialProvider(t *testing.T, addr, token string) *grpc.ClientConn {
	t.Helper()

	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(rpcauth.UnaryClientInterceptor(token)),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	return conn
}

func TestProviderConnectsAfterManagerIsReachable(t *testing.T) {
	t.Parallel()

	manager, managerAddr := startManager(t)
	cfg := testConfig(t, managerAddr)
	chat := fake.NewAdapter()

	provider := start(t, &cfg.Common, chat)

	conn := dialProvider(t, provider.Addr(), rpcToken)

	require.Eventually(t, func() bool {
		res, err := asobellv1.NewProviderServiceClient(conn).GetInfo(t.Context(), &asobellv1.GetInfoRequest{})

		return err == nil && res.GetConnected()
	}, 10*time.Second, 20*time.Millisecond, "疎通できたらチャットツールへ接続する")

	assert.Len(t, manager.Reported(), 1, "起動時に疎通確認として報告する")

	health, err := healthpb.NewHealthClient(conn).Check(t.Context(), &healthpb.HealthCheckRequest{})
	require.NoError(t, err)
	assert.Equal(t, healthpb.HealthCheckResponse_SERVING, health.GetStatus())
}

func TestProviderRequiresSharedToken(t *testing.T) {
	t.Parallel()

	_, managerAddr := startManager(t)
	cfg := testConfig(t, managerAddr)

	provider := start(t, &cfg.Common, fake.NewAdapter())
	conn := dialProvider(t, provider.Addr(), "wrong-token-wrong-token-wrong-tok")

	_, err := asobellv1.NewProviderServiceClient(conn).GetInfo(t.Context(), &asobellv1.GetInfoRequest{})
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestProviderHealthIsOpenToProbes(t *testing.T) {
	t.Parallel()

	_, managerAddr := startManager(t)
	cfg := testConfig(t, managerAddr)
	provider := start(t, &cfg.Common, fake.NewAdapter())

	// Health は認証対象外。Compose のヘルスチェックがトークンを持たずに叩けるようにする。
	conn := dialProvider(t, provider.Addr(), "")

	_, err := healthpb.NewHealthClient(conn).Check(t.Context(), &healthpb.HealthCheckRequest{})
	require.NoError(t, err)
}

func TestHealthcheckReachesRunningProvider(t *testing.T) {
	t.Parallel()

	_, managerAddr := startManager(t)
	cfg := testConfig(t, managerAddr)
	provider := start(t, &cfg.Common, fake.NewAdapter())

	probe := cfg.Common
	probe.ListenAddr = provider.Addr()

	require.Eventually(t, func() bool {
		return app.Healthcheck(t.Context(), &probe) == nil
	}, 10*time.Second, 20*time.Millisecond)
}

func TestHealthcheckFailsWhenNothingIsListening(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t, "127.0.0.1:1")
	cfg.ListenAddr = "127.0.0.1:1"

	require.Error(t, app.Healthcheck(t.Context(), &cfg.Common))
}

func TestNewFailsWithoutManagerAddress(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t, "127.0.0.1:1")
	cfg.ManagerAddr = ""

	_, err := app.New(t.Context(), &cfg.Common, fake.NewAdapter(), discardLogger(), "test")
	require.Error(t, err)
}
