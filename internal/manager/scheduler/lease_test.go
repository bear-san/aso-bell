package scheduler_test

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/manager/scheduler"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
)

// countingJobs は ExtendLease の呼び出し回数を数えるだけの JobRepository。
type countingJobs struct {
	usecase.JobRepository

	extended atomic.Int64
}

func (r *countingJobs) ExtendLease(ctx context.Context, id string, until, now time.Time) error {
	r.extended.Add(1)

	return r.JobRepository.ExtendLease(ctx, id, until, now)
}

// blockingHandler は解放されるまでジョブの処理を止める。
type blockingHandler struct {
	entered chan struct{}
	release chan struct{}
}

func newBlockingHandler() *blockingHandler {
	return &blockingHandler{entered: make(chan struct{}, 1), release: make(chan struct{})}
}

func (h *blockingHandler) Handle(_ context.Context, _ *domain.Job) error {
	select {
	case h.entered <- struct{}{}:
	default:
	}

	<-h.release

	return nil
}

func newBlockingWorker(
	t *testing.T,
	h *harness,
	lease time.Duration,
) (*scheduler.Worker, *countingJobs, *blockingHandler) {
	t.Helper()

	jobs := &countingJobs{JobRepository: h.repo.Jobs()}
	handler := newBlockingHandler()
	worker := scheduler.NewWorker(jobs, handler, h.clock, slog.New(slog.DiscardHandler), scheduler.Config{
		PollInterval: time.Millisecond,
		BatchSize:    1,
		Lease:        lease,
	})

	return worker, jobs, handler
}

func TestWorkerExtendsLeaseWhileRunning(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createEvent(t, testNow().Add(tenDays))
	h.clock.Advance(3 * day)

	worker, jobs, handler := newBlockingWorker(t, h, 20*time.Millisecond)

	ctx, cancel := context.WithCancel(t.Context())

	done := make(chan struct{})

	go func() {
		defer close(done)

		worker.Run(ctx)
	}()

	<-handler.entered

	require.Eventually(t, func() bool {
		return jobs.extended.Load() > 0
	}, time.Second, 5*time.Millisecond, "リース期間の半分ごとに延長する")

	cancel()
	close(handler.release)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop after cancel")
	}
}

func TestWorkerFinishesRunningJobAfterCancel(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))
	h.clock.Advance(3 * day)

	worker, _, handler := newBlockingWorker(t, h, scheduler.DefaultLease)

	ctx, cancel := context.WithCancel(t.Context())

	done := make(chan struct{})

	go func() {
		defer close(done)

		worker.Run(ctx)
	}()

	<-handler.entered
	cancel()

	// 停止要求のあとでハンドラを終わらせても、結果は書き戻される。
	close(handler.release)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop after cancel")
	}

	assert.Len(t, h.jobsOfKind(t, ev.ID, domain.JobReminder, domain.JobDone), 1)
}

func TestWorkerLimitsConcurrentJobs(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ev := h.createEvent(t, testNow().Add(tenDays))
	// 3 件のリマインドをすべて実行可能にする。
	h.clock.Advance(tenDays - time.Hour)

	require.Len(t, h.jobsOfKind(t, ev.ID, domain.JobReminder, domain.JobPending), 3)

	handler := newBlockingHandler()
	worker := scheduler.NewWorker(h.repo.Jobs(), handler, h.clock, slog.New(slog.DiscardHandler), scheduler.Config{
		PollInterval: time.Millisecond,
		BatchSize:    10,
		Workers:      1,
	})

	ctx, cancel := context.WithCancel(t.Context())

	done := make(chan struct{})

	go func() {
		defer close(done)

		worker.Run(ctx)
	}()

	<-handler.entered

	assert.Eventually(t, func() bool {
		return len(h.jobsOfKind(t, ev.ID, domain.JobReminder, domain.JobRunning)) == 1
	}, time.Second, 5*time.Millisecond, "同時実行数を超えてジョブを掴まない")

	cancel()
	close(handler.release)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop after cancel")
	}
}
