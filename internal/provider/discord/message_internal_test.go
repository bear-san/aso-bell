package discord

import (
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/shared/markup"
)

func TestContentRendersMarkup(t *testing.T) {
	t.Parallel()

	got := content(&asobellv1.Message{Text: "参加者 " + markup.User(organizer)})

	assert.Equal(t, "参加者 <@"+organizer+">", got)
}

func TestContentTruncatesToDiscordLimit(t *testing.T) {
	t.Parallel()

	got := content(&asobellv1.Message{Text: strings.Repeat("あ", 3000)})

	assert.Len(t, []rune(got), 2000)
}

func TestComponentsSplitRowsAndTruncateLabels(t *testing.T) {
	t.Parallel()

	buttons := make([]*asobellv1.Button, 0, 7)
	for range 7 {
		buttons = append(buttons, &asobellv1.Button{ActionId: "asobell:join:1", Label: strings.Repeat("あ", 100)})
	}

	rows := components(buttons)
	require.Len(t, rows, 2, "1 行 5 個までで折り返す")

	first, ok := rows[0].(discordgo.ActionsRow)
	require.True(t, ok)
	assert.Len(t, first.Components, 5)

	button, ok := first.Components[0].(discordgo.Button)
	require.True(t, ok)
	assert.Len(t, []rune(button.Label), 80, "ラベルは 80 文字まで")
}

func TestComponentsMapStyles(t *testing.T) {
	t.Parallel()

	rows := components([]*asobellv1.Button{
		{ActionId: "a", Style: asobellv1.ButtonStyle_BUTTON_STYLE_PRIMARY},
		{ActionId: "b", Style: asobellv1.ButtonStyle_BUTTON_STYLE_DANGER},
		{ActionId: "c", Style: asobellv1.ButtonStyle_BUTTON_STYLE_DEFAULT},
	})

	row, ok := rows[0].(discordgo.ActionsRow)
	require.True(t, ok)

	styles := make([]discordgo.ButtonStyle, 0, len(row.Components))

	for _, component := range row.Components {
		button, isButton := component.(discordgo.Button)
		require.True(t, isButton)
		styles = append(styles, button.Style)
	}

	assert.Equal(t,
		[]discordgo.ButtonStyle{discordgo.PrimaryButton, discordgo.DangerButton, discordgo.SecondaryButton},
		styles)
}

func TestMessageSendSuppressesPreview(t *testing.T) {
	t.Parallel()

	send := messageSend(&asobellv1.Message{Text: "https://example.com", SuppressPreview: true})

	assert.Equal(t, discordgo.MessageFlagsSuppressEmbeds, send.Flags)
}

func TestMessageEditClearsButtons(t *testing.T) {
	t.Parallel()

	edit := messageEdit(channelID, messageID, &asobellv1.Message{Text: "終了しました"})

	require.NotNil(t, edit.Components)
	assert.Empty(t, *edit.Components, "ボタンを持たない Message で更新するとボタンが消える")
	assert.Equal(t, messageID, edit.ID)
}

func TestWebhookParamsAddsEphemeralFlag(t *testing.T) {
	t.Parallel()

	params := webhookParams(&asobellv1.Message{Text: "本人だけに見える"}, true)

	assert.NotZero(t, params.Flags&discordgo.MessageFlagsEphemeral)
}

func TestCommandDefinitionIsGuildOnly(t *testing.T) {
	t.Parallel()

	cmd := CommandDefinition("asobell")

	assert.Equal(t, "asobell", cmd.Name)
	require.NotNil(t, cmd.Contexts)
	assert.Equal(t, []discordgo.InteractionContextType{discordgo.InteractionContextGuild}, *cmd.Contexts)
	require.NotNil(t, cmd.IntegrationTypes)
	assert.Equal(t,
		[]discordgo.ApplicationIntegrationType{discordgo.ApplicationIntegrationGuildInstall},
		*cmd.IntegrationTypes)

	names := make([]string, 0, len(cmd.Options))
	for _, opt := range cmd.Options {
		assert.Equal(t, discordgo.ApplicationCommandOptionSubCommand, opt.Type)
		names = append(names, opt.Name)
	}

	assert.ElementsMatch(t, []string{
		subNew, subEdit, subRemind, subEnd, subCancel, subLeave,
		subInfo, subList, subRecruit, subLogin, subUnlink, subHelp,
	}, names)
}

func TestNewSubcommandRequiresTitleAndWhen(t *testing.T) {
	t.Parallel()

	var options []*discordgo.ApplicationCommandOption

	for _, opt := range CommandDefinition("asobell").Options {
		if opt.Name == subNew {
			options = opt.Options
		}
	}

	required := map[string]bool{}
	for _, opt := range options {
		required[opt.Name] = opt.Required
	}

	assert.True(t, required[optTitle])
	assert.True(t, required[optWhen])
	assert.False(t, required[optEnds])
	assert.False(t, required[optRemind])
}
