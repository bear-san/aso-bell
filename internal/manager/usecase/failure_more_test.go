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

func channelNotFound() error {
	return rpcerr.New(codes.NotFound, rpcerr.ReasonChannelNotFound, "channel not found", nil)
}

func TestParticipationSurfacesRepositoryFailures(t *testing.T) {
	t.Parallel()

	join := func(t *testing.T, h *harness, ev *domain.Event) error {
		t.Helper()

		_, err := newParticipations(h).Apply(t.Context(), usecase.ParticipationInput{
			EventID: ev.ID, ChatUserID: guest, DisplayName: "ゲスト", Action: domain.ActionJoin,
		})

		return err
	}

	peek := func(t *testing.T, h *harness, ev *domain.Event) error {
		t.Helper()

		_, err := newParticipations(h).Apply(t.Context(), usecase.ParticipationInput{
			EventID: ev.ID, ChatUserID: guest, DisplayName: "ゲスト", Action: domain.ActionPeek,
		})

		return err
	}

	promote := func(t *testing.T, h *harness, ev *domain.Event) error {
		t.Helper()

		require.NoError(t, peek(t, h, ev))
		h.faults.failOn(opJobsCancel, errBoom)

		return join(t, h, ev)
	}

	leave := func(t *testing.T, h *harness, ev *domain.Event) error {
		t.Helper()

		_, err := newParticipations(h).Leave(t.Context(), ev.ID, guest)

		return err
	}

	remove := func(t *testing.T, h *harness, ev *domain.Event) error {
		t.Helper()

		return newParticipations(h).Remove(t.Context(), ev.ID, guest)
	}

	members := func(t *testing.T, h *harness, ev *domain.Event) error {
		t.Helper()

		_, _, err := newParticipations(h).ActiveMembers(t.Context(), ev.ID)

		return err
	}

	runFailCases(t, []failCase{
		{"参加: 現在の参加レコード取得", opPartsGetByUser, join},
		{"参加: 参加レコードの登録", opPartsUpsert, join},
		{"参加: 参加者数の加算", opEventsAddCount, join},
		{"チラ見: 期限切れジョブの登録", opJobsEnqueue, peek},
		{"参加へ昇格: チラ見ジョブの取消", opJobsCancel, promote},
		{"退出: イベント取得", opEventsGet, leave},
		{"退出: ワークスペース取得", opWorkspacesGet, leave},
		{"除外: 参加レコード取得", opPartsGetByUser, remove},
		{"参加者一覧: 参加レコード一覧", opPartsListByEvent, members},
	})
}

func TestLeaveSurfacesStatusUpdateFailure(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))
	apply(t, h, ev, domain.ActionJoin)

	h.faults.failOn(opPartsSetStatus, errBoom)

	_, err := newParticipations(h).Leave(t.Context(), ev.ID, guest)
	require.ErrorIs(t, err, errBoom)
}

func TestLeaveTwiceIsNotFound(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))
	apply(t, h, ev, domain.ActionJoin)

	svc := newParticipations(h)
	_, err := svc.Leave(t.Context(), ev.ID, guest)
	require.NoError(t, err)

	_, err = svc.Leave(t.Context(), ev.ID, guest)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestExpirePeekSurfacesRepositoryFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		op   string
	}{
		{"参加レコード取得", opPartsGet},
		{"イベント取得", opEventsGet},
		{"ワークスペース取得", opWorkspacesGet},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			ev := h.createEvent(t, testNow().Add(tenDays))
			res := apply(t, h, ev, domain.ActionPeek)

			h.clock.Advance(h.workspac.Settings.PeekDuration + time.Minute)
			h.faults.failOn(tc.op, errBoom)

			_, _, err := newParticipations(h).ExpirePeek(t.Context(), res.Participation.ID)
			require.ErrorIs(t, err, errBoom)
		})
	}
}

func TestApplyKeepsSideEffectsOnceWhenRecordAppearsFirst(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))
	apply(t, h, ev, domain.ActionJoin)

	before, err := h.repo.Events().Get(t.Context(), ev.ID)
	require.NoError(t, err)

	// 別の goroutine が先に登録し終えた状況を作る。Upsert の前後を見て「すでに参加済み」と直す。
	h.faults.failOn(opPartsGetByUser, domain.ErrNotFound)

	res, err := newParticipations(h).Apply(t.Context(), usecase.ParticipationInput{
		EventID: ev.ID, ChatUserID: guest, DisplayName: "ゲスト", Action: domain.ActionJoin,
	})
	require.NoError(t, err)
	assert.Equal(t, domain.TransitionAlreadyParticipant, res.Transition.Kind)

	after, err := h.repo.Events().Get(t.Context(), ev.ID)
	require.NoError(t, err)
	assert.Equal(t, before.ParticipantCount, after.ParticipantCount)
}

func TestJoinClosesEventWhenChannelIsGone(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))
	h.fake.FailNext("AddMember", channelNotFound())

	_, err := newParticipations(h).Apply(t.Context(), usecase.ParticipationInput{
		EventID: ev.ID, ChatUserID: guest, DisplayName: "ゲスト", Action: domain.ActionJoin,
	})
	require.ErrorIs(t, err, domain.ErrNotFound)

	stored, err := h.repo.Events().Get(t.Context(), ev.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.EventStatusEnded, stored.Status)
	assert.Equal(t, domain.EndReasonChannelLost, stored.EndReason)
}

func TestJoinSurfacesOtherChannelErrors(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))
	h.fake.FailNext("AddMember", rpcerr.New(
		codes.PermissionDenied, rpcerr.ReasonBotPermission, "missing scope", nil,
	))

	_, err := newParticipations(h).Apply(t.Context(), usecase.ParticipationInput{
		EventID: ev.ID, ChatUserID: guest, DisplayName: "ゲスト", Action: domain.ActionJoin,
	})
	require.ErrorIs(t, err, usecase.ErrBotPermission)

	stored, err := h.repo.Events().Get(t.Context(), ev.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.EventStatusOpen, stored.Status, "チャンネルは生きているのでイベントは続く")
}

func TestLeaveClosesEventWhenChannelIsGone(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))
	apply(t, h, ev, domain.ActionJoin)

	h.fake.FailNext("RemoveMember", channelNotFound())

	_, err := newParticipations(h).Leave(t.Context(), ev.ID, guest)
	require.ErrorIs(t, err, domain.ErrNotFound)

	stored, err := h.repo.Events().Get(t.Context(), ev.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.EndReasonChannelLost, stored.EndReason)
}

// eventWithoutChannel はチャンネルを持たないイベントを直接作る。
// 作成途中で中止されたイベントに後からボタンが押される状況を再現するため、ユースケースを通さない。
func eventWithoutChannel(t *testing.T, h *harness) *domain.Event {
	t.Helper()

	ev, err := domain.NewEvent(domain.NewEventParams{
		WorkspaceID:     h.workspac.ID,
		Title:           "チャンネルなし",
		StartsAt:        testNow().Add(tenDays),
		Organizer:       domain.ChatUserRef{UserID: organizer},
		OriginChannelID: "C0ORIGIN",
		ReminderPolicy:  domain.ReminderPolicy{Mode: domain.ReminderModeNone},
		CreatedVia:      domain.CreatedViaChat,
	}, testNow())
	require.NoError(t, err)

	created, err := h.repo.Events().Create(t.Context(), ev)
	require.NoError(t, err)

	return created
}

func TestParticipationWithoutChannelSkipsProvider(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := eventWithoutChannel(t, h)
	svc := newParticipations(h)

	_, err := svc.Apply(t.Context(), usecase.ParticipationInput{
		EventID: ev.ID, ChatUserID: guest, DisplayName: "ゲスト", Action: domain.ActionJoin,
	})
	require.NoError(t, err)

	_, err = svc.Leave(t.Context(), ev.ID, guest)
	require.NoError(t, err)

	assert.Empty(t, h.fake.CallsOf("AddMember"))
	assert.Empty(t, h.fake.CallsOf("RemoveMember"))
}

func TestRemoveSurfacesFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		op   string
	}{
		{"イベント取得", opEventsGet},
		{"参加レコードの更新", opPartsSetStatus},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			ev := h.createEvent(t, testNow().Add(tenDays))
			apply(t, h, ev, domain.ActionJoin)

			h.faults.failOn(tc.op, errBoom)
			require.ErrorIs(t, newParticipations(h).Remove(t.Context(), ev.ID, guest), errBoom)
		})
	}
}

func TestExpirePeekSurfacesDetachFailure(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))
	res := apply(t, h, ev, domain.ActionPeek)

	h.clock.Advance(h.workspac.Settings.PeekDuration + time.Minute)
	h.faults.failOn(opPartsSetStatus, errBoom)

	_, _, err := newParticipations(h).ExpirePeek(t.Context(), res.Participation.ID)
	require.ErrorIs(t, err, errBoom)
}

func TestJoinReportsChannelLossEvenWhenClosingFails(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))

	h.fake.FailNext("AddMember", channelNotFound())
	h.faults.failOn(opEventsUpdate, errBoom)

	_, err := newParticipations(h).Apply(t.Context(), usecase.ParticipationInput{
		EventID: ev.ID, ChatUserID: guest, DisplayName: "ゲスト", Action: domain.ActionJoin,
	})
	require.ErrorIs(t, err, domain.ErrNotFound, "終了処理の失敗は元の原因を覆い隠さない")
}
