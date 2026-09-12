package discord

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/provider/adapter"
)

// channelDoc はスタブが返すチャンネル。
func channelDoc(name string, overwrites ...map[string]any) map[string]any {
	return map[string]any{
		"id":                    channelID,
		"name":                  name,
		"type":                  int(discordgo.ChannelTypeGuildText),
		"guild_id":              guildID,
		"permission_overwrites": overwrites,
	}
}

func memberOverwrite(id string) map[string]any {
	return map[string]any{"id": id, "type": int(discordgo.PermissionOverwriteTypeMember), "allow": "0", "deny": "0"}
}

func TestCreatePrivateChannelHidesFromEveryone(t *testing.T) {
	t.Parallel()

	a, s := newTestAdapter(t)
	s.handle("POST /guilds/"+guildID+"/channels", http.StatusCreated, channelDoc("ev-0920-boardgame"))

	ch, err := a.CreatePrivateChannel(t.Context(), workspace(), adapter.CreateChannelInput{
		Name:      "ev-0920-boardgame",
		Topic:     "ボドゲ会",
		MemberIDs: []string{organizer},
	})
	require.NoError(t, err)
	assert.Equal(t, channelID, ch.ID)
	assert.True(t, ch.IsPrivate)

	requests := s.requestsTo(http.MethodPost, "/channels")
	require.Len(t, requests, 1)

	overwrites, ok := requests[0].Body["permission_overwrites"].([]any)
	require.True(t, ok)
	require.Len(t, overwrites, 3, "@everyone・Bot・主催者の 3 件")

	everyone, ok := overwrites[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, guildID, everyone["id"], "@everyone ロール ID は Guild ID と同じ")
}

func TestAddMemberSkipsWhenOverwriteExists(t *testing.T) {
	t.Parallel()

	a, s := newTestAdapter(t)
	s.handle("GET /channels/"+channelID, http.StatusOK, channelDoc("ev-0920", memberOverwrite(guest)))
	s.handle("PUT /channels/"+channelID+"/permissions/"+guest, http.StatusNoContent, nil)

	require.ErrorIs(t, a.AddMember(t.Context(), workspace(), channelID, guest), adapter.ErrAlreadyMember)
	assert.Empty(t, s.requestsTo(http.MethodPut, "/permissions/"+guest), "既にメンバーなら overwrite を触らない")
}

func TestAddMemberSetsOverwrite(t *testing.T) {
	t.Parallel()

	a, s := newTestAdapter(t)
	s.handle("GET /channels/"+channelID, http.StatusOK, channelDoc("ev-0920"))
	s.handle("PUT /channels/"+channelID+"/permissions/"+guest, http.StatusNoContent, nil)

	require.NoError(t, a.AddMember(t.Context(), workspace(), channelID, guest))

	requests := s.requestsTo(http.MethodPut, "/permissions/"+guest)
	require.Len(t, requests, 1)
	assert.Equal(t, strconv.FormatInt(memberPermissions, 10), requests[0].Body["allow"], "閲覧・投稿・履歴だけを許可する")
}

func TestRemoveMemberWithoutOverwrite(t *testing.T) {
	t.Parallel()

	a, s := newTestAdapter(t)
	s.handle("GET /channels/"+channelID, http.StatusOK, channelDoc("ev-0920"))

	require.ErrorIs(t, a.RemoveMember(t.Context(), workspace(), channelID, guest), adapter.ErrNotMember)
}

func TestRemoveMemberDeletesOverwrite(t *testing.T) {
	t.Parallel()

	a, s := newTestAdapter(t)
	s.handle("GET /channels/"+channelID, http.StatusOK, channelDoc("ev-0920", memberOverwrite(guest)))
	s.handle("DELETE /channels/"+channelID+"/permissions/"+guest, http.StatusNoContent, nil)

	require.NoError(t, a.RemoveMember(t.Context(), workspace(), channelID, guest))
	assert.Len(t, s.requestsTo(http.MethodDelete, "/permissions/"+guest), 1)
}

func TestArchiveChannelRenamesWithPrefix(t *testing.T) {
	t.Parallel()

	a, s := newTestAdapter(t)
	s.handle("GET /channels/"+channelID, http.StatusOK, channelDoc("ev-0920-boardgame"))
	s.handle("PATCH /channels/"+channelID, http.StatusOK, channelDoc("archived_ev-0920-boardgame"))

	ch, err := a.ArchiveChannel(t.Context(), workspace(), channelID, "800000000000000008")
	require.NoError(t, err)
	assert.True(t, ch.Archived)
	assert.Equal(t, "archived_ev-0920-boardgame", ch.Name)

	requests := s.requestsTo(http.MethodPatch, "/channels/"+channelID)
	require.Len(t, requests, 1)
	assert.Equal(t, "archived_ev-0920-boardgame", requests[0].Body["name"])
	assert.Equal(t, "800000000000000008", requests[0].Body["parent_id"])
}

func TestArchiveChannelIsIdempotent(t *testing.T) {
	t.Parallel()

	a, s := newTestAdapter(t)
	s.handle("GET /channels/"+channelID, http.StatusOK, channelDoc("archived_ev-0920"))

	ch, err := a.ArchiveChannel(t.Context(), workspace(), channelID, "")
	require.NoError(t, err)
	assert.True(t, ch.Archived)
	assert.Empty(t, s.requestsTo(http.MethodPatch, "/channels/"+channelID), "既にアーカイブ済みなら変更しない")
}

func TestPostMessageSendsContentAndButtons(t *testing.T) {
	t.Parallel()

	a, s := newTestAdapter(t)
	s.handle("POST /channels/"+channelID+"/messages", http.StatusOK, map[string]any{
		"id": messageID, "channel_id": channelID,
	})

	ref, err := a.PostMessage(t.Context(), workspace(), channelID, &asobellv1.Message{
		Text:    "ボドゲ会",
		Buttons: []*asobellv1.Button{{ActionId: "asobell:join:1", Label: "参加"}},
	})
	require.NoError(t, err)
	assert.Equal(t, messageID, ref.MessageID)

	requests := s.requestsTo(http.MethodPost, "/messages")
	require.Len(t, requests, 1)
	assert.Equal(t, "ボドゲ会", requests[0].Body["content"])
	assert.NotEmpty(t, requests[0].Body["components"])
}

func TestSendDirectMessageSuppressesPreview(t *testing.T) {
	t.Parallel()

	a, s := newTestAdapter(t)
	s.handle("POST /users/@me/channels", http.StatusOK, map[string]any{"id": "900000000000000009"})
	s.handle("POST /channels/900000000000000009/messages", http.StatusOK, map[string]any{
		"id": messageID, "channel_id": "900000000000000009",
	})

	_, err := a.SendDirectMessage(t.Context(), workspace(), guest, &asobellv1.Message{Text: "連携 URL"})
	require.NoError(t, err)

	requests := s.requestsTo(http.MethodPost, "/messages")
	require.Len(t, requests, 1)
	assert.InEpsilon(t, float64(discordgo.MessageFlagsSuppressEmbeds), requests[0].Body["flags"], 0.0001,
		"連携 URL の先読みを防ぐためプレビューを抑止する")
}

func TestPinUsesCurrentEndpoint(t *testing.T) {
	t.Parallel()

	a, s := newTestAdapter(t)
	s.handle("PUT /channels/"+channelID+"/messages/pins/"+messageID, http.StatusNoContent, nil)

	require.NoError(t, a.PinMessage(t.Context(), workspace(), adapter.MessageRef{
		ChannelID: channelID, MessageID: messageID,
	}))
	assert.Len(t, s.requestsTo(http.MethodPut, "/messages/pins/"+messageID), 1)
}

func TestResolveUserPrefersNickname(t *testing.T) {
	t.Parallel()

	a, s := newTestAdapter(t)
	s.handle("GET /guilds/"+guildID+"/members/"+organizer, http.StatusOK, map[string]any{
		"nick": "けんたろう",
		"user": map[string]any{"id": organizer, "username": "kentaro", "global_name": "Kentaro"},
	})

	user, err := a.ResolveUser(t.Context(), workspace(), organizer)
	require.NoError(t, err)
	assert.Equal(t, "けんたろう", user.DisplayName)
	assert.False(t, user.IsBot)
}

func TestListChannelsPagesTextChannels(t *testing.T) {
	t.Parallel()

	a, s := newTestAdapter(t)
	s.handle("GET /guilds/"+guildID+"/channels", http.StatusOK, []map[string]any{
		{"id": "1", "name": "a", "type": int(discordgo.ChannelTypeGuildText)},
		{"id": "2", "name": "b", "type": int(discordgo.ChannelTypeGuildText)},
		{"id": "3", "name": "voice", "type": int(discordgo.ChannelTypeGuildVoice)},
	})

	first, next, err := a.ListChannels(t.Context(), workspace(), "", 1)
	require.NoError(t, err)
	require.Len(t, first, 1)
	assert.Equal(t, "1", first[0].ID)
	assert.Equal(t, "1", next)

	rest, next, err := a.ListChannels(t.Context(), workspace(), next, 10)
	require.NoError(t, err)
	require.Len(t, rest, 1, "ボイスチャンネルは含めない")
	assert.Equal(t, "2", rest[0].ID)
	assert.Empty(t, next)
}

func TestRegisterCommandsOverwritesPerGuild(t *testing.T) {
	t.Parallel()

	a, s := newTestAdapter(t)
	s.handle("PUT /applications/"+appID+"/guilds/"+guildID+"/commands", http.StatusOK, []map[string]any{})

	require.NoError(t, a.registerCommands(t.Context(), guildID))
	assert.Len(t, s.requestsTo(http.MethodPut, "/guilds/"+guildID+"/commands"), 1)
}

func TestPostEphemeralIsUnsupported(t *testing.T) {
	t.Parallel()

	a, _ := newTestAdapter(t)

	err := a.PostEphemeral(t.Context(), workspace(), channelID, guest, &asobellv1.Message{Text: "hi"})
	require.ErrorIs(t, err, adapter.ErrUnsupported, "Runtime が DM へ切り替える")
}

func TestRESTErrorsBecomeSentinels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		body   any
		want   error
	}{
		{
			"権限不足",
			http.StatusForbidden,
			map[string]any{"code": 50013, "message": "Missing Permissions"},
			adapter.ErrPermission,
		},
		{
			"チャンネル不明",
			http.StatusNotFound,
			map[string]any{"code": codeUnknownChannel, "message": "Unknown Channel"},
			adapter.ErrNotFound,
		},
		{"サーバー障害", http.StatusBadGateway, nil, adapter.ErrUnavailable},
		{
			"ピン上限",
			http.StatusBadRequest,
			map[string]any{"code": codeMaxPins, "message": "Max pins"},
			adapter.ErrPinLimit,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			a, s := newTestAdapter(t)
			s.handle("GET /channels/"+channelID, tc.status, tc.body)

			_, err := a.ArchiveChannel(t.Context(), workspace(), channelID, "")
			require.ErrorIs(t, err, tc.want)
		})
	}
}

func TestDMBlockedIsDetected(t *testing.T) {
	t.Parallel()

	a, s := newTestAdapter(t)
	s.handle("POST /users/@me/channels", http.StatusForbidden, map[string]any{
		"code": codeCannotSendToUser, "message": "Cannot send messages to this user",
	})

	_, err := a.SendDirectMessage(t.Context(), workspace(), guest, &asobellv1.Message{Text: "連携 URL"})
	require.ErrorIs(t, err, adapter.ErrDMBlocked)
}

func TestConvertMapsRateLimitAndContext(t *testing.T) {
	t.Parallel()

	require.ErrorIs(t, convert(&discordgo.RateLimitError{}), adapter.ErrRateLimited)
	require.ErrorIs(t, convert(context.DeadlineExceeded), adapter.ErrUnavailable)
	require.NoError(t, convert(nil))
}

func TestChannelNameFitsDiscordRules(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "ev-0920-ボドゲ会", channelName("EV-0920 ボドゲ会"))
	assert.Equal(t, "event", channelName("  "))
	assert.Len(t, []rune(channelName(strings.Repeat("あ", 120))), channelNameLimit)
}

func TestKindAndCapabilitiesMatchDiscordLimits(t *testing.T) {
	t.Parallel()

	a, _ := newTestAdapter(t)

	assert.Equal(t, asobellv1.ProviderKind_PROVIDER_KIND_DISCORD, a.Kind())

	caps := a.Capabilities()
	assert.False(t, caps.GetForms(), "モーダルに日付入力が無いためフォームは使わない")
	assert.True(t, caps.GetEphemeral())
	assert.True(t, caps.GetDirectMessage())
	assert.Equal(t, botID, a.BotUserID())
}

func TestUpdateMessageEditsInPlace(t *testing.T) {
	t.Parallel()

	a, s := newTestAdapter(t)
	s.handle("PATCH /channels/"+channelID+"/messages/"+messageID, http.StatusOK, map[string]any{
		"id": messageID, "channel_id": channelID,
	})

	require.NoError(t, a.UpdateMessage(
		t.Context(),
		workspace(),
		adapter.MessageRef{ChannelID: channelID, MessageID: messageID},
		&asobellv1.Message{Text: "集合は 19:30 に変更"},
	))

	edits := s.requestsTo(http.MethodPatch, "/messages/"+messageID)
	require.Len(t, edits, 1)
	assert.Equal(t, "集合は 19:30 に変更", edits[0].Body["content"])
}

func TestUnpinUsesCurrentEndpoint(t *testing.T) {
	t.Parallel()

	a, s := newTestAdapter(t)
	s.handle("DELETE /channels/"+channelID+"/messages/pins/"+messageID, http.StatusNoContent, nil)

	require.NoError(t, a.UnpinMessage(t.Context(), workspace(), adapter.MessageRef{
		ChannelID: channelID, MessageID: messageID,
	}))
	assert.Len(t, s.requestsTo(http.MethodDelete, "/messages/pins/"+messageID), 1)
}

func TestHTTPStatusFallbackWithoutJSONCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		want   error
	}{
		{"認証失敗", http.StatusUnauthorized, adapter.ErrPermission},
		{"レート制限", http.StatusTooManyRequests, adapter.ErrRateLimited},
		{"存在しない", http.StatusNotFound, adapter.ErrNotFound},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			a, s := newTestAdapter(t)
			// JSON の code を持たないエラー本文でも、HTTP ステータスだけで種別を決められること。
			s.handle("DELETE /channels/"+channelID+"/messages/pins/"+messageID, tc.status,
				map[string]any{"message": "error"})

			err := a.UnpinMessage(t.Context(), workspace(), adapter.MessageRef{
				ChannelID: channelID, MessageID: messageID,
			})
			require.ErrorIs(t, err, tc.want)
		})
	}
}
