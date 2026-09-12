package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/shared/channelname"
)

// チャンネル名が衝突したときに連番を付けて作り直す回数(docs/07-bot-ux.md §2.3)。
const channelNameAttempts = 3

// CreateEventInput は新規イベントの入力。チャットからも WebConsole からも同じ入口を使う。
type CreateEventInput struct {
	WorkspaceID     string
	Title           string
	Description     string
	Location        string
	StartsAt        time.Time
	EndsAt          *time.Time
	Organizer       domain.ChatUserRef
	OriginChannelID string
	// ReminderPolicy が nil のときはワークスペースの既定ポリシーを使う。
	ReminderPolicy *domain.ReminderPolicy
	CreatedVia     domain.CreatedVia
	CreatedBy      string
}

// UpdateEventInput はイベント編集の入力。Actor は権限判定に使う。
type UpdateEventInput struct {
	EventID string
	Update  domain.EventUpdate
	Actor   Actor
}

// CloseEventInput は終了・中止の入力。
type CloseEventInput struct {
	EventID string
	Reason  domain.EndReason
	Actor   Actor
}

// Actor は操作者。チャットからの操作は ChatUserID を、WebConsole からの操作は Console を立てる。
type Actor struct {
	ChatUserID string
	// Console は WebConsole 経由かどうか。WebConsole からは主催者以外も管理操作ができる(docs/07 §6)。
	Console bool
}

// EventService はイベントのライフサイクルを扱う。
type EventService struct {
	deps Deps
}

// NewEventService は EventService を作る。
func NewEventService(deps Deps) *EventService {
	return &EventService{deps: deps.withDefaults()}
}

// Create はイベントを作り、チャンネル作成・告知投稿・ジョブ登録までを行う(docs/07-bot-ux.md §2.3)。
// 途中で失敗した場合はイベントを provision_failed で中止し、作成済みのチャンネルはアーカイブジョブに委ねる。
func (s *EventService) Create(ctx context.Context, in CreateEventInput) (*domain.Event, error) {
	if err := s.deps.requireProviderOnline(); err != nil {
		return nil, err
	}

	ws, err := s.deps.workspace(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}

	now := s.deps.now()

	policy := ws.Settings.DefaultReminderPolicy
	if in.ReminderPolicy != nil {
		policy = *in.ReminderPolicy
	}

	ev, err := domain.NewEvent(domain.NewEventParams{
		WorkspaceID:     ws.ID,
		Title:           in.Title,
		Description:     in.Description,
		Location:        in.Location,
		StartsAt:        in.StartsAt,
		EndsAt:          in.EndsAt,
		Organizer:       in.Organizer,
		OriginChannelID: in.OriginChannelID,
		ReminderPolicy:  policy,
		CreatedVia:      in.CreatedVia,
		CreatedBy:       in.CreatedBy,
	}, now)
	if err != nil {
		return nil, err
	}

	created, err := s.deps.Repo.Events().Create(ctx, ev)
	if err != nil {
		return nil, err
	}

	if err = s.registerOrganizer(ctx, ws, created, now); err != nil {
		return nil, err
	}

	if err = s.provision(ctx, ws, created); err != nil {
		s.abortProvision(ctx, created)

		return nil, err
	}

	if err = s.scheduleReminders(ctx, ws, created); err != nil {
		return nil, err
	}

	if err = s.deps.enqueue(
		ctx,
		domain.NewAutoEndJob(created.ID, created.AutoEndAt(ws.Settings.AutoEndGrace)),
	); err != nil {
		return nil, err
	}

	return created, nil
}

// Get は ID でイベントを取得する。
func (s *EventService) Get(ctx context.Context, id string) (*domain.Event, error) {
	return s.deps.event(ctx, id)
}

// GetByChannel はイベントチャンネル ID でイベントを取得する。
func (s *EventService) GetByChannel(ctx context.Context, channelID string) (*domain.Event, error) {
	return s.deps.Repo.Events().GetByChannelID(ctx, channelID)
}

// List は条件に合うイベントを返す。
func (s *EventService) List(ctx context.Context, filter EventFilter) (EventPage, error) {
	return s.deps.Repo.Events().List(ctx, filter)
}

// Update はイベントを編集し、告知・概要メッセージの更新とリマインドの再生成を行う。
func (s *EventService) Update(ctx context.Context, in UpdateEventInput) (*domain.Event, error) {
	ev, err := s.deps.event(ctx, in.EventID)
	if err != nil {
		return nil, err
	}

	if err = authorizeManage(ev, in.Actor); err != nil {
		return nil, err
	}

	ws, err := s.deps.workspace(ctx, ev.WorkspaceID)
	if err != nil {
		return nil, err
	}

	if err = ev.Apply(in.Update, s.deps.now()); err != nil {
		return nil, err
	}

	if err = s.deps.Repo.Events().Update(ctx, ev); err != nil {
		return nil, err
	}

	s.refreshMessages(ctx, ws, ev)

	if err = s.scheduleReminders(ctx, ws, ev); err != nil {
		return nil, err
	}

	return ev, nil
}

// Close はイベントを終了または中止する。二度目の呼び出しは何もせず、現在のイベントを返す(冪等)。
func (s *EventService) Close(ctx context.Context, in CloseEventInput) (*domain.Event, error) {
	ev, err := s.deps.event(ctx, in.EventID)
	if err != nil {
		return nil, err
	}

	if err = authorizeManage(ev, in.Actor); err != nil {
		return nil, err
	}

	now := s.deps.now()

	var changed bool
	if in.Reason == domain.EndReasonCanceled {
		changed = ev.Cancel(in.Reason, now)
	} else {
		changed = ev.End(in.Reason, now)
	}

	if !changed {
		return ev, nil
	}

	if err = s.deps.Repo.Events().Update(ctx, ev); err != nil {
		return nil, err
	}

	ws, err := s.deps.workspace(ctx, ev.WorkspaceID)
	if err != nil {
		return nil, err
	}

	s.closeMessages(ctx, ws, ev)

	if _, err = s.deps.Repo.Jobs().CancelByEvent(
		ctx,
		ev.ID,
		[]domain.JobKind{domain.JobReminder, domain.JobEventAutoEnd, domain.JobPeekExpire},
		now,
	); err != nil {
		return nil, err
	}

	if ev.Channel.ChannelID != "" {
		if err = s.deps.enqueue(ctx, domain.NewArchiveChannelJob(ev.ID, now)); err != nil {
			return nil, err
		}
	}

	return ev, nil
}

// ArchiveChannel はイベントチャンネルをアーカイブする(docs/11-scheduler.md §4.4)。
// アーカイブ済みなら何もしない。Discord はアーカイブ時に名前も変わるため、返された名前で記録を更新する。
func (s *EventService) ArchiveChannel(ctx context.Context, eventID string) error {
	ev, err := s.deps.event(ctx, eventID)
	if err != nil {
		return err
	}

	if ev.Channel.ChannelID == "" || ev.Channel.Archived {
		return nil
	}

	ws, err := s.deps.workspace(ctx, ev.WorkspaceID)
	if err != nil {
		return err
	}

	info, err := s.deps.Provider.ArchiveChannel(
		ctx, workspaceRefOf(ws), ev.Channel.ChannelID, ws.Settings.Discord.ArchiveCategoryID,
	)
	if err != nil {
		// 手動で削除されたチャンネルはアーカイブしようがないため、記録だけ合わせて終わる。
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}

		info = ChannelInfo{ID: ev.Channel.ChannelID, Name: ev.Channel.Name}
	}

	channel := domain.ChannelRef{ChannelID: ev.Channel.ChannelID, Name: info.Name, Archived: true}
	if channel.Name == "" {
		channel.Name = ev.Channel.Name
	}

	if err = s.deps.Repo.Events().SetChannel(ctx, ev.ID, channel, s.deps.now()); err != nil {
		return err
	}

	ev.Channel = channel

	return nil
}

// SetReminderPolicy はリマインドペースを変更し、ジョブを積み直す。
func (s *EventService) SetReminderPolicy(
	ctx context.Context,
	eventID string,
	policy domain.ReminderPolicy,
	actor Actor,
) (*domain.Event, error) {
	return s.Update(ctx, UpdateEventInput{
		EventID: eventID,
		Update:  domain.EventUpdate{ReminderPolicy: &policy},
		Actor:   actor,
	})
}

func (s *EventService) registerOrganizer(
	ctx context.Context,
	ws *domain.Workspace,
	ev *domain.Event,
	now time.Time,
) error {
	if _, err := s.deps.Repo.Participations().Upsert(ctx, ParticipationUpsert{
		EventID:     ev.ID,
		WorkspaceID: ws.ID,
		ChatUserID:  ev.Organizer.UserID,
		DisplayName: ev.Organizer.DisplayName,
		Role:        domain.RoleParticipant,
		Now:         now,
	}); err != nil {
		return err
	}

	if err := s.deps.Repo.Events().AddParticipantCount(ctx, ev.ID, 1, now); err != nil {
		return err
	}

	ev.ParticipantCount++

	return nil
}

func (s *EventService) provision(ctx context.Context, ws *domain.Workspace, ev *domain.Event) error {
	channel, err := s.createChannel(ctx, ws, ev)
	if err != nil {
		return err
	}

	ev.Channel = domain.ChannelRef{ChannelID: channel.ID, Name: channel.Name, Archived: channel.Archived}
	if err = s.deps.Repo.Events().SetChannel(ctx, ev.ID, ev.Channel, s.deps.now()); err != nil {
		return err
	}

	messages, err := s.postAnnouncements(ctx, ws, ev)
	if err != nil {
		return err
	}

	ev.Messages = messages

	return s.deps.Repo.Events().SetMessages(ctx, ev.ID, messages, s.deps.now())
}

func (s *EventService) createChannel(
	ctx context.Context,
	ws *domain.Workspace,
	ev *domain.Event,
) (ChannelInfo, error) {
	loc, err := ws.Settings.Location()
	if err != nil {
		return ChannelInfo{}, err
	}

	base := channelname.Build(
		ws.Settings.ChannelNamePrefix,
		ev.Title,
		ev.StartsAt.In(loc),
		channelNameLimit(ws.Provider),
	)

	var lastErr error

	for attempt := range channelNameAttempts {
		name := base
		if attempt > 0 {
			name = channelname.WithSuffix(base, attempt+1, channelNameLimit(ws.Provider))
		}

		channel, createErr := s.deps.Provider.CreatePrivateChannel(ctx, workspaceRefOf(ws), CreateChannelInput{
			Name:       name,
			Topic:      ev.Title,
			MemberIDs:  []string{ev.Organizer.UserID},
			CategoryID: ws.Settings.Discord.EventCategoryID,
		})
		if createErr == nil {
			return channel, nil
		}

		// 名前の衝突だけが連番でやり直せる。権限不足などはやり直しても同じ結果になる。
		if !errors.Is(createErr, ErrNameTaken) && !errors.Is(createErr, domain.ErrAlreadyExists) {
			return ChannelInfo{}, createErr
		}

		lastErr = createErr
	}

	return ChannelInfo{}, fmt.Errorf("create channel %q: %w", base, lastErr)
}

func (s *EventService) postAnnouncements(
	ctx context.Context,
	ws *domain.Workspace,
	ev *domain.Event,
) (domain.MessageRefs, error) {
	var messages domain.MessageRefs

	ref := workspaceRefOf(ws)
	peek := ws.Settings.PeekDuration

	announcement, err := s.deps.Provider.PostMessage(
		ctx, ref, ev.OriginChannelID, s.deps.Messages.Announcement(ev, peek),
	)
	if err != nil {
		return messages, err
	}

	messages.Announcement = &announcement

	s.deps.pin(ctx, ws, announcement)

	if recruit := ws.Settings.RecruitChannelID; recruit != "" && recruit != ev.OriginChannelID {
		// 募集チャンネルの告知はピン留めしない(docs/07 §2.3 手順 6)。
		recruitRef, postErr := s.deps.Provider.PostMessage(ctx, ref, recruit, s.deps.Messages.Announcement(ev, peek))
		if postErr != nil {
			return messages, postErr
		}

		messages.RecruitAnnouncement = &recruitRef
	}

	summary, err := s.deps.Provider.PostMessage(ctx, ref, ev.Channel.ChannelID, s.deps.Messages.Summary(ev))
	if err != nil {
		return messages, err
	}

	messages.Summary = &summary

	s.deps.pin(ctx, ws, summary)

	return messages, nil
}

// abortProvision は作りかけのイベントを中止扱いにし、チャンネルが残っていればアーカイブに回す。
func (s *EventService) abortProvision(ctx context.Context, ev *domain.Event) {
	now := s.deps.now()
	if !ev.Cancel(domain.EndReasonProvisionFailed, now) {
		return
	}

	if err := s.deps.Repo.Events().Update(ctx, ev); err != nil {
		s.deps.Logger.ErrorContext(ctx, "mark event provision failed", "event_id", ev.ID, "error", err)
	}

	if ev.Channel.ChannelID == "" {
		return
	}

	if err := s.deps.enqueue(ctx, domain.NewArchiveChannelJob(ev.ID, now)); err != nil {
		s.deps.Logger.ErrorContext(ctx, "enqueue archive job", "event_id", ev.ID, "error", err)
	}
}

func (s *EventService) refreshMessages(ctx context.Context, ws *domain.Workspace, ev *domain.Event) {
	peek := ws.Settings.PeekDuration
	s.deps.updateMessage(ctx, ws, ev.Messages.Announcement, s.deps.Messages.Announcement(ev, peek))
	s.deps.updateMessage(ctx, ws, ev.Messages.RecruitAnnouncement, s.deps.Messages.Announcement(ev, peek))
	s.deps.updateMessage(ctx, ws, ev.Messages.Summary, s.deps.Messages.Summary(ev))
}

// closeMessages は終了・中止時にピンを外し、ボタンを無効にした告知へ差し替えて締めの文言を投稿する。
func (s *EventService) closeMessages(ctx context.Context, ws *domain.Workspace, ev *domain.Event) {
	s.refreshMessages(ctx, ws, ev)
	s.deps.unpin(ctx, ws, ev.Messages.Announcement)
	s.deps.unpin(ctx, ws, ev.Messages.Summary)
	s.deps.post(ctx, ws, ev.Channel.ChannelID, s.deps.Messages.Closing(ev))
}

// authorizeManage は編集・終了などの管理操作を主催者に限る。WebConsole からは誰でも行える(docs/07 §6)。
func authorizeManage(ev *domain.Event, actor Actor) error {
	if actor.Console {
		return nil
	}

	if actor.ChatUserID != ev.Organizer.UserID {
		return domain.ErrForbidden
	}

	return nil
}

func channelNameLimit(kind domain.ProviderKind) int {
	if kind == domain.ProviderKindDiscord {
		return channelname.DiscordMaxLen
	}

	return channelname.SlackMaxLen
}

// Workspace はイベントが属するワークスペースを返す。ジョブハンドラが設定値を読むために使う。
func (s *EventService) Workspace(ctx context.Context, id string) (*domain.Workspace, error) {
	return s.deps.workspace(ctx, id)
}
