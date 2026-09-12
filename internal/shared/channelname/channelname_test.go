package channelname_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bear-san/aso-bell/internal/shared/channelname"
)

func TestBuild(t *testing.T) {
	t.Parallel()

	startsAt := time.Date(2026, time.September, 20, 19, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		title string
		want  string
	}{
		{name: "japanese title", title: "ボドゲ会 @渋谷", want: "ev-0920-ボドゲ会-渋谷"},
		{name: "ascii title", title: "Board Game Night", want: "ev-0920-board-game-night"},
		{name: "slash", title: "麻雀/ポーカー", want: "ev-0920-麻雀-ポーカー"},
		{name: "periods removed", title: "a.b.c", want: "ev-0920-a-b-c"},
		{name: "fullwidth normalized", title: "ＢＯＤＯＧＥ１", want: "ev-0920-bodoge1"},
		{name: "emoji dropped", title: "ボドゲ🎲会", want: "ev-0920-ボドゲ-会"},
		{name: "underscore kept", title: "board_game", want: "ev-0920-board_game"},
		{name: "repeated separators collapsed", title: "a   -  b", want: "ev-0920-a-b"},
		{name: "empty title", title: "   ", want: "ev-0920"},
		{name: "symbols only", title: "!!!", want: "ev-0920"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := channelname.Build("ev-", tc.title, startsAt, channelname.SlackMaxLen)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestBuildUsesGivenDate(t *testing.T) {
	t.Parallel()

	startsAt := time.Date(2026, time.January, 3, 9, 0, 0, 0, time.UTC)
	assert.Equal(t, "ev-0103-新年会", channelname.Build("ev-", "新年会", startsAt, channelname.SlackMaxLen))
}

func TestBuildTruncatesToLimit(t *testing.T) {
	t.Parallel()

	startsAt := time.Date(2026, time.September, 20, 19, 0, 0, 0, time.UTC)
	long := strings.Repeat("あ", 200)

	slack := channelname.Build("ev-", long, startsAt, channelname.SlackMaxLen)
	assert.Len(t, []rune(slack), channelname.SlackMaxLen)

	discord := channelname.Build("ev-", long, startsAt, channelname.DiscordMaxLen)
	assert.Len(t, []rune(discord), channelname.DiscordMaxLen)

	// Discord はアーカイブ時の `archived_` を足しても 100 文字に収まる。
	assert.LessOrEqual(t, len([]rune("archived_"+discord)), 100)
}

func TestBuildDoesNotEndWithSeparator(t *testing.T) {
	t.Parallel()

	startsAt := time.Date(2026, time.September, 20, 19, 0, 0, 0, time.UTC)
	title := strings.Repeat("あ", 70) + " しっぽ"

	got := channelname.Build("ev-", title, startsAt, channelname.SlackMaxLen)

	assert.False(t, strings.HasSuffix(got, "-"), "got %q", got)
}

func TestWithSuffix(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "ev-0920-bodoge-2", channelname.WithSuffix("ev-0920-bodoge", 2, channelname.SlackMaxLen))

	long := "ev-0920-" + strings.Repeat("あ", 100)
	got := channelname.WithSuffix(long, 3, channelname.SlackMaxLen)

	require.Len(t, []rune(got), channelname.SlackMaxLen)
	assert.True(t, strings.HasSuffix(got, "-3"), "got %q", got)
}

func TestSlug(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "bodoge", channelname.Slug("BoDoGe"))
	assert.Empty(t, channelname.Slug(""))
}
