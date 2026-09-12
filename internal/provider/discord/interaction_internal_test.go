package discord

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/provider/adapter"
)

// recordingSink は受信イベントを記録するだけの InboundSink。
type recordingSink struct {
	mu         sync.Mutex
	commands   []adapter.Command
	actions    []adapter.Action
	workspaces []adapter.WorkspaceInfo
	states     []bool
}

func (s *recordingSink) OnCommand(_ context.Context, cmd adapter.Command) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.commands = append(s.commands, cmd)
}

func (s *recordingSink) OnAction(_ context.Context, act adapter.Action) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.actions = append(s.actions, act)
}

func (s *recordingSink) OnFormSubmit(context.Context, adapter.FormSubmission) {}

func (s *recordingSink) OnWorkspaceDiscovered(_ context.Context, ws adapter.WorkspaceInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.workspaces = append(s.workspaces, ws)
}

func (s *recordingSink) OnConnectionStateChanged(connected bool, _ error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.states = append(s.states, connected)
}

// attach は Gateway を開かずに sink だけを結びつける。REST と受信変換だけを試すため。
func attach(t *testing.T, a *Adapter) *recordingSink {
	t.Helper()

	sink := &recordingSink{}

	a.mu.Lock()
	a.ctx = t.Context()
	a.sink = sink
	a.mu.Unlock()

	return sink
}

func commandInteraction(
	sub string,
	options ...*discordgo.ApplicationCommandInteractionDataOption,
) *discordgo.InteractionCreate {
	return &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		Type:      discordgo.InteractionApplicationCommand,
		GuildID:   guildID,
		ChannelID: channelID,
		Member:    &discordgo.Member{User: &discordgo.User{ID: organizer}},
		Data: discordgo.ApplicationCommandInteractionData{
			Name: "asobell",
			Options: []*discordgo.ApplicationCommandInteractionDataOption{{
				Type:    discordgo.ApplicationCommandOptionSubCommand,
				Name:    sub,
				Options: options,
			}},
		},
	}}
}

func stringOption(name, value string) *discordgo.ApplicationCommandInteractionDataOption {
	return &discordgo.ApplicationCommandInteractionDataOption{
		Type:  discordgo.ApplicationCommandOptionString,
		Name:  name,
		Value: value,
	}
}

func TestInteractionBecomesCommand(t *testing.T) {
	t.Parallel()

	a, _ := newTestAdapter(t)
	sink := attach(t, a)

	a.onInteraction(a.session, commandInteraction(subNew,
		stringOption(optTitle, "ボドゲ会"),
		stringOption(optWhen, "9/20 19:00"),
		stringOption(optEnds, "  "),
	))

	require.Len(t, sink.commands, 1)

	payload := sink.commands[0].Payload
	assert.Equal(t, subNew, payload.GetName())
	assert.Equal(t, guildID, payload.GetWorkspace().GetExternalId())
	assert.Equal(t, channelID, payload.GetChannelId())
	assert.Equal(t, organizer, payload.GetUserId())
	assert.Equal(t, map[string]string{optTitle: "ボドゲ会", optWhen: "9/20 19:00"}, payload.GetArgs(),
		"空のオプションは送らない")
	assert.Contains(t, payload.GetRawText(), "title:ボドゲ会")
	assert.NotNil(t, payload.GetReceivedAt())
}

func TestInteractionBecomesAction(t *testing.T) {
	t.Parallel()

	a, _ := newTestAdapter(t)
	sink := attach(t, a)

	a.onInteraction(a.session, &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		Type:      discordgo.InteractionMessageComponent,
		GuildID:   guildID,
		ChannelID: channelID,
		Member:    &discordgo.Member{User: &discordgo.User{ID: guest}},
		Message:   &discordgo.Message{ID: messageID},
		Data:      discordgo.MessageComponentInteractionData{CustomID: "asobell:join:1"},
	}})

	require.Len(t, sink.actions, 1)

	payload := sink.actions[0].Payload
	assert.Equal(t, "asobell:join:1", payload.GetActionId())
	assert.Equal(t, messageID, payload.GetMessageId())
	assert.Equal(t, guest, payload.GetUserId())
}

func TestInteractionIsIgnoredBeforeConnect(t *testing.T) {
	t.Parallel()

	a, _ := newTestAdapter(t)

	// sink が無い状態でも panic せず、何も起きない。
	a.onInteraction(a.session, commandInteraction(subList))
}

func TestResponderDefersThenEditsForCommand(t *testing.T) {
	t.Parallel()

	a, s := newTestAdapter(t)
	interaction := commandInteraction(subList).Interaction
	interaction.ID = "800000000000000008"
	interaction.Token = "token"
	interaction.AppID = appID

	s.handle("POST /interactions/"+interaction.ID+"/"+interaction.Token+"/callback", http.StatusNoContent, nil)
	s.handle("PATCH /webhooks/"+appID+"/"+interaction.Token+"/messages/@original", http.StatusOK,
		map[string]any{"id": messageID, "channel_id": channelID})

	r := &responder{
		session:       a.session,
		interaction:   interaction,
		deferType:     discordgo.InteractionResponseDeferredChannelMessageWithSource,
		editsDeferred: true,
	}

	require.NoError(t, r.Ack(t.Context(), nil))
	require.NoError(t, r.Ack(t.Context(), nil), "2 回目は何もしない")
	require.NoError(t, r.Followup(t.Context(), &asobellv1.Reply{Message: &asobellv1.Message{Text: "一覧"}}))

	callbacks := s.requestsTo(http.MethodPost, "/callback")
	require.Len(t, callbacks, 1, "Ack は 1 度だけ")
	assert.InEpsilon(t, float64(discordgo.InteractionResponseDeferredChannelMessageWithSource),
		callbacks[0].Body["type"], 0.0001)

	edits := s.requestsTo(http.MethodPatch, "/messages/@original")
	require.Len(t, edits, 1)
	assert.Equal(t, "一覧", edits[0].Body["content"])
}

func TestResponderCannotOpenForm(t *testing.T) {
	t.Parallel()

	a, _ := newTestAdapter(t)
	r := &responder{session: a.session, interaction: commandInteraction(subNew).Interaction}

	require.ErrorIs(t, r.OpenForm(t.Context(), &asobellv1.Form{}), adapter.ErrUnsupported,
		"Discord のモーダルは日付入力を持てないため forms=false")
}

func TestGuildCreateRegistersCommandsAndNotifies(t *testing.T) {
	t.Parallel()

	a, s := newTestAdapter(t)
	sink := attach(t, a)
	s.handle("PUT /applications/"+appID+"/guilds/"+guildID+"/commands", http.StatusOK, []map[string]any{})

	guild := &discordgo.GuildCreate{Guild: &discordgo.Guild{ID: guildID, Name: "あそび部"}}
	a.onGuildCreate(a.session, guild)

	require.Len(t, sink.workspaces, 1)
	assert.Equal(t, guildID, sink.workspaces[0].ExternalID)
	assert.Equal(t, "あそび部", sink.workspaces[0].Name)
	assert.Len(t, s.requestsTo(http.MethodPut, "/commands"), 1)

	// 再接続で同じ Guild が届いても通知は重ねない。コマンド登録は一括上書きで冪等。
	a.onGuildCreate(a.session, guild)
	assert.Len(t, sink.workspaces, 1)
	assert.Len(t, s.requestsTo(http.MethodPut, "/commands"), 2)
}

func TestConnectionStateIsReported(t *testing.T) {
	t.Parallel()

	a, _ := newTestAdapter(t)
	sink := attach(t, a)

	a.onReady(a.session, &discordgo.Ready{Guilds: []*discordgo.Guild{{ID: guildID, Name: "あそび部"}}})
	assert.True(t, a.Connected())
	assert.Equal(t, []adapter.WorkspaceInfo{{ExternalID: guildID, Name: "あそび部"}}, a.Workspaces())

	a.onDisconnect(a.session, &discordgo.Disconnect{})
	assert.False(t, a.Connected())

	a.onResumed(a.session, &discordgo.Resumed{})
	assert.True(t, a.Connected())

	assert.Equal(t, []bool{true, false, true}, sink.states)
}

func TestResponderPostsFollowupForAction(t *testing.T) {
	t.Parallel()

	a, s := newTestAdapter(t)
	interaction := commandInteraction(subList).Interaction
	interaction.ID = "800000000000000009"
	interaction.Token = "token"
	interaction.AppID = appID

	s.handle("POST /interactions/"+interaction.ID+"/"+interaction.Token+"/callback", http.StatusNoContent, nil)
	s.handle("POST /webhooks/"+appID+"/"+interaction.Token, http.StatusOK,
		map[string]any{"id": messageID, "channel_id": channelID})

	r := &responder{
		session:     a.session,
		interaction: interaction,
		deferType:   discordgo.InteractionResponseDeferredMessageUpdate,
	}

	require.NoError(t, r.Ack(t.Context(), nil))
	require.NoError(t, r.Followup(t.Context(), &asobellv1.Reply{
		Message:    &asobellv1.Message{Text: "参加しました"},
		Visibility: asobellv1.Visibility_VISIBILITY_CHANNEL,
	}))

	callbacks := s.requestsTo(http.MethodPost, "/callback")
	require.Len(t, callbacks, 1)
	assert.InEpsilon(t, float64(discordgo.InteractionResponseDeferredMessageUpdate),
		callbacks[0].Body["type"], 0.0001, "ボタンは元メッセージの遅延更新で受領する")

	posts := s.requestsTo(http.MethodPost, "/"+interaction.Token)
	require.Len(t, posts, 1, "ボタンの返信は元メッセージを書き換えず追加投稿する")
	assert.Equal(t, "参加しました", posts[0].Body["content"])
	assert.Nil(t, posts[0].Body["flags"], "VISIBILITY_CHANNEL なら ephemeral フラグを付けない")
}

func TestUserIDFallsBackToDMUser(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		interaction *discordgo.Interaction
		want        string
	}{
		{
			"Guild 内",
			&discordgo.Interaction{Member: &discordgo.Member{User: &discordgo.User{ID: organizer}}},
			organizer,
		},
		{"DM", &discordgo.Interaction{User: &discordgo.User{ID: guest}}, guest},
		{"実行者不明", &discordgo.Interaction{}, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, userID(tc.interaction))
		})
	}
}
