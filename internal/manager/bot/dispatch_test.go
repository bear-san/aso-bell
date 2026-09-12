package bot_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/manager/bot"
	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/manager/memstore"
	"github.com/bear-san/aso-bell/internal/manager/providerclient"
	"github.com/bear-san/aso-bell/internal/manager/testutil"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
	"github.com/bear-san/aso-bell/internal/provider/fake"
)

const (
	organizer = "U0123"
	guest     = "U0GUEST"
	origin    = "C0ORIGIN"
	tenDays   = 10 * 24 * time.Hour
)

type stubStatus struct{}

func (stubStatus) State() domain.ProviderState {
	return domain.ProviderState{Kind: domain.ProviderKindSlack, Connected: true, Status: domain.ProviderStatusOnline}
}

type dispatchHarness struct {
	repo       *memstore.Store
	fake       *fake.ProviderServer
	clock      *testutil.FakeClock
	dispatcher *bot.Dispatcher
	events     *usecase.EventService
	parts      *usecase.ParticipationService
	workspace  *domain.Workspace
}

func dispatchNow() time.Time {
	return time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC)
}

func newDispatchHarness(t *testing.T) *dispatchHarness {
	t.Helper()

	repo := memstore.New()
	server := fake.New()

	conn, stop, err := fake.Serve(server)
	require.NoError(t, err)
	t.Cleanup(stop)

	clock := testutil.NewFakeClock(dispatchNow())
	provider := providerclient.New(conn)
	deps := usecase.Deps{
		Repo:     repo,
		Provider: provider,
		Status:   stubStatus{},
		Messages: bot.Renderer{},
		Clock:    clock,
		Logger:   slog.New(slog.DiscardHandler),
	}

	h := &dispatchHarness{
		repo:   repo,
		fake:   server,
		clock:  clock,
		events: usecase.NewEventService(deps),
		parts:  usecase.NewParticipationService(deps),
	}

	h.dispatcher = bot.NewDispatcher(bot.Services{
		Events:        h.events,
		Participation: h.parts,
		Workspaces:    usecase.NewWorkspaceService(deps),
		Identities:    usecase.NewIdentityService(deps),
		Provider:      provider,
		Clock:         clock,
		Logger:        slog.New(slog.DiscardHandler),
		ConsoleURL:    "https://console.example.com",
		// テストでは作成完了まで待てるよう同期実行にする。
		Async: func(fn func(context.Context)) { fn(t.Context()) },
	})

	ws, err := repo.Workspaces().Upsert(t.Context(), &domain.Workspace{
		Provider:   domain.ProviderKindSlack,
		ExternalID: "T0123",
		Name:       "あそび部",
		Settings:   domain.DefaultWorkspaceSettings(),
	}, dispatchNow())
	require.NoError(t, err)

	h.workspace = ws

	return h
}

func (h *dispatchHarness) command(t *testing.T, userID, channelID, rawText string) *asobellv1.Reply {
	t.Helper()

	reply, err := h.dispatcher.Command(t.Context(), &asobellv1.Command{
		Workspace: &asobellv1.WorkspaceRef{ExternalId: "T0123"},
		ChannelId: channelID,
		UserId:    userID,
		RawText:   rawText,
	})
	require.NoError(t, err)

	return reply
}

func (h *dispatchHarness) createEvent(t *testing.T) *domain.Event {
	t.Helper()

	ev, err := h.events.Create(t.Context(), usecase.CreateEventInput{
		WorkspaceID:     h.workspace.ID,
		Title:           "ボドゲ会",
		StartsAt:        dispatchNow().Add(tenDays),
		Organizer:       domain.ChatUserRef{UserID: organizer},
		OriginChannelID: origin,
		CreatedVia:      domain.CreatedViaChat,
		CreatedBy:       organizer,
	})
	require.NoError(t, err)

	return ev
}

func TestDispatchHelp(t *testing.T) {
	t.Parallel()

	h := newDispatchHarness(t)
	reply := h.command(t, organizer, origin, "help")

	assert.Equal(t, asobellv1.Visibility_VISIBILITY_EPHEMERAL, reply.GetVisibility())
	assert.Contains(t, reply.GetMessage().GetText(), "あそベル の使い方")
}

func TestDispatchUnknownCommand(t *testing.T) {
	t.Parallel()

	h := newDispatchHarness(t)
	reply := h.command(t, organizer, origin, "teleport")

	assert.Contains(t, reply.GetMessage().GetText(), "知らないコマンド")
}

func TestDispatchNewOpensForm(t *testing.T) {
	t.Parallel()

	h := newDispatchHarness(t)
	reply := h.command(t, organizer, origin, "new")

	require.NotNil(t, reply.GetOpenForm())
	assert.Equal(t, bot.FormEventNew, reply.GetOpenForm().GetFormId())
	assert.Equal(t, origin, reply.GetOpenForm().GetMetadata())
}

func TestDispatchEventCommandOutsideEventChannel(t *testing.T) {
	t.Parallel()

	h := newDispatchHarness(t)
	reply := h.command(t, organizer, origin, "info")

	assert.Contains(t, reply.GetMessage().GetText(), "イベントチャンネルで実行してください")
}

func TestDispatchInfoListsParticipants(t *testing.T) {
	t.Parallel()

	h := newDispatchHarness(t)
	ev := h.createEvent(t)

	_, err := h.parts.Apply(t.Context(), usecase.ParticipationInput{
		EventID: ev.ID, ChatUserID: guest, Action: domain.ActionPeek,
	})
	require.NoError(t, err)

	reply := h.command(t, guest, ev.Channel.ChannelID, "info")

	text := reply.GetMessage().GetText()
	assert.Contains(t, text, "ボドゲ会")
	assert.Contains(t, text, "🙋 参加 1 人")
	assert.Contains(t, text, "👀 チラ見 1 人")
}

func TestDispatchListShowsOpenEvents(t *testing.T) {
	t.Parallel()

	h := newDispatchHarness(t)

	reply := h.command(t, guest, origin, "list")
	assert.Contains(t, reply.GetMessage().GetText(), "募集中のイベントはありません")

	h.createEvent(t)

	reply = h.command(t, guest, origin, "list")
	assert.Contains(t, reply.GetMessage().GetText(), "ボドゲ会")
}

func TestDispatchOrganizerOnlyCommands(t *testing.T) {
	t.Parallel()

	tests := []string{"end", "cancel", "remind off"}

	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()

			h := newDispatchHarness(t)
			ev := h.createEvent(t)

			reply := h.command(t, guest, ev.Channel.ChannelID, raw)
			assert.Contains(t, reply.GetMessage().GetText(), "主催者のみ")

			stored, err := h.repo.Events().Get(t.Context(), ev.ID)
			require.NoError(t, err)
			assert.Equal(t, domain.EventStatusOpen, stored.Status)
		})
	}
}

func TestDispatchEndAndCancel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw    string
		want   domain.EventStatus
		reason domain.EndReason
	}{
		{"end", domain.EventStatusEnded, domain.EndReasonManual},
		{"cancel", domain.EventStatusCanceled, domain.EndReasonCanceled},
	}

	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()

			h := newDispatchHarness(t)
			ev := h.createEvent(t)

			h.command(t, organizer, ev.Channel.ChannelID, tc.raw)

			stored, err := h.repo.Events().Get(t.Context(), ev.ID)
			require.NoError(t, err)
			assert.Equal(t, tc.want, stored.Status)
			assert.Equal(t, tc.reason, stored.EndReason)
		})
	}
}

func TestDispatchRemindSetsPace(t *testing.T) {
	t.Parallel()

	h := newDispatchHarness(t)
	ev := h.createEvent(t)

	reply := h.command(t, organizer, ev.Channel.ChannelID, "remind 7d,1d")

	assert.Contains(t, reply.GetMessage().GetText(), "リマインドを")

	stored, err := h.repo.Events().Get(t.Context(), ev.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReminderModeOffsets, stored.ReminderPolicy.Mode)
	assert.Len(t, stored.ReminderPolicy.Offsets, 2)
}

func TestDispatchRemindRejectsInvalidPace(t *testing.T) {
	t.Parallel()

	h := newDispatchHarness(t)
	ev := h.createEvent(t)

	reply := h.command(t, organizer, ev.Channel.ChannelID, "remind そのうち")

	assert.Contains(t, reply.GetMessage().GetText(), "リマインドを解釈できません")
}

func TestDispatchLeave(t *testing.T) {
	t.Parallel()

	h := newDispatchHarness(t)
	ev := h.createEvent(t)

	reply := h.command(t, guest, ev.Channel.ChannelID, "leave")
	assert.Contains(t, reply.GetMessage().GetText(), "参加していません")

	_, err := h.parts.Apply(t.Context(), usecase.ParticipationInput{
		EventID: ev.ID, ChatUserID: guest, Action: domain.ActionJoin,
	})
	require.NoError(t, err)

	reply = h.command(t, guest, ev.Channel.ChannelID, "leave")
	assert.Contains(t, reply.GetMessage().GetText(), "抜けました")

	reply = h.command(t, organizer, ev.Channel.ChannelID, "leave")
	assert.Contains(t, reply.GetMessage().GetText(), "主催者は抜けられません")
}

func TestDispatchRecruitSetAndClear(t *testing.T) {
	t.Parallel()

	h := newDispatchHarness(t)

	h.command(t, organizer, "C0RECRUIT", "recruit set")

	ws, err := h.repo.Workspaces().Get(t.Context(), h.workspace.ID)
	require.NoError(t, err)
	assert.Equal(t, "C0RECRUIT", ws.Settings.RecruitChannelID)

	h.command(t, organizer, "C0RECRUIT", "recruit clear")

	ws, err = h.repo.Workspaces().Get(t.Context(), h.workspace.ID)
	require.NoError(t, err)
	assert.Empty(t, ws.Settings.RecruitChannelID)
}

func TestDispatchLoginIssuesOneTimeURL(t *testing.T) {
	t.Parallel()

	h := newDispatchHarness(t)

	reply := h.command(t, guest, origin, "login")

	assert.Equal(t, asobellv1.Visibility_VISIBILITY_EPHEMERAL, reply.GetVisibility(),
		"連携 URL は本人にだけ返す")
	assert.Contains(t, reply.GetMessage().GetText(), "https://console.example.com/link/")
}

func TestDispatchUnlinkWithoutLink(t *testing.T) {
	t.Parallel()

	h := newDispatchHarness(t)

	reply := h.command(t, guest, origin, "unlink")

	assert.Contains(t, reply.GetMessage().GetText(), "連携していません")
}

func TestDispatchActionJoin(t *testing.T) {
	t.Parallel()

	h := newDispatchHarness(t)
	ev := h.createEvent(t)

	reply, err := h.dispatcher.Action(t.Context(), &asobellv1.Action{
		Workspace: &asobellv1.WorkspaceRef{ExternalId: "T0123"},
		ChannelId: origin,
		UserId:    guest,
		ActionId:  bot.ActionID(domain.ActionJoin, ev.ID),
	})
	require.NoError(t, err)

	assert.Contains(t, reply.GetMessage().GetText(), "参加しました")

	channel, ok := h.fake.Channel(ev.Channel.ChannelID)
	require.True(t, ok)
	assert.Contains(t, channel.Members, guest)
}

func TestDispatchActionOnClosedEvent(t *testing.T) {
	t.Parallel()

	h := newDispatchHarness(t)
	ev := h.createEvent(t)

	_, err := h.events.Close(t.Context(), usecase.CloseEventInput{
		EventID: ev.ID,
		Reason:  domain.EndReasonManual,
		Actor:   usecase.Actor{Console: true},
	})
	require.NoError(t, err)

	reply, err := h.dispatcher.Action(t.Context(), &asobellv1.Action{
		Workspace: &asobellv1.WorkspaceRef{ExternalId: "T0123"},
		UserId:    guest,
		ActionId:  bot.ActionID(domain.ActionJoin, ev.ID),
	})
	require.NoError(t, err)

	assert.Contains(t, reply.GetMessage().GetText(), "終了しています")
}

func TestDispatchFormSubmitCreatesEvent(t *testing.T) {
	t.Parallel()

	h := newDispatchHarness(t)

	reply, err := h.dispatcher.FormSubmit(t.Context(), &asobellv1.FormSubmission{
		Workspace: &asobellv1.WorkspaceRef{ExternalId: "T0123"},
		UserId:    organizer,
		FormId:    bot.FormEventNew,
		Metadata:  origin,
		Values: map[string]string{
			bot.FieldTitle:  "ボドゲ会",
			bot.FieldDate:   "2026-09-20",
			bot.FieldTime:   "19:00",
			bot.FieldRemind: bot.RemindPresetStandard,
		},
	})
	require.NoError(t, err)
	assert.Empty(t, reply.GetFormErrors())

	page, err := h.repo.Events().List(t.Context(), usecase.EventFilter{WorkspaceID: h.workspace.ID})
	require.NoError(t, err)
	require.Len(t, page.Events, 1)
	assert.Equal(t, "ボドゲ会", page.Events[0].Title)

	// 作成結果は実行チャンネルへ本人だけに知らせる。
	posts := h.fake.PostsIn(origin)
	require.NotEmpty(t, posts)
	assert.True(t, posts[len(posts)-1].Ephemeral)
}

func TestDispatchFormSubmitReturnsFieldErrors(t *testing.T) {
	t.Parallel()

	h := newDispatchHarness(t)

	reply, err := h.dispatcher.FormSubmit(t.Context(), &asobellv1.FormSubmission{
		Workspace: &asobellv1.WorkspaceRef{ExternalId: "T0123"},
		UserId:    organizer,
		FormId:    bot.FormEventNew,
		Metadata:  origin,
		Values:    map[string]string{bot.FieldDate: "2026-09-20"},
	})
	require.NoError(t, err)

	assert.Contains(t, reply.GetFormErrors(), bot.FieldTitle)

	page, err := h.repo.Events().List(t.Context(), usecase.EventFilter{WorkspaceID: h.workspace.ID})
	require.NoError(t, err)
	assert.Empty(t, page.Events)
}

func TestDispatchFormSubmitEditUpdatesEvent(t *testing.T) {
	t.Parallel()

	h := newDispatchHarness(t)
	ev := h.createEvent(t)

	reply, err := h.dispatcher.FormSubmit(t.Context(), &asobellv1.FormSubmission{
		Workspace: &asobellv1.WorkspaceRef{ExternalId: "T0123"},
		UserId:    organizer,
		FormId:    bot.FormEventEdit,
		Metadata:  ev.ID,
		Values: map[string]string{
			bot.FieldTitle:  "ボドゲ会(改)",
			bot.FieldDate:   "2026-09-25",
			bot.FieldTime:   "20:00",
			bot.FieldRemind: bot.RemindPresetCustom,
		},
	})
	require.NoError(t, err)
	assert.Empty(t, reply.GetFormErrors())

	stored, err := h.repo.Events().Get(t.Context(), ev.ID)
	require.NoError(t, err)
	assert.Equal(t, "ボドゲ会(改)", stored.Title)
}

func TestDispatchUnknownWorkspace(t *testing.T) {
	t.Parallel()

	h := newDispatchHarness(t)

	reply, err := h.dispatcher.Command(t.Context(), &asobellv1.Command{
		Workspace: &asobellv1.WorkspaceRef{ExternalId: "T0UNKNOWN"},
		ChannelId: origin,
		UserId:    organizer,
		RawText:   "list",
	})
	require.NoError(t, err)

	assert.Contains(t, reply.GetMessage().GetText(), "同期されていません")
}
