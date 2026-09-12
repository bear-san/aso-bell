package bot_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/manager/bot"
)

func slackCommand(text string) *asobellv1.Command {
	return &asobellv1.Command{
		Workspace: &asobellv1.WorkspaceRef{WorkspaceId: "66e0a1b2c3d4e5f607182930", ExternalId: "T0001"},
		ChannelId: "C001",
		UserId:    "U001",
		RawText:   text,
	}
}

func TestParseCommandFromSlackText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		text     string
		wantSub  bot.Subcommand
		wantArgs map[string]string
	}{
		{"サブコマンドのみ", "new", bot.SubcommandNew, map[string]string{}},
		{"空はヘルプ", "", bot.SubcommandHelp, map[string]string{}},
		{"コマンド名付き", "/asobell info", bot.SubcommandInfo, map[string]string{}},
		{"大文字", "END", bot.SubcommandEnd, map[string]string{}},
		{"リマインド", "remind 7d,1d,3h", bot.SubcommandRemind, map[string]string{"pace": "7d,1d,3h"}},
		{"リマインド間隔", "remind every 2d 20:00", bot.SubcommandRemind, map[string]string{"pace": "every 2d 20:00"}},
		{"募集チャンネル設定", "recruit set", bot.SubcommandRecruit, map[string]string{"action": "set"}},
		{"募集チャンネル解除", "recruit CLEAR", bot.SubcommandRecruit, map[string]string{"action": "clear"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := bot.ParseCommand(slackCommand(tc.text))
			require.NoError(t, err)

			assert.Equal(t, tc.wantSub, got.Sub)
			assert.Equal(t, tc.wantArgs, got.Args)
			assert.Equal(t, "C001", got.ChannelID)
			assert.Equal(t, "U001", got.UserID)
			assert.Equal(t, "66e0a1b2c3d4e5f607182930", got.WorkspaceID)
			assert.Equal(t, "T0001", got.ExternalID)
		})
	}
}

func TestParseCommandFromDiscordOptions(t *testing.T) {
	t.Parallel()

	cmd := slackCommand("")
	cmd.Name = "new"
	cmd.Args = map[string]string{
		"Title":       " ボドゲ会 ",
		"when":        "9/20 19:00",
		"description": "",
	}

	got, err := bot.ParseCommand(cmd)
	require.NoError(t, err)

	assert.Equal(t, bot.SubcommandNew, got.Sub)
	assert.Equal(t, "ボドゲ会", got.Arg(bot.ArgTitle))
	assert.Equal(t, "9/20 19:00", got.Arg(bot.ArgWhen))
	assert.Empty(t, got.Arg(bot.ArgDescription))
}

func TestParseCommandKeepsExplicitArgs(t *testing.T) {
	t.Parallel()

	cmd := slackCommand("remind off")
	cmd.Args = map[string]string{"pace": "7d"}

	got, err := bot.ParseCommand(cmd)
	require.NoError(t, err)

	assert.Equal(t, "7d", got.Arg(bot.ArgPace))
}

func TestParseCommandRejectsUnknownSubcommand(t *testing.T) {
	t.Parallel()

	_, err := bot.ParseCommand(slackCommand("explode now"))

	require.ErrorIs(t, err, bot.ErrUnknownCommand)
}

func TestSubcommandScopes(t *testing.T) {
	t.Parallel()

	assert.True(t, bot.SubcommandEdit.NeedsEventChannel())
	assert.True(t, bot.SubcommandLeave.NeedsEventChannel())
	assert.False(t, bot.SubcommandNew.NeedsEventChannel())
	assert.False(t, bot.SubcommandList.NeedsEventChannel())

	assert.True(t, bot.SubcommandCancel.OrganizerOnly())
	assert.True(t, bot.SubcommandRemind.OrganizerOnly())
	assert.False(t, bot.SubcommandLeave.OrganizerOnly())
	assert.False(t, bot.SubcommandInfo.OrganizerOnly())

	assert.False(t, bot.Subcommand("explode").Valid())
	assert.False(t, bot.Subcommand("explode").NeedsEventChannel())
	assert.False(t, bot.Subcommand("explode").OrganizerOnly())
}
