// Package scheduler は MongoDB を永続キューとして用いるジョブワーカーを提供する(docs/11-scheduler.md)。
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
)

// ErrRescheduled はハンドラ内でジョブを先の時刻へ積み直したことを表す。
// ワーカーはこの場合だけ結果(done / pending)を書き戻さず、ハンドラが設定した runAt を残す。
var ErrRescheduled = errors.New("job rescheduled")

// Services はハンドラが使うユースケース。app が組み立てて注入する。
type Services struct {
	Events        *usecase.EventService
	Participation *usecase.ParticipationService
	Reminders     *usecase.ReminderService
	Clock         usecase.Clock
	Jobs          usecase.JobRepository
}

// Handlers は kind ごとのジョブ処理をまとめる。
// すべてのハンドラは実行時点の DB を読み直し、前提が崩れていれば何もせず成功扱いにする(docs/11 §4)。
type Handlers struct {
	svc Services
}

// NewHandlers は Handlers を作る。
func NewHandlers(svc Services) *Handlers {
	if svc.Clock == nil {
		svc.Clock = usecase.SystemClock{}
	}

	return &Handlers{svc: svc}
}

// Handle はジョブ 1 件を処理する。
func (h *Handlers) Handle(ctx context.Context, job *domain.Job) error {
	switch job.Kind {
	case domain.JobReminder:
		return h.reminder(ctx, job)
	case domain.JobPeekExpire:
		return h.peekExpire(ctx, job)
	case domain.JobEventAutoEnd:
		return h.autoEnd(ctx, job)
	case domain.JobArchiveChannel:
		return h.archiveChannel(ctx, job)
	default:
		return fmt.Errorf("%w: unknown job kind %q", domain.ErrPermanent, string(job.Kind))
	}
}

func (h *Handlers) reminder(ctx context.Context, job *domain.Job) error {
	_, err := h.svc.Reminders.Send(ctx, usecase.SendReminderInput{
		EventID:      job.Payload.EventID,
		Sequence:     job.Payload.Sequence,
		ScheduledFor: job.Payload.ScheduledFor,
	})

	return skipMissing(err)
}

// peekExpire は期限切れのチラ見を除外する。延長されていれば新しい期限へ再スケジュールする(docs/11 §4.2)。
func (h *Handlers) peekExpire(ctx context.Context, job *domain.Job) error {
	part, expired, err := h.svc.Participation.ExpirePeek(ctx, job.Payload.ParticipationID)
	if err != nil {
		return skipMissing(err)
	}

	if expired || part == nil || part.Role != domain.RolePeeker || !part.IsActive() {
		return nil
	}

	if part.ExpiresAt == nil {
		return nil
	}

	return h.reschedule(ctx, job, *part.ExpiresAt)
}

// autoEnd は終了予定時刻での自動終了を行う。編集で延期されていれば再スケジュールする(docs/11 §4.3)。
func (h *Handlers) autoEnd(ctx context.Context, job *domain.Job) error {
	ev, err := h.svc.Events.Get(ctx, job.Payload.EventID)
	if err != nil {
		return skipMissing(err)
	}

	if !ev.IsOpen() {
		return nil
	}

	ws, err := h.svc.Events.Workspace(ctx, ev.WorkspaceID)
	if err != nil {
		return skipMissing(err)
	}

	now := h.svc.Clock.Now().UTC()
	if at := ev.AutoEndAt(ws.Settings.AutoEndGrace); at.After(now) {
		return h.reschedule(ctx, job, at)
	}

	_, err = h.svc.Events.Close(ctx, usecase.CloseEventInput{
		EventID: ev.ID,
		Reason:  domain.EndReasonAuto,
		Actor:   usecase.Actor{Console: true},
	})

	return skipMissing(err)
}

func (h *Handlers) archiveChannel(ctx context.Context, job *domain.Job) error {
	return skipMissing(h.svc.Events.ArchiveChannel(ctx, job.Payload.EventID))
}

func (h *Handlers) reschedule(ctx context.Context, job *domain.Job, runAt time.Time) error {
	if err := h.svc.Jobs.Reschedule(ctx, job.ID, runAt, h.svc.Clock.Now().UTC()); err != nil {
		return err
	}

	return ErrRescheduled
}

// skipMissing は対象が消えているジョブを成功扱いにする。存在しないものへ何度リトライしても結果は変わらない。
func skipMissing(err error) error {
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}

	return err
}
