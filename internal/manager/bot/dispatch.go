package bot

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
)

// 返信の定型文言(docs/07-bot-ux.md §3.4, §6, §8)。
const (
	msgNotEventChannel = "イベントチャンネルで実行してください"
	msgOrganizerOnly   = "この操作は主催者のみ実行できます"
	msgEventClosed     = "このイベントは終了しています"
	msgProviderFailed  = "チャンネルへの追加に失敗しました。時間をおいて再度お試しください"
	msgUnknownCommand  = "知らないコマンドです。`/asobell help` で使い方を確認できます"
	msgWorkspaceUnkown = "ワークスペースがまだ同期されていません。しばらくしてからお試しください"
	msgNotLinked       = "WebConsole と連携していません。`/asobell login` から連携できます"
	msgUnlinked        = "WebConsole との連携を解除しました"
	msgNotParticipant  = "このイベントには参加していません"
	msgOrganizerLeave  = "主催者は抜けられません。`/asobell end` で終了するか `/asobell cancel` で中止してください"
	msgEventCreating   = "イベントを作成しています。少し待ってね"
	msgEventUpdated    = "イベントを更新しました"
	msgRecruitCleared  = "このワークスペースの募集チャンネル設定を解除しました"
	msgInvalidWhen     = "日時を解釈できません。`2026-09-20 19:00` `9/20 19:00` `明日 19:00` のように指定してください"
	msgInvalidEnds     = "終了予定を解釈できません。`+3h` `22:00` `9/20 22:00` のように指定してください"
	msgInvalidPace     = "リマインドを解釈できません。`7d,1d,3h` `every 2d 20:00` `off` のように指定してください"
)

// defaultAsyncTimeout はフォーム送信後に非同期で行うイベント作成の制限時間。
// Slack のモーダルは即座に閉じるため、作成はリクエストの寿命とは切り離して実行する(docs/07 §2.1)。
const defaultAsyncTimeout = 2 * time.Minute

// Services は Bot Core が使うユースケース。app が組み立てて注入する。
type Services struct {
	Events        *usecase.EventService
	Participation *usecase.ParticipationService
	Workspaces    *usecase.WorkspaceService
	Identities    *usecase.IdentityService
	Provider      usecase.ProviderPort
	Clock         usecase.Clock
	Logger        *slog.Logger
	// ConsoleURL は WebConsole の公開 URL。連携 URL の組み立てに使う。
	ConsoleURL string
	// Async は非同期処理の起動方法。テストは同期実行に差し替える。
	Async func(func(context.Context))
}

// Dispatcher は Provider から届いたコマンド・ボタン・フォームを処理して Reply を組み立てる。
type Dispatcher struct {
	svc Services
}

// NewDispatcher は Dispatcher を作る。
func NewDispatcher(svc Services) *Dispatcher {
	if svc.Clock == nil {
		svc.Clock = usecase.SystemClock{}
	}

	if svc.Logger == nil {
		svc.Logger = slog.Default()
	}

	if svc.Async == nil {
		svc.Async = goAsync
	}

	return &Dispatcher{svc: svc}
}

// goAsync は呼び出し元の値(ログ属性など)を引き継ぎつつ、キャンセルだけを切り離して実行する。
func goAsync(fn func(context.Context)) {
	go fn(context.Background())
}

func (d *Dispatcher) now() time.Time {
	return d.svc.Clock.Now().UTC()
}

// Command はスラッシュコマンドを処理する。
func (d *Dispatcher) Command(ctx context.Context, raw *asobellv1.Command) (*asobellv1.Reply, error) {
	cmd, err := ParseCommand(raw)
	if err != nil {
		// 解釈できない入力はユーザーへの案内であって RPC の失敗ではないため、Reply として返す。
		return ephemeral(Error(msgUnknownCommand)), nil //nolint:nilerr // 上記の理由により握りつぶす。
	}

	ws, err := d.workspace(ctx, cmd.WorkspaceID, cmd.ExternalID)
	if err != nil {
		return d.replyForError(err), nil
	}

	if cmd.Sub == SubcommandHelp {
		return ephemeral(Help()), nil
	}

	if cmd.Sub.NeedsEventChannel() {
		return d.eventCommand(ctx, ws, cmd)
	}

	return d.workspaceCommand(ctx, ws, cmd)
}

// Action はボタン押下を処理する(docs/07-bot-ux.md §4)。
func (d *Dispatcher) Action(ctx context.Context, raw *asobellv1.Action) (*asobellv1.Reply, error) {
	action, eventID, err := ParseActionID(raw.GetActionId())
	if err != nil {
		// 古いメッセージのボタンなど、解釈できない action_id は案内で返す。
		return ephemeral(Error(msgUnknownCommand)), nil //nolint:nilerr // 上記の理由により握りつぶす。
	}

	ws, err := d.workspace(ctx, raw.GetWorkspace().GetWorkspaceId(), raw.GetWorkspace().GetExternalId())
	if err != nil {
		return d.replyForError(err), nil
	}

	res, err := d.svc.Participation.Apply(ctx, usecase.ParticipationInput{
		EventID:    eventID,
		ChatUserID: raw.GetUserId(),
		Action:     action,
	})
	if err != nil {
		return d.replyForError(err), nil
	}

	return ephemeral(ActionReply(
		res.Transition.Kind, res.Event.Channel.ChannelID, res.ExpiresAt, ws.Settings.PeekDuration,
	)), nil
}

// FormSubmit はモーダル送信を処理する。バリデーションだけ同期で行い、作成本体は非同期に回す。
func (d *Dispatcher) FormSubmit(ctx context.Context, sub *asobellv1.FormSubmission) (*asobellv1.Reply, error) {
	ws, err := d.workspace(ctx, sub.GetWorkspace().GetWorkspaceId(), sub.GetWorkspace().GetExternalId())
	if err != nil {
		return d.replyForError(err), nil
	}

	in, fieldErrs := ParseEventForm(sub.GetValues(), d.now(), ws.Settings)
	if len(fieldErrs) > 0 {
		return &asobellv1.Reply{FormErrors: fieldErrs}, nil
	}

	switch sub.GetFormId() {
	case FormEventNew:
		return d.submitNewEvent(ws, sub, in), nil
	case FormEventEdit:
		return d.submitEditEvent(ctx, sub, in)
	default:
		return ephemeral(Error(msgUnknownCommand)), nil
	}
}

func (d *Dispatcher) submitNewEvent(
	ws *domain.Workspace,
	sub *asobellv1.FormSubmission,
	in EventFormInput,
) *asobellv1.Reply {
	create := d.createInput(ws, sub.GetUserId(), sub.GetMetadata(), in)
	origin := sub.GetMetadata()
	userID := sub.GetUserId()

	d.svc.Async(func(ctx context.Context) {
		ctx, cancel := context.WithTimeout(ctx, defaultAsyncTimeout)
		defer cancel()

		ev, err := d.svc.Events.Create(ctx, create)
		if err != nil {
			d.notify(ctx, ws, origin, userID, d.messageForError(err))

			return
		}

		d.notify(ctx, ws, origin, userID, Created(ev))
	})

	return ephemeral(Error(msgEventCreating))
}

func (d *Dispatcher) submitEditEvent(
	ctx context.Context,
	sub *asobellv1.FormSubmission,
	in EventFormInput,
) (*asobellv1.Reply, error) {
	_, err := d.svc.Events.Update(ctx, usecase.UpdateEventInput{
		EventID: sub.GetMetadata(),
		Update:  in.Update(),
		Actor:   usecase.Actor{ChatUserID: sub.GetUserId()},
	})
	if err != nil {
		return d.replyForError(err), nil
	}

	return ephemeral(Error(msgEventUpdated)), nil
}

// notify は非同期処理の結果を本人へ知らせる。届かなくても再試行はしない。
func (d *Dispatcher) notify(
	ctx context.Context,
	ws *domain.Workspace,
	channelID, userID string,
	msg *asobellv1.Message,
) {
	if channelID == "" || msg == nil {
		return
	}

	ref := usecase.WorkspaceRef{WorkspaceID: ws.ID, ExternalID: ws.ExternalID}
	if err := d.svc.Provider.PostEphemeral(ctx, ref, channelID, userID, msg); err != nil {
		d.svc.Logger.WarnContext(ctx, "notify command result failed", "channel_id", channelID, "error", err)
	}
}

func (d *Dispatcher) workspace(ctx context.Context, workspaceID, externalID string) (*domain.Workspace, error) {
	return d.svc.Workspaces.Resolve(ctx, usecase.WorkspaceRef{WorkspaceID: workspaceID, ExternalID: externalID})
}

// replyForError はユースケースのエラーを ephemeral の定型文へ変換する(docs/07-bot-ux.md §3.4, §6)。
func (d *Dispatcher) replyForError(err error) *asobellv1.Reply {
	return ephemeral(d.messageForError(err))
}

func (d *Dispatcher) messageForError(err error) *asobellv1.Message {
	var invalid *domain.ValidationError

	switch {
	case errors.Is(err, domain.ErrForbidden):
		return Error(msgOrganizerOnly)
	case errors.Is(err, domain.ErrEventClosed):
		return Error(msgEventClosed)
	case errors.Is(err, domain.ErrOrganizerCannotLeave):
		return Error(msgOrganizerLeave)
	case errors.Is(err, domain.ErrNotFound):
		return Error(msgWorkspaceUnkown)
	case errors.As(err, &invalid):
		return Error(invalid.Error())
	default:
		return Error(msgProviderFailed)
	}
}

func ephemeral(msg *asobellv1.Message) *asobellv1.Reply {
	return &asobellv1.Reply{Message: msg, Visibility: asobellv1.Visibility_VISIBILITY_EPHEMERAL}
}

func openForm(form *asobellv1.Form) *asobellv1.Reply {
	return &asobellv1.Reply{OpenForm: form, Visibility: asobellv1.Visibility_VISIBILITY_EPHEMERAL}
}

// createInput はフォーム・コマンドの解析結果をイベント作成の入力に変換する。
func (d *Dispatcher) createInput(
	ws *domain.Workspace,
	userID, originChannelID string,
	in EventFormInput,
) usecase.CreateEventInput {
	return usecase.CreateEventInput{
		WorkspaceID:     ws.ID,
		Title:           in.Title,
		Description:     in.Description,
		StartsAt:        in.StartsAt,
		EndsAt:          in.EndsAt,
		Organizer:       domain.ChatUserRef{UserID: userID},
		OriginChannelID: originChannelID,
		ReminderPolicy:  in.ReminderPolicy,
		CreatedVia:      domain.CreatedViaChat,
		CreatedBy:       userID,
	}
}

// Update はフォームの解析結果を編集内容に変換する。フォームは全項目を送るため、空欄は「消す」を意味する。
func (in EventFormInput) Update() domain.EventUpdate {
	update := domain.EventUpdate{
		Title:          &in.Title,
		Description:    &in.Description,
		StartsAt:       &in.StartsAt,
		ReminderPolicy: in.ReminderPolicy,
	}

	if in.EndsAt != nil {
		update.EndsAt = in.EndsAt
	} else {
		update.ClearEndsAt = true
	}

	return update
}

// workspaceCommand はイベントチャンネルを必要としないサブコマンドを処理する。
func (d *Dispatcher) workspaceCommand(
	ctx context.Context,
	ws *domain.Workspace,
	cmd Command,
) (*asobellv1.Reply, error) {
	switch cmd.Sub {
	case SubcommandNew:
		return d.newEvent(ctx, ws, cmd)
	case SubcommandList:
		return d.listEvents(ctx, ws)
	case SubcommandRecruit:
		return d.recruit(ctx, ws, cmd)
	case SubcommandLogin:
		return d.login(ctx, ws, cmd)
	case SubcommandUnlink:
		return d.unlink(ctx, ws, cmd)
	case SubcommandHelp, SubcommandEdit, SubcommandRemind, SubcommandEnd,
		SubcommandCancel, SubcommandLeave, SubcommandInfo:
		return ephemeral(Help()), nil
	default:
		return ephemeral(Error(msgUnknownCommand)), nil
	}
}

// eventCommand はイベントチャンネル内でのみ使えるサブコマンドを処理する。
func (d *Dispatcher) eventCommand(
	ctx context.Context,
	ws *domain.Workspace,
	cmd Command,
) (*asobellv1.Reply, error) {
	ev, err := d.svc.Events.GetByChannel(ctx, cmd.ChannelID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return ephemeral(Error(msgNotEventChannel)), nil
		}

		return d.replyForError(err), nil
	}

	switch cmd.Sub {
	case SubcommandEdit:
		return d.editEvent(ctx, ws, ev, cmd)
	case SubcommandRemind:
		return d.setPace(ctx, ev, cmd)
	case SubcommandEnd:
		return d.closeEvent(ctx, ev, cmd, domain.EndReasonManual)
	case SubcommandCancel:
		return d.closeEvent(ctx, ev, cmd, domain.EndReasonCanceled)
	case SubcommandLeave:
		return d.leave(ctx, ev, cmd)
	case SubcommandInfo:
		return d.info(ctx, ev)
	case SubcommandNew, SubcommandList, SubcommandRecruit, SubcommandLogin,
		SubcommandUnlink, SubcommandHelp:
		return ephemeral(Help()), nil
	default:
		return ephemeral(Error(msgUnknownCommand)), nil
	}
}

// newEvent は引数が揃っていればそのまま作成し、揃っていなければフォームを開く(docs/07-bot-ux.md §2)。
func (d *Dispatcher) newEvent(ctx context.Context, ws *domain.Workspace, cmd Command) (*asobellv1.Reply, error) {
	if cmd.Arg(ArgTitle) == "" || cmd.Arg(ArgWhen) == "" {
		form, err := NewEventForm(d.now(), ws.Settings, cmd.ChannelID)
		if err != nil {
			return d.replyForError(err), nil
		}

		return openForm(form), nil
	}

	in, msg := d.parseEventArgs(ws, cmd)
	if msg != nil {
		return ephemeral(msg), nil
	}

	ev, err := d.svc.Events.Create(ctx, d.createInput(ws, cmd.UserID, cmd.ChannelID, in))
	if err != nil {
		return d.replyForError(err), nil
	}

	return ephemeral(Created(ev)), nil
}

// parseEventArgs はコマンド引数の日時・ペースを解析する。エラーは受理形式を含めて返す(docs/07 §1.2)。
func (d *Dispatcher) parseEventArgs(ws *domain.Workspace, cmd Command) (EventFormInput, *asobellv1.Message) {
	loc, err := ws.Settings.Location()
	if err != nil {
		loc = time.UTC
	}

	startsAt, err := ParseStartTime(cmd.Arg(ArgWhen), d.now(), loc, ws.Settings.DefaultStartTime)
	if err != nil {
		return EventFormInput{}, Error(msgInvalidWhen)
	}

	in := EventFormInput{
		Title:       strings.TrimSpace(cmd.Arg(ArgTitle)),
		Description: strings.TrimSpace(cmd.Arg(ArgDescription)),
		StartsAt:    startsAt,
	}

	if raw := cmd.Arg(ArgEnds); raw != "" {
		endsAt, endErr := ParseEndTime(raw, startsAt, loc)
		if endErr != nil {
			return EventFormInput{}, Error(msgInvalidEnds)
		}

		in.EndsAt = &endsAt
	}

	if raw := cmd.Arg(ArgRemind); raw != "" {
		policy, paceErr := ParseReminderPace(raw)
		if paceErr != nil {
			return EventFormInput{}, Error(msgInvalidPace)
		}

		in.ReminderPolicy = &policy
	}

	return in, nil
}

func (d *Dispatcher) editEvent(
	ctx context.Context,
	ws *domain.Workspace,
	ev *domain.Event,
	cmd Command,
) (*asobellv1.Reply, error) {
	if !hasEditArgs(cmd) {
		form, err := EditEventForm(ev, ws.Settings)
		if err != nil {
			return d.replyForError(err), nil
		}

		return openForm(form), nil
	}

	update, msg := d.parseEditArgs(ws, cmd)
	if msg != nil {
		return ephemeral(msg), nil
	}

	if _, err := d.svc.Events.Update(ctx, usecase.UpdateEventInput{
		EventID: ev.ID,
		Update:  update,
		Actor:   usecase.Actor{ChatUserID: cmd.UserID},
	}); err != nil {
		return d.replyForError(err), nil
	}

	return ephemeral(Error(msgEventUpdated)), nil
}

// parseEditArgs は指定された引数だけを変更内容にする。フォームと違い、未指定は「変えない」を意味する。
func (d *Dispatcher) parseEditArgs(ws *domain.Workspace, cmd Command) (domain.EventUpdate, *asobellv1.Message) {
	loc, err := ws.Settings.Location()
	if err != nil {
		loc = time.UTC
	}

	var update domain.EventUpdate

	if title := strings.TrimSpace(cmd.Arg(ArgTitle)); title != "" {
		update.Title = &title
	}

	if desc := strings.TrimSpace(cmd.Arg(ArgDescription)); desc != "" {
		update.Description = &desc
	}

	startsAt := d.now()

	if raw := cmd.Arg(ArgWhen); raw != "" {
		startsAt, err = ParseStartTime(raw, d.now(), loc, ws.Settings.DefaultStartTime)
		if err != nil {
			return domain.EventUpdate{}, Error(msgInvalidWhen)
		}

		update.StartsAt = &startsAt
	}

	if raw := cmd.Arg(ArgEnds); raw != "" {
		endsAt, endErr := ParseEndTime(raw, startsAt, loc)
		if endErr != nil {
			return domain.EventUpdate{}, Error(msgInvalidEnds)
		}

		update.EndsAt = &endsAt
	}

	return update, nil
}

func hasEditArgs(cmd Command) bool {
	for _, key := range []string{ArgTitle, ArgWhen, ArgEnds, ArgDescription} {
		if cmd.Arg(key) != "" {
			return true
		}
	}

	return false
}

func (d *Dispatcher) setPace(ctx context.Context, ev *domain.Event, cmd Command) (*asobellv1.Reply, error) {
	raw := cmd.Arg(ArgPace)
	if raw == "" {
		return ephemeral(Error(msgInvalidPace)), nil
	}

	policy, err := ParseReminderPace(raw)
	if err != nil {
		// 受理形式の案内を返すのが目的で、RPC としては成功させる。
		return ephemeral(Error(msgInvalidPace)), nil //nolint:nilerr // 上記の理由により握りつぶす。
	}

	if _, err = d.svc.Events.SetReminderPolicy(
		ctx, ev.ID, policy, usecase.Actor{ChatUserID: cmd.UserID},
	); err != nil {
		return d.replyForError(err), nil
	}

	return ephemeral(Error("リマインドを「" + FormatReminderPace(policy) + "」に設定しました")), nil
}

func (d *Dispatcher) closeEvent(
	ctx context.Context,
	ev *domain.Event,
	cmd Command,
	reason domain.EndReason,
) (*asobellv1.Reply, error) {
	closed, err := d.svc.Events.Close(ctx, usecase.CloseEventInput{
		EventID: ev.ID,
		Reason:  reason,
		Actor:   usecase.Actor{ChatUserID: cmd.UserID},
	})
	if err != nil {
		return d.replyForError(err), nil
	}

	if closed.Status == domain.EventStatusCanceled {
		return ephemeral(Error("イベントを中止しました")), nil
	}

	return ephemeral(Error("イベントを終了しました")), nil
}

func (d *Dispatcher) leave(ctx context.Context, ev *domain.Event, cmd Command) (*asobellv1.Reply, error) {
	if _, err := d.svc.Participation.Leave(ctx, ev.ID, cmd.UserID); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return ephemeral(Error(msgNotParticipant)), nil
		}

		return d.replyForError(err), nil
	}

	return ephemeral(Error("イベントから抜けました")), nil
}

func (d *Dispatcher) info(ctx context.Context, ev *domain.Event) (*asobellv1.Reply, error) {
	participants, peekers, err := d.svc.Participation.ActiveMembers(ctx, ev.ID)
	if err != nil {
		return d.replyForError(err), nil
	}

	return ephemeral(EventInfo(ev, participants, peekers)), nil
}

func (d *Dispatcher) listEvents(ctx context.Context, ws *domain.Workspace) (*asobellv1.Reply, error) {
	now := d.now()

	page, err := d.svc.Events.List(ctx, usecase.EventFilter{
		WorkspaceID: ws.ID,
		Statuses:    []domain.EventStatus{domain.EventStatusOpen},
		StartsAfter: &now,
	})
	if err != nil {
		return d.replyForError(err), nil
	}

	return ephemeral(EventList(page.Events, now)), nil
}

func (d *Dispatcher) recruit(ctx context.Context, ws *domain.Workspace, cmd Command) (*asobellv1.Reply, error) {
	switch strings.ToLower(strings.TrimSpace(cmd.Arg(ArgAction))) {
	case RecruitActionSet:
		if _, err := d.svc.Workspaces.SetRecruitChannel(ctx, ws.ID, cmd.ChannelID); err != nil {
			return d.replyForError(err), nil
		}

		return ephemeral(Error("このチャンネルを募集チャンネルに設定しました")), nil
	case RecruitActionClear:
		if _, err := d.svc.Workspaces.ClearRecruitChannel(ctx, ws.ID); err != nil {
			return d.replyForError(err), nil
		}

		return ephemeral(Error(msgRecruitCleared)), nil
	default:
		return ephemeral(Error("`/asobell recruit set` または `/asobell recruit clear` を指定してください")), nil
	}
}

// login は連携 URL を本人にだけ返す(docs/07-bot-ux.md §5)。トークンは ephemeral 以外へ出さない。
func (d *Dispatcher) login(ctx context.Context, ws *domain.Workspace, cmd Command) (*asobellv1.Reply, error) {
	if user, err := d.svc.Identities.ResolveConsoleUser(ctx, ws.ID, cmd.UserID); err == nil {
		return ephemeral(AlreadyLinked(user.Email, d.svc.ConsoleURL)), nil
	} else if !errors.Is(err, domain.ErrNotFound) {
		return d.replyForError(err), nil
	}

	token, err := d.svc.Identities.IssueLinkToken(ctx, ws.ID, cmd.UserID, "")
	if err != nil {
		return d.replyForError(err), nil
	}

	return ephemeral(LinkInvitation(d.linkURL(token))), nil
}

func (d *Dispatcher) linkURL(token string) string {
	return strings.TrimRight(d.svc.ConsoleURL, "/") + "/link/" + token
}

func (d *Dispatcher) unlink(ctx context.Context, ws *domain.Workspace, cmd Command) (*asobellv1.Reply, error) {
	if err := d.svc.Identities.UnlinkChatUser(ctx, ws.ID, cmd.UserID); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return ephemeral(Error(msgNotLinked)), nil
		}

		return d.replyForError(err), nil
	}

	return ephemeral(Error(msgUnlinked)), nil
}
