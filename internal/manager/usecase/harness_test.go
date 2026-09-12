package usecase_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bear-san/aso-bell/internal/manager/bot"
	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/manager/memstore"
	"github.com/bear-san/aso-bell/internal/manager/providerclient"
	"github.com/bear-san/aso-bell/internal/manager/testutil"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
	"github.com/bear-san/aso-bell/internal/provider/fake"
)

// stubStatus は Provider の観測状態を固定する ProviderStatusPort。
type stubStatus struct {
	state domain.ProviderState
}

func (s *stubStatus) State() domain.ProviderState { return s.state }

// harness は memstore と bufconn 上の fake.ProviderServer を組み合わせた usecase のテスト環境。
// ProviderPort は本番と同じ providerclient を通すため、ステータス → ドメインエラー変換も一緒に検証される。
type harness struct {
	repo     *memstore.Store
	faults   *faultRepo
	fake     *fake.ProviderServer
	clock    *testutil.FakeClock
	status   *stubStatus
	deps     usecase.Deps
	events   *usecase.EventService
	workspac *domain.Workspace
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

	clock := testutil.NewFakeClock(testNow())
	status := &stubStatus{state: domain.ProviderState{
		Kind:      domain.ProviderKindSlack,
		Connected: true,
		Status:    domain.ProviderStatusOnline,
	}}

	faults := newFaultRepo(repo)
	deps := usecase.Deps{
		Repo:     faults,
		Provider: providerclient.New(conn),
		Status:   status,
		Messages: bot.Renderer{},
		Clock:    clock,
		Logger:   discardLogger(),
	}

	h := &harness{
		repo:   repo,
		faults: faults,
		fake:   server,
		clock:  clock,
		status: status,
		deps:   deps,
		events: usecase.NewEventService(deps),
	}
	h.workspac = h.seedWorkspace(t)

	return h
}

func (h *harness) seedWorkspace(t *testing.T) *domain.Workspace {
	t.Helper()

	ws, err := h.repo.Workspaces().Upsert(t.Context(), &domain.Workspace{
		Provider:   domain.ProviderKindSlack,
		ExternalID: "T0123",
		Name:       "あそび部",
		Settings:   domain.DefaultWorkspaceSettings(),
	}, testNow())
	require.NoError(t, err)

	return ws
}

func (h *harness) updateSettings(t *testing.T, mutate func(*domain.WorkspaceSettings)) {
	t.Helper()

	settings := h.workspac.Settings
	mutate(&settings)

	ws, err := h.repo.Workspaces().UpdateSettings(t.Context(), h.workspac.ID, settings, testNow())
	require.NoError(t, err)

	h.workspac = ws
}

// createEvent は既定値のイベントを 1 件作る。
func (h *harness) createEvent(t *testing.T, startsAt time.Time) *domain.Event {
	t.Helper()

	ev, err := h.events.Create(t.Context(), h.createInput(startsAt))
	require.NoError(t, err)

	return ev
}

func (h *harness) createInput(startsAt time.Time) usecase.CreateEventInput {
	return usecase.CreateEventInput{
		WorkspaceID:     h.workspac.ID,
		Title:           "ボドゲ会",
		Description:     "18時集合",
		Location:        "渋谷",
		StartsAt:        startsAt,
		Organizer:       domain.ChatUserRef{UserID: "U0123", DisplayName: "kentaro"},
		OriginChannelID: "C0ORIGIN",
		CreatedVia:      domain.CreatedViaChat,
		CreatedBy:       "U0123",
	}
}

func (h *harness) jobsOfKind(
	t *testing.T,
	eventID string,
	kind domain.JobKind,
	statuses ...domain.JobStatus,
) []*domain.Job {
	t.Helper()

	all, err := h.repo.Jobs().ListByEvent(t.Context(), eventID, statuses...)
	require.NoError(t, err)

	var out []*domain.Job

	for _, job := range all {
		if job.Kind == kind {
			out = append(out, job)
		}
	}

	return out
}
