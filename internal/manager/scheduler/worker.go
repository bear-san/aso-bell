package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
)

// ワーカーの既定値(docs/11-scheduler.md §3)。
const (
	DefaultPollInterval = 10 * time.Second
	DefaultBatchSize    = 10
	DefaultLease        = 2 * time.Minute
)

// Config はワーカーの動作設定。零値は既定値で補う。
type Config struct {
	PollInterval time.Duration
	BatchSize    int
	Lease        time.Duration
}

func (c Config) withDefaults() Config {
	if c.PollInterval <= 0 {
		c.PollInterval = DefaultPollInterval
	}

	if c.BatchSize <= 0 {
		c.BatchSize = DefaultBatchSize
	}

	if c.Lease <= 0 {
		c.Lease = DefaultLease
	}

	return c
}

// Handler はジョブ 1 件を処理する。*Handlers が実装する。
type Handler interface {
	Handle(ctx context.Context, job *domain.Job) error
}

// Worker は jobs コレクションをポーリングしてジョブを実行する。
type Worker struct {
	jobs    usecase.JobRepository
	handler Handler
	clock   usecase.Clock
	logger  *slog.Logger
	cfg     Config
}

// NewWorker は Worker を作る。
func NewWorker(
	jobs usecase.JobRepository,
	handler Handler,
	clock usecase.Clock,
	logger *slog.Logger,
	cfg Config,
) *Worker {
	if clock == nil {
		clock = usecase.SystemClock{}
	}

	if logger == nil {
		logger = slog.Default()
	}

	return &Worker{jobs: jobs, handler: handler, clock: clock, logger: logger, cfg: cfg.withDefaults()}
}

// Run は ctx がキャンセルされるまでポーリングを続ける。実行中のジョブを待ってから戻る。
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()

	var wg sync.WaitGroup

	defer wg.Wait()

	for {
		w.poll(ctx, &wg)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// poll は 1 巡分のジョブを取得して並行に実行する。
func (w *Worker) poll(ctx context.Context, wg *sync.WaitGroup) {
	for range w.cfg.BatchSize {
		if ctx.Err() != nil {
			return
		}

		job, err := w.jobs.Claim(ctx, w.clock.Now().UTC(), w.cfg.Lease)
		if err != nil {
			if !errors.Is(err, domain.ErrNotFound) && ctx.Err() == nil {
				w.logger.ErrorContext(ctx, "claim job failed", "error", err)
			}

			return
		}

		wg.Go(func() { w.run(ctx, job) })
	}
}

// RunOnce は取得できるジョブを 1 巡分だけ同期的に処理し、処理件数を返す。テストと起動直後の消化に使う。
func (w *Worker) RunOnce(ctx context.Context) int {
	var done int

	for range w.cfg.BatchSize {
		job, err := w.jobs.Claim(ctx, w.clock.Now().UTC(), w.cfg.Lease)
		if err != nil {
			if !errors.Is(err, domain.ErrNotFound) {
				w.logger.ErrorContext(ctx, "claim job failed", "error", err)
			}

			return done
		}

		w.run(ctx, job)

		done++
	}

	return done
}

// run は 1 件のジョブを処理し、結果を jobs に書き戻す(docs/11 §3.2)。
func (w *Worker) run(ctx context.Context, job *domain.Job) {
	now := w.clock.Now().UTC()

	if job.Attempts > job.MaxAttempts {
		w.fail(ctx, job, "max attempts exceeded", now)

		return
	}

	err := w.handler.Handle(ctx, job)
	if err == nil {
		w.complete(ctx, job, now)

		return
	}

	// ハンドラ内で再スケジュールされたジョブは pending に戻っているため、結果を上書きしない。
	if errors.Is(err, ErrRescheduled) {
		return
	}

	if errors.Is(err, domain.ErrPermanent) || isValidation(err) {
		w.fail(ctx, job, err.Error(), now)

		return
	}

	w.retry(ctx, job, err, now)
}

func (w *Worker) complete(ctx context.Context, job *domain.Job, now time.Time) {
	if err := w.jobs.Complete(ctx, job.ID, now); err != nil {
		w.logger.ErrorContext(ctx, "complete job failed", "job_id", job.ID, "job_kind", string(job.Kind), "error", err)
	}
}

func (w *Worker) fail(ctx context.Context, job *domain.Job, reason string, now time.Time) {
	w.logger.ErrorContext(ctx, "job failed", "job_id", job.ID, "job_kind", string(job.Kind), "reason", reason)

	if err := w.jobs.Fail(ctx, job.ID, reason, now); err != nil {
		w.logger.ErrorContext(ctx, "mark job failed", "job_id", job.ID, "error", err)
	}
}

func (w *Worker) retry(ctx context.Context, job *domain.Job, cause error, now time.Time) {
	runAt := now.Add(domain.Backoff(job.Attempts))

	w.logger.WarnContext(ctx, "job retry scheduled",
		"job_id", job.ID, "job_kind", string(job.Kind), "attempts", job.Attempts, "error", cause)

	if err := w.jobs.Retry(ctx, job.ID, runAt, cause.Error(), now); err != nil {
		w.logger.ErrorContext(ctx, "retry job failed", "job_id", job.ID, "error", err)
	}
}

func isValidation(err error) bool {
	var v *domain.ValidationError

	return errors.As(err, &v)
}
