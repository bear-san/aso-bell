package usecase_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
	"github.com/bear-san/aso-bell/internal/shared/rpcerr"
)

var errBoom = errors.New("boom")

// failCase は「リポジトリの特定の操作が失敗したとき、ユースケースがその失敗を握り潰さない」ことを
// 確かめるケース。Mongo の障害はインメモリ実装では再現できないため、faultRepo で差し込む。
type failCase struct {
	name string
	op   string
	run  func(t *testing.T, h *harness, ev *domain.Event) error
}

func runFailCases(t *testing.T, cases []failCase) {
	t.Helper()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			ev := h.createEvent(t, testNow().Add(tenDays))
			h.faults.failOn(tc.op, errBoom)

			require.ErrorIs(t, tc.run(t, h, ev), errBoom)
		})
	}
}

func TestEventServiceSurfacesRepositoryFailures(t *testing.T) {
	t.Parallel()

	title := "新しい名前"
	update := usecase.UpdateEventInput{
		Update: domain.EventUpdate{Title: &title},
		Actor:  usecase.Actor{Console: true},
	}

	create := func(t *testing.T, h *harness, _ *domain.Event) error {
		t.Helper()

		_, err := h.events.Create(t.Context(), h.createInput(testNow().Add(tenDays)))

		return err
	}

	edit := func(t *testing.T, h *harness, ev *domain.Event) error {
		t.Helper()

		in := update
		in.EventID = ev.ID
		_, err := h.events.Update(t.Context(), in)

		return err
	}

	closeEvent := func(t *testing.T, h *harness, ev *domain.Event) error {
		t.Helper()

		_, err := h.events.Close(t.Context(), usecase.CloseEventInput{
			EventID: ev.ID,
			Reason:  domain.EndReasonManual,
			Actor:   usecase.Actor{Console: true},
		})

		return err
	}

	archive := func(t *testing.T, h *harness, ev *domain.Event) error {
		t.Helper()

		return h.events.ArchiveChannel(t.Context(), ev.ID)
	}

	runFailCases(t, []failCase{
		{"作成: ワークスペース取得", opWorkspacesGet, create},
		{"作成: イベント登録", opEventsCreate, create},
		{"作成: 主催者の参加登録", opPartsUpsert, create},
		{"作成: 参加者数の加算", opEventsAddCount, create},
		{"作成: チャンネル記録", opEventsSetChannel, create},
		{"作成: リマインドの積み直し", opJobsCancel, create},
		{"作成: リマインド登録", opJobsEnqueue, create},
		{"作成: 送信済み判定", opLogsSentAt, create},
		{"作成: 既存ジョブの参照", opJobsListByEvent, create},
		{"編集: イベント取得", opEventsGet, edit},
		{"編集: ワークスペース取得", opWorkspacesGet, edit},
		{"編集: イベント更新", opEventsUpdate, edit},
		{"編集: リマインドの積み直し", opJobsCancel, edit},
		{"終了: イベント取得", opEventsGet, closeEvent},
		{"終了: イベント更新", opEventsUpdate, closeEvent},
		{"終了: ワークスペース取得", opWorkspacesGet, closeEvent},
		{"終了: ジョブ取消", opJobsCancel, closeEvent},
		{"終了: アーカイブジョブ登録", opJobsEnqueue, closeEvent},
		{"アーカイブ: イベント取得", opEventsGet, archive},
		{"アーカイブ: ワークスペース取得", opWorkspacesGet, archive},
		{"アーカイブ: チャンネル記録", opEventsSetChannel, archive},
	})
}

func TestCreateEventSurfacesAutoEndEnqueueFailure(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	// リマインドを積まない設定にして、失敗する enqueue を自動終了ジョブだけに絞る。
	h.updateSettings(t, func(s *domain.WorkspaceSettings) {
		s.DefaultReminderPolicy = domain.ReminderPolicy{Mode: domain.ReminderModeNone}
	})
	h.faults.failOn(opJobsEnqueue, errBoom)

	_, err := h.events.Create(t.Context(), h.createInput(testNow().Add(tenDays)))
	require.ErrorIs(t, err, errBoom)
}

func TestUpdateEventRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))

	empty := " "
	_, err := h.events.Update(t.Context(), usecase.UpdateEventInput{
		EventID: ev.ID,
		Update:  domain.EventUpdate{Title: &empty},
		Actor:   usecase.Actor{Console: true},
	})

	var invalid *domain.ValidationError
	require.ErrorAs(t, err, &invalid)
}

func TestCreateEventFailsWhenTimezoneIsUnknown(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.updateSettings(t, func(s *domain.WorkspaceSettings) { s.Timezone = "Mars/Olympus" })

	_, err := h.events.Create(t.Context(), h.createInput(testNow().Add(tenDays)))
	require.Error(t, err)
	assert.Empty(t, h.fake.CallsOf("CreatePrivateChannel"))
}

func TestCreateEventStopsOnNonRetryableChannelError(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.fake.FailNext("CreatePrivateChannel", rpcerr.New(
		codes.PermissionDenied, rpcerr.ReasonBotPermission, "missing scope", nil,
	))

	_, err := h.events.Create(t.Context(), h.createInput(testNow().Add(tenDays)))
	require.ErrorIs(t, err, usecase.ErrBotPermission)
	assert.Len(t, h.fake.CallsOf("CreatePrivateChannel"), 1, "やり直しても同じ結果になる失敗は再試行しない")
}

func TestCreateEventGivesUpAfterNameCollisions(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	for range 3 {
		h.fake.FailNext("CreatePrivateChannel", rpcerr.New(
			codes.AlreadyExists, rpcerr.ReasonNameTaken, "name taken", nil,
		))
	}

	_, err := h.events.Create(t.Context(), h.createInput(testNow().Add(tenDays)))
	require.ErrorIs(t, err, usecase.ErrNameTaken)
	assert.Len(t, h.fake.CallsOf("CreatePrivateChannel"), 3)
}

func TestCreateEventAbortsWithoutChannelOnFailure(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.fake.FailNext("CreatePrivateChannel", rpcerr.New(
		codes.PermissionDenied, rpcerr.ReasonBotPermission, "missing scope", nil,
	))

	_, err := h.events.Create(t.Context(), h.createInput(testNow().Add(tenDays)))
	require.Error(t, err)

	page, err := h.repo.Events().List(t.Context(), usecase.EventFilter{WorkspaceID: h.workspac.ID})
	require.NoError(t, err)
	require.Len(t, page.Events, 1)
	assert.Equal(t, domain.EventStatusCanceled, page.Events[0].Status)
	assert.Empty(t, h.jobsOfKind(t, page.Events[0].ID, domain.JobArchiveChannel, domain.JobPending))
}

func TestCreateEventAbortLogsFailuresAndKeepsError(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	// 概要投稿の失敗で中止に入り、その後始末そのものも失敗させる。
	h.fake.FailNext("PostMessage", nil)
	h.fake.FailNext("PostMessage", rpcerr.New(codes.Unavailable, rpcerr.ReasonPlatformUnavailable, "down", nil))
	h.faults.failOn(opEventsUpdate, errBoom)
	h.faults.failOn(opJobsEnqueue, errBoom)

	_, err := h.events.Create(t.Context(), h.createInput(testNow().Add(tenDays)))
	require.ErrorIs(t, err, domain.ErrProviderUnavailable, "後始末の失敗は元の失敗を覆い隠さない")
}

func TestCreateEventSurfacesRecruitPostFailure(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.updateSettings(t, func(s *domain.WorkspaceSettings) { s.RecruitChannelID = "C0RECRUIT" })
	h.fake.FailNext("PostMessage", nil)
	h.fake.FailNext("PostMessage", rpcerr.New(codes.NotFound, rpcerr.ReasonChannelNotFound, "gone", nil))

	_, err := h.events.Create(t.Context(), h.createInput(testNow().Add(tenDays)))
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestCreateEventUsesDiscordChannelNameLimit(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	ws, err := h.repo.Workspaces().Upsert(t.Context(), &domain.Workspace{
		Provider:   domain.ProviderKindDiscord,
		ExternalID: "G0123",
		Name:       "あそび部(Discord)",
		Settings:   domain.DefaultWorkspaceSettings(),
	}, testNow())
	require.NoError(t, err)

	in := h.createInput(testNow().Add(tenDays))
	in.WorkspaceID = ws.ID

	ev, err := h.events.Create(t.Context(), in)
	require.NoError(t, err)
	assert.NotEmpty(t, ev.Channel.Name)
}

func TestArchiveChannelKeepsRecordWhenChannelIsGone(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))

	// 手動で消されたチャンネルを、名前も分からない状態で参照している場合。
	require.NoError(t, h.repo.Events().SetChannel(
		t.Context(), ev.ID, domain.ChannelRef{ChannelID: "C0GONE"}, testNow(),
	))

	require.NoError(t, h.events.ArchiveChannel(t.Context(), ev.ID))

	stored, err := h.repo.Events().Get(t.Context(), ev.ID)
	require.NoError(t, err)
	assert.True(t, stored.Channel.Archived)
}

func TestArchiveChannelSurfacesProviderFailure(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))
	h.fake.FailNext("ArchiveChannel", rpcerr.New(
		codes.PermissionDenied, rpcerr.ReasonBotPermission, "missing scope", nil,
	))

	require.ErrorIs(t, h.events.ArchiveChannel(t.Context(), ev.ID), usecase.ErrBotPermission)
}

func TestCloseEventTolerantOfMessageFailures(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))

	h.fake.FailNext("UpdateMessage", rpcerr.New(codes.NotFound, rpcerr.ReasonMessageNotFound, "gone", nil))
	h.fake.FailNext("UnpinMessage", rpcerr.New(codes.NotFound, rpcerr.ReasonMessageNotFound, "gone", nil))
	h.fake.FailNext("PostMessage", rpcerr.New(codes.Unavailable, rpcerr.ReasonPlatformUnavailable, "down", nil))

	ended, err := h.events.Close(t.Context(), usecase.CloseEventInput{
		EventID: ev.ID,
		Reason:  domain.EndReasonManual,
		Actor:   usecase.Actor{Console: true},
	})
	require.NoError(t, err, "案内の投稿に失敗してもイベントは終了する")
	assert.Equal(t, domain.EventStatusEnded, ended.Status)
}

func TestCreateEventTolerantOfPinFailure(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.fake.FailNext("PinMessage", rpcerr.New(codes.ResourceExhausted, rpcerr.ReasonPinLimit, "too many pins", nil))

	_, err := h.events.Create(t.Context(), h.createInput(testNow().Add(tenDays)))
	require.NoError(t, err, "ピン留めの失敗はイベント作成を止めない")
}
