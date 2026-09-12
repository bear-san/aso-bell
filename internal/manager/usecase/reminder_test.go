package usecase_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
)

func sendInput(ev *domain.Event) usecase.SendReminderInput {
	return usecase.SendReminderInput{
		EventID:      ev.ID,
		Sequence:     1,
		ScheduledFor: ev.StartsAt.Add(-day),
	}
}

func TestSendReminderPostsToEventChannel(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))
	apply(t, h, ev, domain.ActionJoin)

	before := len(h.fake.PostsIn(ev.Channel.ChannelID))

	sent, err := usecase.NewReminderService(h.deps).Send(t.Context(), sendInput(ev))
	require.NoError(t, err)
	assert.True(t, sent)

	posts := h.fake.PostsIn(ev.Channel.ChannelID)
	require.Len(t, posts, before+1)

	text := posts[len(posts)-1].Message.GetText()
	assert.Contains(t, text, "ボドゲ会")
	assert.Contains(t, text, organizer)
	assert.Contains(t, text, guest)

	logs, err := h.repo.ReminderLogs().ListByEvent(t.Context(), ev.ID)
	require.Len(t, logs, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, logs[0].Sequence)
	assert.True(t, ev.StartsAt.Add(-day).Equal(logs[0].ScheduledFor))
	require.Len(t, logs[0].Targets, 1)
	assert.Equal(t, domain.ReminderTargetEvent, logs[0].Targets[0].Kind)
	assert.True(t, logs[0].Targets[0].OK)
}

func TestSendReminderAlsoPostsToRecruitChannel(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.updateSettings(t, func(s *domain.WorkspaceSettings) { s.RecruitChannelID = "C0RECRUIT" })
	ev := h.createEvent(t, testNow().Add(tenDays))

	before := len(h.fake.PostsIn("C0RECRUIT"))

	sent, err := usecase.NewReminderService(h.deps).Send(t.Context(), sendInput(ev))
	require.NoError(t, err)
	assert.True(t, sent)

	posts := h.fake.PostsIn("C0RECRUIT")
	require.Len(t, posts, before+1)
	// 募集チャンネル向けは参加・チラ見ボタンを添える。
	assert.NotEmpty(t, posts[len(posts)-1].Message.GetButtons())

	logs, err := h.repo.ReminderLogs().ListByEvent(t.Context(), ev.ID)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Len(t, logs[0].Targets, 2)
}

func TestSendReminderIsIdempotentPerSequence(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))
	svc := usecase.NewReminderService(h.deps)

	sent, err := svc.Send(t.Context(), sendInput(ev))
	require.NoError(t, err)
	require.True(t, sent)

	before := len(h.fake.PostsIn(ev.Channel.ChannelID))

	sent, err = svc.Send(t.Context(), sendInput(ev))
	require.NoError(t, err)
	assert.False(t, sent)
	assert.Len(t, h.fake.PostsIn(ev.Channel.ChannelID), before)
}

func TestSendReminderSkipsClosedOrStartedEvent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(t *testing.T, h *harness, ev *domain.Event)
	}{
		{"終了済み", func(t *testing.T, h *harness, ev *domain.Event) {
			t.Helper()

			_, err := h.events.Close(t.Context(), usecase.CloseEventInput{
				EventID: ev.ID,
				Reason:  domain.EndReasonManual,
				Actor:   usecase.Actor{Console: true},
			})
			require.NoError(t, err)
		}},
		{"開始済み", func(t *testing.T, h *harness, _ *domain.Event) {
			t.Helper()
			h.clock.Advance(tenDays)
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			ev := h.createEvent(t, testNow().Add(tenDays))
			tc.setup(t, h, ev)

			before := len(h.fake.PostsIn(ev.Channel.ChannelID))

			sent, err := usecase.NewReminderService(h.deps).Send(t.Context(), sendInput(ev))
			require.NoError(t, err)
			assert.False(t, sent)
			assert.Len(t, h.fake.PostsIn(ev.Channel.ChannelID), before)

			logs, err := h.repo.ReminderLogs().ListByEvent(t.Context(), ev.ID)
			require.NoError(t, err)
			assert.Empty(t, logs)
		})
	}
}

func TestSendReminderRecordsPartialFailure(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.updateSettings(t, func(s *domain.WorkspaceSettings) { s.RecruitChannelID = "C0RECRUIT" })
	ev := h.createEvent(t, testNow().Add(tenDays))

	h.fake.FailNext("PostMessage", errors.New("boom"))

	sent, err := usecase.NewReminderService(h.deps).Send(t.Context(), sendInput(ev))
	require.NoError(t, err)
	assert.True(t, sent, "片方の失敗ではジョブを失敗させない")

	logs, err := h.repo.ReminderLogs().ListByEvent(t.Context(), ev.ID)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Len(t, logs[0].Targets, 2)
	assert.False(t, logs[0].Targets[0].OK)
	assert.NotEmpty(t, logs[0].Targets[0].Error)
	assert.True(t, logs[0].Targets[1].OK)
}

func TestScheduleRemindersSkipsAlreadySentTimes(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))

	// 7 日前のリマインドを送信済みにしてからポリシーを積み直す。
	sent, err := usecase.NewReminderService(h.deps).Send(t.Context(), usecase.SendReminderInput{
		EventID:      ev.ID,
		Sequence:     1,
		ScheduledFor: ev.StartsAt.Add(-7 * day),
	})
	require.NoError(t, err)
	require.True(t, sent)

	updated, err := h.events.SetReminderPolicy(t.Context(), ev.ID, domain.DefaultReminderPolicy(), usecase.Actor{
		ChatUserID: organizer,
	})
	require.NoError(t, err)
	require.Equal(t, domain.EventStatusOpen, updated.Status)

	pending := h.jobsOfKind(t, ev.ID, domain.JobReminder, domain.JobPending)
	require.Len(t, pending, 2, "送信済みの 7 日前は積み直さない")

	for _, job := range pending {
		assert.False(t, ev.StartsAt.Add(-7*day).Equal(job.RunAt))
		// sequence は取消済みジョブと衝突しないよう単調増加させる。
		assert.Greater(t, job.Payload.Sequence, 3)
	}
}
