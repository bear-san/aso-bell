package rpc_test

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
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/manager/bot"
	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/manager/memstore"
	"github.com/bear-san/aso-bell/internal/manager/providerclient"
	"github.com/bear-san/aso-bell/internal/manager/rpc"
	"github.com/bear-san/aso-bell/internal/manager/testutil"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
	"github.com/bear-san/aso-bell/internal/provider/fake"
)

const (
	bufSize   = 1024 * 1024
	organizer = "U0123"
	guest     = "U0GUEST"
	origin    = "C0ORIGIN"
)

type stubStatus struct{}

func (stubStatus) State() domain.ProviderState {
	return domain.ProviderState{Kind: domain.ProviderKindSlack, Connected: true, Status: domain.ProviderStatusOnline}
}

// serveManager は ManagerService を bufconn 上に立てる。Provider 側も bufconn で動くため、
// Manager → Provider の相互呼び出しまで含めてインプロセスで検証できる(docs/13-testing.md §3)。
func serveManager(t *testing.T, svc asobellv1.ManagerServiceServer) asobellv1.ManagerServiceClient {
	t.Helper()

	lis := bufconn.Listen(bufSize)
	server := grpc.NewServer()
	asobellv1.RegisterManagerServiceServer(server, svc)

	go func() { _ = server.Serve(lis) }()

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = conn.Close()

		server.Stop()

		_ = lis.Close()
	})

	return asobellv1.NewManagerServiceClient(conn)
}

type harness struct {
	client   asobellv1.ManagerServiceClient
	repo     *memstore.Store
	provider *fake.ProviderServer
	events   *usecase.EventService
}

func testNow() time.Time {
	return time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC)
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	repo := memstore.New()
	server := fake.New()

	conn, stop, err := fake.Serve(server)
	require.NoError(t, err)
	t.Cleanup(stop)

	provider := providerclient.New(conn)
	logger := slog.New(slog.DiscardHandler)
	deps := usecase.Deps{
		Repo:     repo,
		Provider: provider,
		Status:   stubStatus{},
		Messages: bot.Renderer{},
		Clock:    testutil.NewFakeClock(testNow()),
		Logger:   logger,
	}

	workspaces := usecase.NewWorkspaceService(deps)
	events := usecase.NewEventService(deps)
	dispatcher := bot.NewDispatcher(bot.Services{
		Events:        events,
		Participation: usecase.NewParticipationService(deps),
		Workspaces:    workspaces,
		Identities:    usecase.NewIdentityService(deps),
		Provider:      provider,
		Clock:         deps.Clock,
		Logger:        logger,
		ConsoleURL:    "https://console.example.com",
		Async:         func(fn func(context.Context)) { fn(t.Context()) },
	})

	return &harness{
		client:   serveManager(t, rpc.NewManagerService(dispatcher, workspaces, stubStatus{}, logger)),
		repo:     repo,
		provider: server,
		events:   events,
	}
}

func (h *harness) report(t *testing.T) *asobellv1.WorkspaceRef {
	t.Helper()

	res, err := h.client.ReportWorkspaces(t.Context(), &asobellv1.ReportWorkspacesRequest{
		Workspaces: []*asobellv1.WorkspaceInfo{{ExternalId: "T0123", Name: "あそび部"}},
	})
	require.NoError(t, err)
	require.Len(t, res.GetWorkspaces(), 1)

	return res.GetWorkspaces()[0]
}

func TestReportWorkspacesRegistersWorkspace(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ref := h.report(t)

	assert.Equal(t, "T0123", ref.GetExternalId())
	assert.NotEmpty(t, ref.GetWorkspaceId())

	stored, err := h.repo.Workspaces().Get(t.Context(), ref.GetWorkspaceId())
	require.NoError(t, err)
	assert.Equal(t, "あそび部", stored.Name)
}

func TestHandleCommandReturnsReply(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ref := h.report(t)

	res, err := h.client.HandleCommand(t.Context(), &asobellv1.HandleCommandRequest{
		Command: &asobellv1.Command{
			Workspace: ref,
			ChannelId: origin,
			UserId:    organizer,
			RawText:   "help",
		},
	})
	require.NoError(t, err)

	assert.Equal(t, asobellv1.Visibility_VISIBILITY_EPHEMERAL, res.GetReply().GetVisibility())
	assert.Contains(t, res.GetReply().GetMessage().GetText(), "あそベル の使い方")
}

func TestHandleFormSubmitCreatesEventThroughProvider(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ref := h.report(t)

	res, err := h.client.HandleFormSubmit(t.Context(), &asobellv1.HandleFormSubmitRequest{
		Submission: &asobellv1.FormSubmission{
			Workspace: ref,
			UserId:    organizer,
			FormId:    bot.FormEventNew,
			Metadata:  origin,
			Values: map[string]string{
				bot.FieldTitle:  "ボドゲ会",
				bot.FieldDate:   "2026-09-20",
				bot.FieldTime:   "19:00",
				bot.FieldRemind: bot.RemindPresetStandard,
			},
		},
	})
	require.NoError(t, err)
	assert.Empty(t, res.GetReply().GetFormErrors())

	page, err := h.repo.Events().List(t.Context(), usecase.EventFilter{WorkspaceID: ref.GetWorkspaceId()})
	require.NoError(t, err)
	require.Len(t, page.Events, 1)

	// Manager → Provider の相互呼び出しでチャンネルが作られている。
	channel, ok := h.provider.Channel(page.Events[0].Channel.ChannelID)
	require.True(t, ok)
	assert.True(t, channel.IsPrivate)
	assert.Contains(t, channel.Members, organizer)
}

func TestHandleActionJoinsEvent(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ref := h.report(t)

	ev, err := h.events.Create(t.Context(), usecase.CreateEventInput{
		WorkspaceID:     ref.GetWorkspaceId(),
		Title:           "ボドゲ会",
		StartsAt:        testNow().Add(10 * 24 * time.Hour),
		Organizer:       domain.ChatUserRef{UserID: organizer},
		OriginChannelID: origin,
		CreatedVia:      domain.CreatedViaChat,
		CreatedBy:       organizer,
	})
	require.NoError(t, err)

	res, err := h.client.HandleAction(t.Context(), &asobellv1.HandleActionRequest{
		Action: &asobellv1.Action{
			Workspace: ref,
			ChannelId: origin,
			UserId:    guest,
			ActionId:  bot.ActionID(domain.ActionJoin, ev.ID),
		},
	})
	require.NoError(t, err)

	assert.Contains(t, res.GetReply().GetMessage().GetText(), "参加しました")

	channel, ok := h.provider.Channel(ev.Channel.ChannelID)
	require.True(t, ok)
	assert.Contains(t, channel.Members, guest)
}

func TestHandleCommandForUnknownWorkspace(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	res, err := h.client.HandleCommand(t.Context(), &asobellv1.HandleCommandRequest{
		Command: &asobellv1.Command{
			Workspace: &asobellv1.WorkspaceRef{ExternalId: "T0UNKNOWN"},
			ChannelId: origin,
			UserId:    organizer,
			RawText:   "list",
		},
	})
	require.NoError(t, err, "未同期のワークスペースは案内を返し、RPC は成功させる")

	assert.Contains(t, res.GetReply().GetMessage().GetText(), "同期されていません")
}

func TestHandleFormSubmitRejectsUnknownWorkspaceWithStatus(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	res, err := h.client.HandleFormSubmit(t.Context(), &asobellv1.HandleFormSubmitRequest{
		Submission: &asobellv1.FormSubmission{
			Workspace: &asobellv1.WorkspaceRef{WorkspaceId: "66e0a1b2c3d4e5f607182930"},
			UserId:    organizer,
			FormId:    bot.FormEventNew,
		},
	})
	require.NoError(t, err)
	assert.Contains(t, res.GetReply().GetMessage().GetText(), "同期されていません")
	assert.Equal(t, codes.OK, status.Code(err))
}
