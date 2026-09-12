package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/bear-san/aso-bell/internal/manager/domain"
)

// ParticipationInput は参加・チラ見の入力。
type ParticipationInput struct {
	EventID     string
	ChatUserID  string
	DisplayName string
	Action      domain.ParticipationAction
}

// ParticipationResult は参加操作の結果。Bot はこの内容から本人向けの返信を組み立てる。
type ParticipationResult struct {
	Event         *domain.Event
	Participation *domain.Participation
	Transition    domain.Transition
	// ExpiresAt はチラ見中の期限。参加者では零値。
	ExpiresAt time.Time
}

// ParticipationService は参加・チラ見・退出を扱う。
type ParticipationService struct {
	deps   Deps
	events *EventService
}

// NewParticipationService は ParticipationService を作る。
func NewParticipationService(deps Deps) *ParticipationService {
	normalized := deps.withDefaults()

	return &ParticipationService{deps: normalized, events: NewEventService(normalized)}
}

// Apply は参加・チラ見のボタン押下を処理する(docs/04-domain-model.md §2.5.1)。
// 連打しても副作用が 1 度だけになるよう、遷移は Upsert 前のレコードから決める。
func (s *ParticipationService) Apply(ctx context.Context, in ParticipationInput) (ParticipationResult, error) {
	ev, ws, err := s.openEvent(ctx, in.EventID)
	if err != nil {
		return ParticipationResult{}, err
	}

	prev, err := s.currentParticipation(ctx, in.EventID, in.ChatUserID)
	if err != nil {
		return ParticipationResult{}, err
	}

	transition := domain.DecideTransition(prev, in.Action)
	if !transition.AddToChannel && !transition.CancelPeekJob {
		return ParticipationResult{
			Event:         ev,
			Participation: prev,
			Transition:    transition,
			ExpiresAt:     expiresAtOf(prev),
		}, nil
	}

	now := s.deps.now()

	var expiresAt *time.Time

	if transition.Role == domain.RolePeeker {
		at := now.Add(ws.Settings.PeekDuration)
		expiresAt = &at
	}

	change, err := s.deps.Repo.Participations().Upsert(ctx, ParticipationUpsert{
		EventID:     ev.ID,
		WorkspaceID: ws.ID,
		ChatUserID:  in.ChatUserID,
		DisplayName: in.DisplayName,
		Role:        transition.Role,
		ExpiresAt:   expiresAt,
		Now:         now,
	})
	if err != nil {
		return ParticipationResult{}, err
	}

	// 同時押下では Upsert 前のレコードが遷移の真実になる。別の goroutine が先に登録していれば、
	// ここで改めて「すでに参加済み」と判定し、副作用を 1 度に保つ。
	if settled := domain.DecideTransition(change.Before, in.Action); settled.Kind != transition.Kind {
		return ParticipationResult{
			Event:         ev,
			Participation: change.After,
			Transition:    settled,
			ExpiresAt:     expiresAtOf(change.After),
		}, nil
	}

	if transition.AddToChannel {
		if err = s.joinChannel(ctx, ws, ev, in.ChatUserID); err != nil {
			return ParticipationResult{}, err
		}
	}

	if err = s.afterTransition(ctx, ws, ev, change.After, transition); err != nil {
		return ParticipationResult{}, err
	}

	return ParticipationResult{
		Event:         ev,
		Participation: change.After,
		Transition:    transition,
		ExpiresAt:     expiresAtOf(change.After),
	}, nil
}

// Leave は参加者が自分でイベントから抜ける。主催者は抜けられない(docs/07-bot-ux.md §6)。
func (s *ParticipationService) Leave(
	ctx context.Context,
	eventID, chatUserID string,
) (*domain.Participation, error) {
	ev, ws, err := s.openEvent(ctx, eventID)
	if err != nil {
		return nil, err
	}

	if chatUserID == ev.Organizer.UserID {
		return nil, domain.ErrOrganizerCannotLeave
	}

	part, err := s.deps.Repo.Participations().GetByUser(ctx, eventID, chatUserID)
	if err != nil {
		return nil, err
	}

	if !part.IsActive() {
		return nil, domain.ErrNotFound
	}

	if err = s.detach(ctx, ws, ev, part, domain.ParticipationLeft); err != nil {
		return nil, err
	}

	s.deps.post(ctx, ws, ev.Channel.ChannelID, s.deps.Messages.Left(chatUserID, ev.ParticipantCount))
	s.events.refreshMessages(ctx, ws, ev)

	return part, nil
}

// Remove は WebConsole から参加者を除外する。
func (s *ParticipationService) Remove(ctx context.Context, eventID, chatUserID string) error {
	if err := s.deps.requireProviderOnline(); err != nil {
		return err
	}

	ev, ws, err := s.openEvent(ctx, eventID)
	if err != nil {
		return err
	}

	part, err := s.deps.Repo.Participations().GetByUser(ctx, eventID, chatUserID)
	if err != nil {
		return err
	}

	if !part.IsActive() {
		return nil
	}

	if err = s.detach(ctx, ws, ev, part, domain.ParticipationRemoved); err != nil {
		return err
	}

	s.deps.post(ctx, ws, ev.Channel.ChannelID, s.deps.Messages.RemovedByOrganizer(chatUserID))
	s.events.refreshMessages(ctx, ws, ev)

	return nil
}

// ExpirePeek はチラ見の期限切れでチャンネルから除外する。除外まで行ったときだけ true を返す。
// 期限が延びている場合や既に抜けている場合は何もしない(docs/11-scheduler.md §4.2)。
func (s *ParticipationService) ExpirePeek(
	ctx context.Context,
	participationID string,
) (*domain.Participation, bool, error) {
	part, err := s.deps.Repo.Participations().Get(ctx, participationID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, false, nil
		}

		return nil, false, err
	}

	if part.Role != domain.RolePeeker || !part.IsActive() {
		return part, false, nil
	}

	if !part.PeekExpired(s.deps.now()) {
		return part, false, nil
	}

	ev, err := s.deps.event(ctx, part.EventID)
	if err != nil {
		return nil, false, err
	}

	ws, err := s.deps.workspace(ctx, ev.WorkspaceID)
	if err != nil {
		return nil, false, err
	}

	if err = s.detach(ctx, ws, ev, part, domain.ParticipationExpired); err != nil {
		return nil, false, err
	}

	return part, true, nil
}

func (s *ParticipationService) openEvent(
	ctx context.Context,
	eventID string,
) (*domain.Event, *domain.Workspace, error) {
	ev, err := s.deps.event(ctx, eventID)
	if err != nil {
		return nil, nil, err
	}

	if !ev.IsOpen() {
		return nil, nil, domain.ErrEventClosed
	}

	ws, err := s.deps.workspace(ctx, ev.WorkspaceID)
	if err != nil {
		return nil, nil, err
	}

	return ev, ws, nil
}

func (s *ParticipationService) currentParticipation(
	ctx context.Context,
	eventID, chatUserID string,
) (*domain.Participation, error) {
	part, err := s.deps.Repo.Participations().GetByUser(ctx, eventID, chatUserID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil //nolint:nilnil // 参加レコードが無いことは正常な入口の状態で、エラーではない。
	}

	return part, err
}

func (s *ParticipationService) joinChannel(
	ctx context.Context,
	ws *domain.Workspace,
	ev *domain.Event,
	userID string,
) error {
	if ev.Channel.ChannelID == "" {
		return nil
	}

	_, err := s.deps.Provider.AddMember(ctx, workspaceRefOf(ws), ev.Channel.ChannelID, userID)
	if err == nil {
		return nil
	}

	return s.handleChannelError(ctx, ev, err)
}

func (s *ParticipationService) leaveChannel(
	ctx context.Context,
	ws *domain.Workspace,
	ev *domain.Event,
	userID string,
) error {
	if ev.Channel.ChannelID == "" {
		return nil
	}

	_, err := s.deps.Provider.RemoveMember(ctx, workspaceRefOf(ws), ev.Channel.ChannelID, userID)
	if err == nil {
		return nil
	}

	return s.handleChannelError(ctx, ev, err)
}

// handleChannelError はイベントチャンネルが消えていた場合にイベントを終了させる(docs/07-bot-ux.md §8)。
// 手動で削除・アーカイブされたチャンネルには誰も出入りできず、イベントを続ける意味がないため。
func (s *ParticipationService) handleChannelError(ctx context.Context, ev *domain.Event, cause error) error {
	if !errors.Is(cause, domain.ErrNotFound) {
		return cause
	}

	if _, err := s.events.Close(ctx, CloseEventInput{
		EventID: ev.ID,
		Reason:  domain.EndReasonChannelLost,
		Actor:   Actor{Console: true},
	}); err != nil {
		s.deps.Logger.ErrorContext(ctx, "close event after channel lost", "event_id", ev.ID, "error", err)
	}

	return cause
}

// afterTransition は参加者数の更新・チャンネルへの通知・ジョブの登録取消をまとめて行う。
func (s *ParticipationService) afterTransition(
	ctx context.Context,
	ws *domain.Workspace,
	ev *domain.Event,
	part *domain.Participation,
	transition domain.Transition,
) error {
	if transition.Role == domain.RoleParticipant {
		if err := s.addCount(ctx, ev, 1); err != nil {
			return err
		}
	}

	if transition.CancelPeekJob {
		if _, err := s.deps.Repo.Jobs().CancelByEvent(
			ctx, ev.ID, []domain.JobKind{domain.JobPeekExpire}, s.deps.now(),
		); err != nil {
			return err
		}
	}

	if transition.Role == domain.RolePeeker && part.ExpiresAt != nil {
		job := domain.NewPeekExpireJob(ev.ID, part.ID, part.PeekCount, *part.ExpiresAt)
		if err := s.deps.enqueue(ctx, job); err != nil {
			return err
		}
	}

	s.deps.post(ctx, ws, ev.Channel.ChannelID, s.deps.Messages.Transition(
		transition.Kind, part.ChatUserID, ev.ParticipantCount, expiresAtOf(part),
	))
	s.events.refreshMessages(ctx, ws, ev)

	return nil
}

// detach はチャンネルから除外し、参加レコードを終了状態にする。
func (s *ParticipationService) detach(
	ctx context.Context,
	ws *domain.Workspace,
	ev *domain.Event,
	part *domain.Participation,
	status domain.ParticipationStatus,
) error {
	if err := s.leaveChannel(ctx, ws, ev, part.ChatUserID); err != nil {
		return err
	}

	if err := s.deps.Repo.Participations().SetStatus(ctx, part.ID, status, s.deps.now()); err != nil {
		return err
	}

	part.Status = status

	if part.Role != domain.RoleParticipant {
		return nil
	}

	return s.addCount(ctx, ev, -1)
}

func (s *ParticipationService) addCount(ctx context.Context, ev *domain.Event, delta int) error {
	if err := s.deps.Repo.Events().AddParticipantCount(ctx, ev.ID, delta, s.deps.now()); err != nil {
		return err
	}

	ev.ParticipantCount += delta

	return nil
}

func expiresAtOf(part *domain.Participation) time.Time {
	if part == nil || part.ExpiresAt == nil {
		return time.Time{}
	}

	return *part.ExpiresAt
}

// ActiveMembers はアクティブな参加者とチラ見中のユーザー ID を返す(`/asobell info`)。
func (s *ParticipationService) ActiveMembers(ctx context.Context, eventID string) ([]string, []string, error) {
	parts, err := s.deps.Repo.Participations().ListByEvent(ctx, eventID, domain.ParticipationActive)
	if err != nil {
		return nil, nil, err
	}

	var participants, peekers []string

	for _, part := range parts {
		if part.Role == domain.RoleParticipant {
			participants = append(participants, part.ChatUserID)

			continue
		}

		peekers = append(peekers, part.ChatUserID)
	}

	return participants, peekers, nil
}
