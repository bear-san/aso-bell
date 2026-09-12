package runtime_test

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/test/bufconn"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/provider/adapter"
	"github.com/bear-san/aso-bell/internal/provider/fake"
	"github.com/bear-san/aso-bell/internal/provider/runtime"
)

const bufSize = 1 << 20

const (
	organizer   = "U0123"
	guest       = "U0GUEST"
	originChan  = "C0ORIGIN"
	externalID  = "T0123"
	workspaceID = "66e0a1b2c3d4e5f607182930"
)

// harness は bufconn 上の fake.ManagerServer と fake.Adapter をつないだ Runtime のテスト環境。
// Manager 側も本物の gRPC を通すため、デッドラインやステータス変換も一緒に検証される。
type harness struct {
	runtime  *runtime.Runtime
	adapter  *fake.Adapter
	manager  *fake.ManagerServer
	provider asobellv1.ProviderServiceClient
	health   healthpb.HealthClient
}

func newHarness(t *testing.T, opts ...fake.AdapterOption) *harness {
	t.Helper()

	manager := fake.NewManagerServer()

	managerConn, stopManager, err := fake.ServeManager(manager)
	require.NoError(t, err)
	t.Cleanup(stopManager)

	a := fake.NewAdapter(opts...)
	rt := runtime.New(runtime.Config{
		Adapter:     a,
		Manager:     asobellv1.NewManagerServiceClient(managerConn),
		Logger:      slog.New(slog.DiscardHandler),
		Version:     "test",
		Timeout:     2 * time.Second,
		FastTimeout: time.Second,
		MinBackoff:  time.Millisecond,
		MaxBackoff:  5 * time.Millisecond,
	})

	providerConn, stopProvider, err := serveRuntime(rt)
	require.NoError(t, err)
	t.Cleanup(stopProvider)

	return &harness{
		runtime:  rt,
		adapter:  a,
		manager:  manager,
		provider: asobellv1.NewProviderServiceClient(providerConn),
		health:   healthpb.NewHealthClient(providerConn),
	}
}

// serveRuntime は Runtime を bufconn 上に起動する。本番と同じく ProviderService と Health を登録する。
func serveRuntime(rt *runtime.Runtime) (*grpc.ClientConn, func(), error) {
	lis := bufconn.Listen(bufSize)
	server := grpc.NewServer()
	rt.Register(server)

	go func() { _ = server.Serve(lis) }()

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
	)
	if err != nil {
		server.Stop()

		return nil, nil, fmt.Errorf("dial bufconn: %w", err)
	}

	return conn, func() {
		_ = conn.Close()

		server.Stop()

		_ = lis.Close()
	}, nil
}

func (h *harness) start(t *testing.T) {
	t.Helper()

	require.NoError(t, h.runtime.Start(t.Context()))
	t.Cleanup(func() { _ = h.runtime.Close(t.Context()) })
}

func workspace() *asobellv1.WorkspaceRef {
	return &asobellv1.WorkspaceRef{WorkspaceId: workspaceID, ExternalId: externalID}
}

func (h *harness) createChannel(t *testing.T, name string) *asobellv1.ChannelInfo {
	t.Helper()

	res, err := h.provider.CreatePrivateChannel(t.Context(), &asobellv1.CreatePrivateChannelRequest{
		Workspace: workspace(),
		Name:      name,
		Topic:     "ボドゲ会",
		MemberIds: []string{organizer},
	})
	require.NoError(t, err)

	return res.GetChannel()
}

func command(name string, responder adapter.Responder) adapter.Command {
	return adapter.Command{
		Payload: &asobellv1.Command{
			Workspace: workspace(),
			ChannelId: originChan,
			UserId:    organizer,
			Name:      name,
			RawText:   name,
		},
		Responder: responder,
	}
}
