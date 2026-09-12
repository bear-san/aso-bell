package scheduler_test

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bear-san/aso-bell/internal/manager/bot"
	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/manager/memstore"
	"github.com/bear-san/aso-bell/internal/manager/providerclient"
	"github.com/bear-san/aso-bell/internal/manager/scheduler"
	"github.com/bear-san/aso-bell/internal/manager/testutil"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
	"github.com/bear-san/aso-bell/internal/provider/fake"
)

const (
	day       = 24 * time.Hour
	tenDays   = 10 * day
	organizer = "U0123"
	guest     = "U0GUEST"
)

type stubStatus struct{}

func (stubStatus) State() domain.ProviderState {
	return domain.ProviderState{
		Kind:      domain.ProviderKindSlack,
		Connected: true,
		Status:    domain.ProviderStatusOnline,
	}
}

// harness は memstore と fake.ProviderServer を組み合わせたジョブワーカーのテスト環境。
type harness struct {
	repo      *memstore.Store
	fake      *fake.ProviderServer
	clock     *testutil.FakeClock
	events    *usecase.EventService
	parts     *usecase.ParticipationService
	handlers  *scheduler.Handlers
	worker    *scheduler.Worker
	workspace *domain.Workspace
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
	deps := usecase.Deps{
		Repo:     repo,
		Provider: providerclient.New(conn),
		Status:   stubStatus{},
		Messages: bot.Renderer{},
		Clock:    clock,
		Logger:   slog.New(slog.DiscardHandler),
	}

	events := usecase.NewEventService(deps)
	parts := usecase.NewParticipationService(deps)
	handlers := scheduler.NewHandlers(scheduler.Services{
		Events:        events,
		Participation: parts,
		Reminders:     usecase.NewReminderService(deps),
		Clock:         clock,
		Jobs:          repo.Jobs(),
	})

	h := &harness{
		repo:     repo,
		fake:     server,
		clock:    clock,
		events:   events,
		parts:    parts,
		handlers: handlers,
		worker: scheduler.NewWorker(
			repo.Jobs(), handlers, clock, slog.New(slog.DiscardHandler), scheduler.Config{},
		),
	}

	ws, err := repo.Workspaces().Upsert(t.Context(), &domain.Workspace{
		Provider:   domain.ProviderKindSlack,
		ExternalID: "T0123",
		Name:       "あそび部",
		Settings:   domain.DefaultWorkspaceSettings(),
	}, testNow())
	require.NoError(t, err)

	h.workspace = ws

	return h
}

func (h *harness) createEvent(t *testing.T, startsAt time.Time) *domain.Event {
	t.Helper()

	ev, err := h.events.Create(t.Context(), usecase.CreateEventInput{
		WorkspaceID:     h.workspace.ID,
		Title:           "ボドゲ会",
		StartsAt:        startsAt,
		Organizer:       domain.ChatUserRef{UserID: organizer, DisplayName: "kentaro"},
		OriginChannelID: "C0ORIGIN",
		CreatedVia:      domain.CreatedViaChat,
		CreatedBy:       organizer,
	})
	require.NoError(t, err)

	return ev
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

// runDue は実行時刻を迎えたジョブを 1 巡分だけ処理する。
func (h *harness) runDue(t *testing.T) int {
	t.Helper()

	return h.worker.RunOnce(t.Context())
}
