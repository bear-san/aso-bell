package app_test

import (
	"context"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/manager/app"
	"github.com/bear-san/aso-bell/internal/manager/config"
	"github.com/bear-san/aso-bell/internal/manager/domain"
	mongostore "github.com/bear-san/aso-bell/internal/manager/store/mongo"
	"github.com/bear-san/aso-bell/internal/manager/testutil"
	"github.com/bear-san/aso-bell/internal/shared/rpcauth"
)

// rpcToken は rpcauth.MinTokenLength を満たすテスト用の共有トークン。
const rpcToken = "0123456789abcdef0123456789abcdef"

// unreachableProvider は接続できない Provider のアドレス。Provider 未起動の状況を作る。
const unreachableProvider = "127.0.0.1:1"

func testConfig(t *testing.T, uri, dbName string) *config.Config {
	t.Helper()

	cfg, err := config.Load(map[string]string{
		"ASOBELL_HTTP_ADDR":              "127.0.0.1:0",
		"ASOBELL_RPC_ADDR":               "127.0.0.1:0",
		"ASOBELL_PROVIDER_ADDR":          unreachableProvider,
		"ASOBELL_RPC_TOKEN":              rpcToken,
		"ASOBELL_BASE_URL":               "https://console.example.com",
		"ASOBELL_MONGO_URI":              uri,
		"ASOBELL_MONGO_DB":               dbName,
		"ASOBELL_GOOGLE_CLIENT_ID":       "client-id",
		"ASOBELL_GOOGLE_CLIENT_SECRET":   "client-secret",
		"ASOBELL_JOBS_POLL_INTERVAL":     "50ms",
		"ASOBELL_PROVIDER_POLL_INTERVAL": "50ms",
		"ASOBELL_LOG_LEVEL":              "error",
	})
	require.NoError(t, err)

	return cfg
}

func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// start は Manager を起動し、Run が終わるのを待てるようにして返す。
func start(t *testing.T, cfg *config.Config) *app.App {
	t.Helper()

	manager, err := app.New(t.Context(), cfg, discardLogger())
	require.NoError(t, err)

	ctx, stop := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() { done <- manager.Run(ctx) }()

	t.Cleanup(func() {
		stop()

		select {
		case runErr := <-done:
			require.NoError(t, runErr)
		case <-time.After(30 * time.Second):
			t.Error("manager did not stop")
		}
	})

	return manager
}

func dialManager(t *testing.T, addr string) *grpc.ClientConn {
	t.Helper()

	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(rpcauth.UnaryClientInterceptor(rpcToken)),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	return conn
}

func TestMigrateCreatesIndexesAndRecordsSchemaVersion(t *testing.T) {
	uri, dbName := testutil.StartMongoURI(t)
	cfg := testConfig(t, uri, dbName)

	require.NoError(t, app.Migrate(t.Context(), cfg, discardLogger()))
	// 冪等であること。運用では serve のたびに実行される。
	require.NoError(t, app.Migrate(t.Context(), cfg, discardLogger()))

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	require.NoError(t, err)

	t.Cleanup(func() { _ = client.Disconnect(context.Background()) })

	store := mongostore.New(client.Database(dbName))

	version, err := store.Meta().SchemaVersion(t.Context())
	require.NoError(t, err)
	assert.Equal(t, mongostore.SchemaVersion, version)

	cursor, err := client.Database(dbName).Collection("jobs").Indexes().List(t.Context())
	require.NoError(t, err)

	var indexes []struct {
		Name   string `bson:"name"`
		Unique bool   `bson:"unique"`
	}
	require.NoError(t, cursor.All(t.Context(), &indexes))

	var unique int

	for _, idx := range indexes {
		if idx.Unique {
			unique++
		}
	}

	assert.Positive(t, unique, "dedupeKey の一意インデックスが作られている")
}

func TestServeAnswersWithoutProvider(t *testing.T) {
	uri, dbName := testutil.StartMongoURI(t)
	manager := start(t, testConfig(t, uri, dbName))

	for _, path := range []string{"/healthz", "/readyz"} {
		res, getErr := http.Get("http://" + manager.HTTPAddr() + path)
		require.NoError(t, getErr)
		require.NoError(t, res.Body.Close())
		assert.Equal(t, http.StatusOK, res.StatusCode, path)
	}

	conn := dialManager(t, manager.GRPCAddr())

	healthRes, err := healthpb.NewHealthClient(conn).Check(t.Context(), &healthpb.HealthCheckRequest{})
	require.NoError(t, err)
	assert.Equal(t, healthpb.HealthCheckResponse_SERVING, healthRes.GetStatus())

	assert.Equal(t, domain.ProviderStatusOffline, manager.ProviderState().Status,
		"Provider へ到達できない間は offline のまま")

	reported, err := asobellv1.NewManagerServiceClient(conn).ReportWorkspaces(
		t.Context(),
		&asobellv1.ReportWorkspacesRequest{
			Workspaces: []*asobellv1.WorkspaceInfo{{ExternalId: "T0123", Name: "あそび部"}},
		},
	)
	require.NoError(t, err, "Provider 未接続でも Manager の API は応答する")
	require.Len(t, reported.GetWorkspaces(), 1)
}

func TestHealthcheckChecksRunningServer(t *testing.T) {
	uri, dbName := testutil.StartMongoURI(t)
	cfg := testConfig(t, uri, dbName)
	manager := start(t, cfg)

	// 実際に待ち受けているポートへ向ける(テストでは 0 を指定しているため)。
	probe := *cfg
	probe.RPCAddr = manager.GRPCAddr()

	require.NoError(t, app.Healthcheck(t.Context(), &probe))
}

func TestHealthcheckFailsWhenNotServing(t *testing.T) {
	uri, dbName := testutil.StartMongoURI(t)
	cfg := testConfig(t, uri, dbName)
	cfg.RPCAddr = "127.0.0.1:1"

	require.Error(t, app.Healthcheck(t.Context(), cfg))
}

func TestServeRunsPendingJobsAfterRestart(t *testing.T) {
	uri, dbName := testutil.StartMongoURI(t)
	cfg := testConfig(t, uri, dbName)

	require.NoError(t, app.Migrate(t.Context(), cfg, discardLogger()))

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	require.NoError(t, err)

	t.Cleanup(func() { _ = client.Disconnect(context.Background()) })

	store := mongostore.New(client.Database(dbName))
	now := time.Now().UTC()

	// 前回のプロセスが積んだまま終わったジョブを用意する。
	// アーカイブ済みのチャンネルを指すため、ハンドラは Provider を呼ばずに完了する。
	ev := seedArchivedEvent(t, store, now)

	job, err := store.Jobs().Enqueue(t.Context(), domain.NewArchiveChannelJob(ev.ID, now.Add(-time.Minute)), now)
	require.NoError(t, err)

	start(t, cfg)

	require.Eventually(t, func() bool {
		stored, getErr := store.Jobs().Get(t.Context(), job.ID)

		return getErr == nil && stored.Status == domain.JobDone
	}, 30*time.Second, 100*time.Millisecond, "起動したワーカーが積み残しのジョブを実行する")
}

func seedArchivedEvent(t *testing.T, store *mongostore.Store, now time.Time) *domain.Event {
	t.Helper()

	ws, err := store.Workspaces().Upsert(t.Context(), &domain.Workspace{
		Provider:   domain.ProviderKindSlack,
		ExternalID: "T0123",
		Name:       "あそび部",
		Settings:   domain.DefaultWorkspaceSettings(),
	}, now)
	require.NoError(t, err)

	ev, err := domain.NewEvent(domain.NewEventParams{
		WorkspaceID:     ws.ID,
		Title:           "ボドゲ会",
		StartsAt:        now.Add(24 * time.Hour),
		Organizer:       domain.ChatUserRef{UserID: "U0123"},
		OriginChannelID: "C0ORIGIN",
		ReminderPolicy:  domain.ReminderPolicy{Mode: domain.ReminderModeNone},
		CreatedVia:      domain.CreatedViaChat,
	}, now)
	require.NoError(t, err)

	created, err := store.Events().Create(t.Context(), ev)
	require.NoError(t, err)

	require.NoError(t, store.Events().SetChannel(t.Context(), created.ID, domain.ChannelRef{
		ChannelID: "C0EVENT",
		Name:      "event-boardgame",
		Archived:  true,
	}, now))

	return created
}
