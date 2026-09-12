package usecase_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
	"github.com/bear-san/aso-bell/internal/shared/rpcerr"
)

func TestSendReminderSurfacesRepositoryFailures(t *testing.T) {
	t.Parallel()

	send := func(t *testing.T, h *harness, ev *domain.Event) error {
		t.Helper()

		_, err := usecase.NewReminderService(h.deps).Send(t.Context(), sendInput(ev))

		return err
	}

	runFailCases(t, []failCase{
		{"イベント取得", opEventsGet, send},
		{"送信済み判定", opLogsSentSeq, send},
		{"ワークスペース取得", opWorkspacesGet, send},
		{"参加者一覧", opPartsListByEvent, send},
		{"送信履歴の記録", opLogsAppend, send},
	})
}

func TestSendReminderFallsBackToNowWhenScheduleIsUnknown(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))

	sent, err := usecase.NewReminderService(h.deps).Send(t.Context(), usecase.SendReminderInput{
		EventID:  ev.ID,
		Sequence: 1,
	})
	require.NoError(t, err)
	require.True(t, sent)

	logs, err := h.repo.ReminderLogs().ListByEvent(t.Context(), ev.ID)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.True(t, testNow().Equal(logs[0].ScheduledFor), "予定時刻が分からなければ実送信時刻を記録する")
}

func TestScheduleRemindersFailsWhenTimezoneIsUnknown(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))
	h.updateSettings(t, func(s *domain.WorkspaceSettings) { s.Timezone = "Mars/Olympus" })

	title := "名前を変える"
	_, err := h.events.Update(t.Context(), usecase.UpdateEventInput{
		EventID: ev.ID,
		Update:  domain.EventUpdate{Title: &title},
		Actor:   usecase.Actor{Console: true},
	})
	require.Error(t, err)
}

func TestWorkspaceSyncSurfacesRepositoryFailure(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.faults.failOn(opWorkspacesUpsert, errBoom)

	_, err := usecase.NewWorkspaceService(h.deps).
		Sync(t.Context(), domain.ProviderKindSlack, []usecase.ProviderWorkspace{
			{ExternalID: "T0999", Name: "別のワークスペース"},
		})
	require.ErrorIs(t, err, errBoom)
}

func TestSyncFromProviderSurfacesProviderFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
	}{
		{"種別の問い合わせ", "GetInfo"},
		{"ワークスペース一覧", "ListConnectedWorkspaces"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			h.fake.FailNext(tc.method, rpcerr.New(
				codes.Unavailable, rpcerr.ReasonPlatformUnavailable, "down", nil,
			))

			_, err := usecase.NewWorkspaceService(h.deps).SyncFromProvider(t.Context())
			require.ErrorIs(t, err, domain.ErrProviderUnavailable)
		})
	}
}

func TestSetRecruitChannelSurfacesRepositoryFailure(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.faults.failOn(opWorkspacesGet, errBoom)

	_, err := usecase.NewWorkspaceService(h.deps).SetRecruitChannel(t.Context(), h.workspac.ID, "C0RECRUIT")
	require.ErrorIs(t, err, errBoom)
}

func TestResolveWorkspaceRequiresReference(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	_, err := usecase.NewWorkspaceService(h.deps).Resolve(t.Context(), usecase.WorkspaceRef{})
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestProviderStatusStoredReadsRepository(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	stored := &domain.ProviderState{
		Kind:       domain.ProviderKindSlack,
		Connected:  false,
		Status:     domain.ProviderStatusOffline,
		Version:    "fake",
		LastSeenAt: testNow(),
		UpdatedAt:  testNow(),
	}
	require.NoError(t, h.repo.Meta().SaveProvider(t.Context(), stored))

	got, err := usecase.NewProviderStatusService(h.deps).Stored(t.Context())
	require.NoError(t, err)
	assert.Equal(t, domain.ProviderStatusOffline, got.Status)
}

func TestIdentityServiceSurfacesRepositoryFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		op   string
		run  func(t *testing.T, h *harness, svc *usecase.IdentityService, plain string) error
	}{
		{
			"発行: ワークスペース取得",
			opWorkspacesGet,
			func(t *testing.T, h *harness, svc *usecase.IdentityService, _ string) error {
				t.Helper()
				_, err := svc.IssueLinkToken(t.Context(), h.workspac.ID, guest, "ゲスト")

				return err
			},
		},
		{
			"発行: トークン保存",
			opTokensCreate,
			func(t *testing.T, h *harness, svc *usecase.IdentityService, _ string) error {
				t.Helper()
				_, err := svc.IssueLinkToken(t.Context(), h.workspac.ID, guest, "ゲスト")

				return err
			},
		},
		{
			"参照: トークン取得",
			opTokensGet,
			func(t *testing.T, _ *harness, svc *usecase.IdentityService, plain string) error {
				t.Helper()
				_, err := svc.GetLinkToken(t.Context(), plain)

				return err
			},
		},
		{
			"参照: ワークスペース取得",
			opWorkspacesGet,
			func(t *testing.T, _ *harness, svc *usecase.IdentityService, plain string) error {
				t.Helper()
				_, err := svc.GetLinkToken(t.Context(), plain)

				return err
			},
		},
		{
			"連携: ワークスペース取得",
			opWorkspacesGet,
			func(t *testing.T, _ *harness, svc *usecase.IdentityService, plain string) error {
				t.Helper()
				_, err := svc.Link(t.Context(), "66e0a1b2c3d4e5f607182930", plain)

				return err
			},
		},
		{
			"解除: 連携一覧",
			opIdentitiesByUser,
			func(t *testing.T, _ *harness, svc *usecase.IdentityService, _ string) error {
				t.Helper()

				return svc.Unlink(t.Context(), "66e0a1b2c3d4e5f607182930", "66e0a1b2c3d4e5f607182931")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			svc := usecase.NewIdentityService(h.deps)
			plain := issueToken(t, h)

			h.faults.failOn(tc.op, errBoom)
			require.ErrorIs(t, tc.run(t, h, svc, plain), errBoom)
		})
	}
}

func TestServicesFallBackToSystemDependencies(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	// Clock と Logger を省いた Deps でも組み立てられる(app 以外の呼び出し元向けの既定値)。
	svc := usecase.NewEventService(usecase.Deps{Repo: h.repo})

	page, err := svc.List(t.Context(), usecase.EventFilter{WorkspaceID: h.workspac.ID})
	require.NoError(t, err)
	assert.Empty(t, page.Events)

	assert.WithinDuration(t, time.Now(), usecase.SystemClock{}.Now(), time.Minute)
}

func TestCloseEventWithoutChannelSkipsMessages(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := eventWithoutChannel(t, h)

	ended, err := h.events.Close(t.Context(), usecase.CloseEventInput{
		EventID: ev.ID,
		Reason:  domain.EndReasonManual,
		Actor:   usecase.Actor{Console: true},
	})
	require.NoError(t, err)
	assert.Equal(t, domain.EventStatusEnded, ended.Status)
	assert.Empty(t, h.fake.CallsOf("PostMessage"))
	assert.Empty(t, h.fake.CallsOf("UnpinMessage"))
	assert.Empty(t, h.jobsOfKind(t, ev.ID, domain.JobArchiveChannel, domain.JobPending))
}
