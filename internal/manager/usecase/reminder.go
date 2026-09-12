package usecase

import (
	"context"
	"time"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/manager/domain"
)

// scheduleReminders は pending のリマインドを取り消してから、ポリシーに従って積み直す(docs/11-scheduler.md §5)。
// sequence はイベント内で単調増加させる。dedupeKey が `reminder:<eventId>:<sequence>` で一意なため、
// 1 から振り直すと取り消し済みジョブの鍵と衝突し、再生成が丸ごと弾かれてしまう。
func (s *EventService) scheduleReminders(ctx context.Context, ws *domain.Workspace, ev *domain.Event) error {
	now := s.deps.now()

	if _, err := s.deps.Repo.Jobs().CancelByEvent(ctx, ev.ID, []domain.JobKind{domain.JobReminder}, now); err != nil {
		return err
	}

	if ev.Status != domain.EventStatusOpen {
		return nil
	}

	loc, err := ws.Settings.Location()
	if err != nil {
		return err
	}

	times, err := ev.ReminderPolicy.Schedule(now, ev.StartsAt, loc)
	if err != nil {
		return err
	}

	sequence, err := s.nextReminderSequence(ctx, ev.ID)
	if err != nil {
		return err
	}

	for _, at := range times {
		// 既に同じ時刻のリマインドを送っていれば、ポリシー再生成で二重送信しない(docs/11 §5)。
		sent, sentErr := s.deps.Repo.ReminderLogs().SentAt(ctx, ev.ID, at)
		if sentErr != nil {
			return sentErr
		}

		if sent {
			continue
		}

		if err = s.deps.enqueue(ctx, domain.NewReminderJob(ev.ID, sequence, at)); err != nil {
			return err
		}

		sequence++
	}

	return nil
}

func (s *EventService) nextReminderSequence(ctx context.Context, eventID string) (int, error) {
	jobs, err := s.deps.Repo.Jobs().ListByEvent(ctx, eventID)
	if err != nil {
		return 0, err
	}

	highest := 0

	for _, job := range jobs {
		if job.Kind == domain.JobReminder {
			highest = max(highest, job.Payload.Sequence)
		}
	}

	return highest + 1, nil
}

// ReminderService はリマインドの送信を扱う。生成(スケジュール)は EventService が持つ。
type ReminderService struct {
	deps Deps
}

// NewReminderService は ReminderService を作る。
func NewReminderService(deps Deps) *ReminderService {
	return &ReminderService{deps: deps.withDefaults()}
}

// SendReminderInput は 1 回分のリマインド送信の入力。ジョブの payload から組み立てる。
type SendReminderInput struct {
	EventID  string
	Sequence int
	// ScheduledFor は本来の予定時刻。ポリシー再生成時の重複判定に使うため、実送信時刻とは別に記録する。
	ScheduledFor time.Time
}

// Send は 1 回分のリマインドを投稿する(docs/11-scheduler.md §4.1)。
// 送信できる状態になければ何もせず sent=false を返す。ジョブハンドラはこれを成功として扱う。
func (s *ReminderService) Send(ctx context.Context, in SendReminderInput) (bool, error) {
	ev, err := s.deps.event(ctx, in.EventID)
	if err != nil {
		return false, err
	}

	if !ev.IsOpen() {
		return false, nil
	}

	// 開始を過ぎたリマインドは送る意味がない(docs/11 §3)。
	now := s.deps.now()
	if !ev.StartsAt.After(now) {
		return false, nil
	}

	sent, err := s.deps.Repo.ReminderLogs().SentSequence(ctx, in.EventID, in.Sequence)
	if err != nil {
		return false, err
	}

	if sent {
		return false, nil
	}

	ws, err := s.deps.workspace(ctx, ev.WorkspaceID)
	if err != nil {
		return false, err
	}

	targets, err := s.postReminders(ctx, ws, ev, now)
	if err != nil {
		return false, err
	}

	scheduledFor := in.ScheduledFor
	if scheduledFor.IsZero() {
		scheduledFor = now
	}

	if _, err = s.deps.Repo.ReminderLogs().Append(ctx, &domain.ReminderLog{
		EventID:      ev.ID,
		Sequence:     in.Sequence,
		ScheduledFor: scheduledFor.UTC(),
		SentAt:       now,
		Targets:      targets,
	}); err != nil {
		return false, err
	}

	return true, nil
}

func (s *ReminderService) postReminders(
	ctx context.Context,
	ws *domain.Workspace,
	ev *domain.Event,
	now time.Time,
) ([]domain.ReminderTarget, error) {
	participants, err := s.participantIDs(ctx, ev.ID)
	if err != nil {
		return nil, err
	}

	targets := []domain.ReminderTarget{s.postTarget(
		ctx, ws, domain.ReminderTargetEvent, ev.Channel.ChannelID,
		s.deps.Messages.Reminder(ev, now, participants),
	)}

	if recruit := ws.Settings.RecruitChannelID; recruit != "" {
		targets = append(targets, s.postTarget(
			ctx, ws, domain.ReminderTargetRecruit, recruit,
			s.deps.Messages.RecruitReminder(ev, now, ws.Settings.PeekDuration),
		))
	}

	return targets, nil
}

// postTarget は 1 チャンネルへ投稿し、失敗しても履歴に残して続行する(docs/11 §4.1)。
// 片側だけの再送は重複投稿を招くため行わない。
func (s *ReminderService) postTarget(
	ctx context.Context,
	ws *domain.Workspace,
	kind domain.ReminderTargetKind,
	channelID string,
	msg *asobellv1.Message,
) domain.ReminderTarget {
	target := domain.ReminderTarget{Kind: kind, ChannelID: channelID}

	ref, err := s.deps.Provider.PostMessage(ctx, workspaceRefOf(ws), channelID, msg)
	if err != nil {
		s.deps.Logger.WarnContext(ctx, "post reminder failed",
			"event_channel", channelID, "target_kind", string(kind), "error", err)
		target.Error = err.Error()

		return target
	}

	target.OK = true
	target.MessageID = ref.MessageID

	return target
}

func (s *ReminderService) participantIDs(ctx context.Context, eventID string) ([]string, error) {
	parts, err := s.deps.Repo.Participations().ListByEvent(ctx, eventID, domain.ParticipationActive)
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(parts))

	for _, part := range parts {
		if part.Role == domain.RoleParticipant {
			ids = append(ids, part.ChatUserID)
		}
	}

	return ids, nil
}
