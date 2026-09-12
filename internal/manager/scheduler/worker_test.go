package scheduler_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/manager/scheduler"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
)

func pendingJob(t *testing.T, h *harness, eventID string, kind domain.JobKind) *domain.Job {
	t.Helper()

	jobs := h.jobsOfKind(t, eventID, kind, domain.JobPending)
	require.Len(t, jobs, 1)

	return jobs[0]
}

func jobByID(t *testing.T, h *harness, id string) *domain.Job {
	t.Helper()

	job, err := h.repo.Jobs().Get(t.Context(), id)
	require.NoError(t, err)

	return job
}

func TestWorkerRunsDueReminder(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))

	// 7 日前のリマインドまで時計を進める。
	h.clock.Advance(3 * day)

	assert.Equal(t, 1, h.runDue(t))

	logs, err := h.repo.ReminderLogs().ListByEvent(t.Context(), ev.ID)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.True(t, ev.StartsAt.Add(-7*day).Equal(logs[0].ScheduledFor))

	assert.Len(t, h.jobsOfKind(t, ev.ID, domain.JobReminder, domain.JobDone), 1)
	assert.Len(t, h.jobsOfKind(t, ev.ID, domain.JobReminder, domain.JobPending), 2)
}

func TestWorkerSkipsFutureJobs(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createEvent(t, testNow().Add(tenDays))

	assert.Equal(t, 0, h.runDue(t), "実行時刻前のジョブは取得しない")
}

func TestWorkerRetriesOnProviderFailure(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))
	h.clock.Advance(3 * day)

	// リマインド投稿は 1 チャンネルのみなので、その失敗は履歴に残るが成功扱い。
	// ここではイベント取得後の SetChannel を失敗させられないため、アーカイブで再試行を確かめる。
	_, err := h.events.Close(t.Context(), usecase.CloseEventInput{
		EventID: ev.ID,
		Reason:  domain.EndReasonManual,
		Actor:   usecase.Actor{Console: true},
	})
	require.NoError(t, err)

	job := pendingJob(t, h, ev.ID, domain.JobArchiveChannel)
	h.fake.FailNext("ArchiveChannel", errors.New("boom"))

	assert.Equal(t, 1, h.runDue(t))

	retried := jobByID(t, h, job.ID)
	assert.Equal(t, domain.JobPending, retried.Status)
	assert.Equal(t, 1, retried.Attempts)
	assert.NotEmpty(t, retried.LastError)
	assert.True(t, retried.RunAt.After(h.clock.Now()), "backoff 後に再実行する")

	h.clock.Advance(domain.Backoff(1))
	assert.Equal(t, 1, h.runDue(t))
	assert.Equal(t, domain.JobDone, jobByID(t, h, job.ID).Status)
}

func TestWorkerFailsJobAfterMaxAttempts(t *testing.T) {
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

	for range domain.DefaultMaxAttempts + 1 {
		h.fake.FailNext("ArchiveChannel", errors.New("boom"))
		require.Equal(t, 1, h.runDue(t))
		h.clock.Advance(domain.Backoff(domain.DefaultMaxAttempts))
	}

	failed := jobByID(t, h, job.ID)
	assert.Equal(t, domain.JobFailed, failed.Status)
	assert.NotNil(t, failed.FinishedAt)
}

func TestWorkerReclaimsExpiredLease(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createEvent(t, testNow().Add(tenDays))
	h.clock.Advance(3 * day)

	// 実行中にクラッシュした状態を作る。
	job, err := h.repo.Jobs().Claim(t.Context(), h.clock.Now(), scheduler.DefaultLease)
	require.NoError(t, err)
	require.Equal(t, domain.JobReminder, job.Kind)

	assert.Equal(t, 0, h.runDue(t), "リース中は他のワーカーが取得しない")

	h.clock.Advance(scheduler.DefaultLease + time.Minute)
	assert.Equal(t, 1, h.runDue(t))
	assert.Equal(t, domain.JobDone, jobByID(t, h, job.ID).Status)
}

func TestWorkerRunStopsOnContextCancel(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))
	h.clock.Advance(3 * day)

	worker := scheduler.NewWorker(h.repo.Jobs(), h.handlers, h.clock, nil, scheduler.Config{
		PollInterval: time.Millisecond,
		BatchSize:    1,
	})

	ctx, cancel := context.WithCancel(t.Context())

	done := make(chan struct{})

	go func() {
		defer close(done)

		worker.Run(ctx)
	}()

	require.Eventually(t, func() bool {
		return len(h.jobsOfKind(t, ev.ID, domain.JobReminder, domain.JobDone)) == 1
	}, time.Second, 5*time.Millisecond)

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop after cancel")
	}
}
