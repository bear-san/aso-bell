package scheduler_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/manager/scheduler"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
)

func peek(t *testing.T, h *harness, ev *domain.Event) *domain.Participation {
	t.Helper()

	res, err := h.parts.Apply(t.Context(), usecase.ParticipationInput{
		EventID:    ev.ID,
		ChatUserID: guest,
		Action:     domain.ActionPeek,
	})
	require.NoError(t, err)

	return res.Participation
}

func TestReminderHandlerSkipsClosedEvent(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))
	job := pendingJob(t, h, ev.ID, domain.JobEventAutoEnd)

	// 終了してもワーカーが取り残しのジョブを引いた場合に no-op となることを見る。
	_, err := h.events.Close(t.Context(), usecase.CloseEventInput{
		EventID: ev.ID,
		Reason:  domain.EndReasonManual,
		Actor:   usecase.Actor{Console: true},
	})
	require.NoError(t, err)

	job.Kind = domain.JobReminder
	job.Payload.Sequence = 1

	require.NoError(t, h.handlers.Handle(t.Context(), job))

	logs, err := h.repo.ReminderLogs().ListByEvent(t.Context(), ev.ID)
	require.NoError(t, err)
	assert.Empty(t, logs)
}

func TestReminderHandlerIgnoresMissingEvent(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	err := h.handlers.Handle(t.Context(), &domain.Job{
		Kind:    domain.JobReminder,
		Payload: domain.JobPayload{EventID: "66e0a1b2c3d4e5f607182930", Sequence: 1},
	})

	require.NoError(t, err, "消えたイベントのジョブは成功扱いにする")
}

func TestPeekExpireHandlerRemovesMember(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))
	part := peek(t, h, ev)

	job := pendingJob(t, h, ev.ID, domain.JobPeekExpire)
	h.clock.Advance(h.workspace.Settings.PeekDuration + time.Minute)

	require.Equal(t, 1, h.runDue(t))

	assert.Equal(t, domain.JobDone, jobByID(t, h, job.ID).Status)

	stored, err := h.repo.Participations().Get(t.Context(), part.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ParticipationExpired, stored.Status)

	channel, ok := h.fake.Channel(ev.Channel.ChannelID)
	require.True(t, ok)
	assert.NotContains(t, channel.Members, guest)
}

func TestPeekExpireHandlerRescheduleWhenExtended(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))
	part := peek(t, h, ev)

	job := pendingJob(t, h, ev.ID, domain.JobPeekExpire)

	// 期限前に引いた場合は延長とみなして積み直す。
	require.ErrorIs(t, h.handlers.Handle(t.Context(), job), scheduler.ErrRescheduled)

	rescheduled := jobByID(t, h, job.ID)
	assert.Equal(t, domain.JobPending, rescheduled.Status)
	assert.True(t, part.ExpiresAt.Equal(rescheduled.RunAt))

	channel, ok := h.fake.Channel(ev.Channel.ChannelID)
	require.True(t, ok)
	assert.Contains(t, channel.Members, guest)
}

func TestAutoEndHandlerEndsEvent(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))
	job := pendingJob(t, h, ev.ID, domain.JobEventAutoEnd)

	h.clock.Advance(tenDays + h.workspace.Settings.AutoEndGrace + time.Minute)

	require.NoError(t, h.handlers.Handle(t.Context(), job))

	stored, err := h.repo.Events().Get(t.Context(), ev.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.EventStatusEnded, stored.Status)
	assert.Equal(t, domain.EndReasonAuto, stored.EndReason)

	// 終了でアーカイブジョブが積まれ、残りのリマインドは取り消される。
	assert.Len(t, h.jobsOfKind(t, ev.ID, domain.JobArchiveChannel, domain.JobPending), 1)
	assert.Empty(t, h.jobsOfKind(t, ev.ID, domain.JobReminder, domain.JobPending))
}

func TestAutoEndHandlerReschedulesWhenPostponed(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))
	job := pendingJob(t, h, ev.ID, domain.JobEventAutoEnd)

	postponed := testNow().Add(20 * day)

	_, err := h.events.Update(t.Context(), usecase.UpdateEventInput{
		EventID: ev.ID,
		Update:  domain.EventUpdate{StartsAt: &postponed},
		Actor:   usecase.Actor{ChatUserID: organizer},
	})
	require.NoError(t, err)

	h.clock.Advance(tenDays + h.workspace.Settings.AutoEndGrace + time.Minute)

	require.ErrorIs(t, h.handlers.Handle(t.Context(), job), scheduler.ErrRescheduled)

	stored, err := h.repo.Events().Get(t.Context(), ev.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.EventStatusOpen, stored.Status, "延期されたイベントは終了しない")

	rescheduled := jobByID(t, h, job.ID)
	assert.Equal(t, domain.JobPending, rescheduled.Status)
	assert.True(t, postponed.Add(h.workspace.Settings.AutoEndGrace).Equal(rescheduled.RunAt))
}

func TestArchiveHandlerIsIdempotent(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))

	_, err := h.events.Close(t.Context(), usecase.CloseEventInput{
		EventID: ev.ID,
		Reason:  domain.EndReasonManual,
		Actor:   usecase.Actor{Console: true},
	})
	require.NoError(t, err)

	job := pendingJob(t, h, ev.ID, domain.JobArchiveChannel)

	require.NoError(t, h.handlers.Handle(t.Context(), job))

	channel, ok := h.fake.Channel(ev.Channel.ChannelID)
	require.True(t, ok)
	assert.True(t, channel.Archived)

	stored, err := h.repo.Events().Get(t.Context(), ev.ID)
	require.NoError(t, err)
	assert.True(t, stored.Channel.Archived)

	before := len(h.fake.CallsOf("ArchiveChannel"))
	require.NoError(t, h.handlers.Handle(t.Context(), job))
	assert.Len(t, h.fake.CallsOf("ArchiveChannel"), before, "アーカイブ済みなら呼び直さない")
}

func TestUnknownJobKindIsPermanentFailure(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	err := h.handlers.Handle(t.Context(), &domain.Job{Kind: "unknown"})

	require.ErrorIs(t, err, domain.ErrPermanent)
}
