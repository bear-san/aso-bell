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
	DefaultWorkers      = 4
)

// leaseHeartbeatDivisor はリース期間に対するハートビート間隔の比。
// 既定のリース 2 分に対し 1 分ごとに延長する(docs/11 §3.2)。
const leaseHeartbeatDivisor = 2

// Config はワーカーの動作設定。零値は既定値で補う(docs/11-scheduler.md §6)。
type Config struct {
	PollInterval time.Duration
	// BatchSize は 1 回のポーリングで取得する最大数。
	BatchSize int
	Lease     time.Duration
	// Workers は同時に実行するジョブの上限。
	Workers int
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

	if c.Workers <= 0 {
		c.Workers = DefaultWorkers
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
	// slots は同時実行数の上限。claim の前に確保し、掴んだジョブを待たせない。
	slots chan struct{}
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

	cfg = cfg.withDefaults()

	return &Worker{
		jobs:    jobs,
		handler: handler,
		clock:   clock,
		logger:  logger,
		cfg:     cfg,
		slots:   make(chan struct{}, cfg.Workers),
	}
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

		// 空きスロットを確保してから claim する。先に掴むと、実行を待つ間もリースを占有してしまう。
		select {
		case w.slots <- struct{}{}:
		default:
			return
		}

		job, err := w.jobs.Claim(ctx, w.clock.Now().UTC(), w.cfg.Lease)
		if err != nil {
			<-w.slots

			if !errors.Is(err, domain.ErrNotFound) && ctx.Err() == nil {
				w.logger.ErrorContext(ctx, "claim job failed", "error", err)
			}

			return
		}

		wg.Go(func() {
			defer func() { <-w.slots }()

			w.run(ctx, job)
		})
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
	started := w.clock.Now().UTC()

	if job.Attempts > job.MaxAttempts {
		w.fail(ctx, job, "max attempts exceeded", started)

		return
	}

	// 停止要求が来ても実行中のジョブは最後まで進める(docs/16-grpc.md §6.3)。
	// Provider 呼び出しを途中で切ると、投稿できたのかどうか分からないまま再試行することになるため。
	// 待ち時間の上限は Run を待つ側(app のシャットダウン)が決める。
	jobCtx := context.WithoutCancel(ctx)

	w.logger.InfoContext(jobCtx, "job started", jobAttrs(job)...)

	stopLease := w.keepLease(jobCtx, job)
	err := w.handler.Handle(jobCtx, job)

	stopLease()

	now := w.clock.Now().UTC()
	attrs := append(jobAttrs(job), "duration_ms", now.Sub(started).Milliseconds())

	switch {
	case err == nil:
		w.logger.InfoContext(jobCtx, "job done", attrs...)
		w.complete(jobCtx, job, now)
	case errors.Is(err, ErrRescheduled):
		// ハンドラ内で再スケジュールされたジョブは pending に戻っているため、結果を上書きしない。
		w.logger.InfoContext(jobCtx, "job rescheduled", attrs...)
	case errors.Is(err, domain.ErrPermanent) || isValidation(err):
		w.fail(jobCtx, job, err.Error(), now)
	default:
		w.retry(jobCtx, job, err, now)
	}
}

// keepLease はハンドラの実行中にリースを延長し続ける。返した関数を呼ぶと停止する。
// 数分かかるジョブが別ワーカーに「リース切れ」と見なされて二重実行されるのを防ぐ(docs/11 §3.2)。
func (w *Worker) keepLease(ctx context.Context, job *domain.Job) func() {
	interval := w.cfg.Lease / leaseHeartbeatDivisor
	if interval <= 0 {
		return func() {}
	}

	done := make(chan struct{})

	var wg sync.WaitGroup

	wg.Go(func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				now := w.clock.Now().UTC()
				if err := w.jobs.ExtendLease(ctx, job.ID, now.Add(w.cfg.Lease), now); err != nil {
					// 延長できないジョブを掴み続けても二重実行は防げないため、ハートビートを諦める。
					w.logger.WarnContext(ctx, "extend job lease failed", append(jobAttrs(job), "error", err)...)

					return
				}
			}
		}
	})

	return func() {
		close(done)
		wg.Wait()
	}
}

// jobAttrs はジョブのログ属性(docs/11 §7)。
func jobAttrs(job *domain.Job) []any {
	return []any{"job_id", job.ID, "job_kind", string(job.Kind), "event_id", job.EventID, "attempt", job.Attempts}
}

func (w *Worker) complete(ctx context.Context, job *domain.Job, now time.Time) {
	if err := w.jobs.Complete(ctx, job.ID, now); err != nil {
		w.logger.ErrorContext(ctx, "complete job failed", append(jobAttrs(job), "error", err)...)
	}
}

func (w *Worker) fail(ctx context.Context, job *domain.Job, reason string, now time.Time) {
	w.logger.ErrorContext(ctx, "job failed", append(jobAttrs(job), "reason", reason)...)

	if err := w.jobs.Fail(ctx, job.ID, reason, now); err != nil {
		w.logger.ErrorContext(ctx, "mark job failed", "job_id", job.ID, "error", err)
	}
}

func (w *Worker) retry(ctx context.Context, job *domain.Job, cause error, now time.Time) {
	runAt := now.Add(domain.Backoff(job.Attempts))

	w.logger.WarnContext(ctx, "job retry scheduled", append(jobAttrs(job), "run_at", runAt, "error", cause)...)

	if err := w.jobs.Retry(ctx, job.ID, runAt, cause.Error(), now); err != nil {
		w.logger.ErrorContext(ctx, "retry job failed", "job_id", job.ID, "error", err)
	}
}

func isValidation(err error) bool {
	var v *domain.ValidationError

	return errors.As(err, &v)
}
