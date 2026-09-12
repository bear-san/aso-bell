package render_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/provider/render"
	"github.com/bear-san/aso-bell/internal/shared/markup"
)

func eventTime() time.Time {
	return time.Date(2026, time.September, 20, 10, 0, 0, 0, time.UTC)
}

func TestInlineConvertsTokens(t *testing.T) {
	t.Parallel()

	text := "参加者: " + markup.User("U0123") + " " + markup.Channel("C0ORIGIN") +
		" 開始 " + markup.Time(eventTime()) + " " + markup.Bold("必読") + " " +
		markup.Link("https://example.com/e/1", "詳細")

	tests := []struct {
		name   string
		syntax render.Syntax
		want   []string
	}{
		{
			"Discord",
			render.DiscordSyntax{},
			[]string{"<@U0123>", "<#C0ORIGIN>", "<t:1789898400:F>", "**必読**", "[詳細](https://example.com/e/1)"},
		},
		{
			"Slack",
			render.SlackSyntax{},
			[]string{"<@U0123>", "<#C0ORIGIN>", "<!date^1789898400^", "*必読*", "<https://example.com/e/1|詳細>"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := render.Inline(tc.syntax, text)
			for _, want := range tc.want {
				assert.Contains(t, got, want)
			}
		})
	}
}

func TestInlineEscapesUserInput(t *testing.T) {
	t.Parallel()

	assert.Equal(t, `a\*b\_c`, render.Inline(render.DiscordSyntax{}, "a*b_c"))
	assert.Equal(t, "a&lt;b&gt;c &amp; d", render.Inline(render.SlackSyntax{}, "a<b>c & d"))
}

func TestInlineKeepsBrokenTokensAsText(t *testing.T) {
	t.Parallel()

	// markup.Escape を通した入力はトークンとして成立しないため、メンションにならない。
	got := render.Inline(render.DiscordSyntax{}, markup.Escape("{{user:U0EVIL}}"))

	assert.NotContains(t, got, "<@U0EVIL>")
	assert.Contains(t, got, "U0EVIL")
}

func TestTextRendersSectionsAndFields(t *testing.T) {
	t.Parallel()

	msg := &asobellv1.Message{
		Text: "ボドゲ会",
		Sections: []*asobellv1.Section{{
			Title: "詳細",
			Body:  "初心者歓迎",
			Fields: []*asobellv1.Field{
				{Label: "場所", Value: "渋谷"},
				{Value: "ラベルなし"},
			},
		}},
	}

	got := render.Text(render.DiscordSyntax{}, msg, render.DiscordTextLimit)

	assert.Equal(t, "ボドゲ会\n\n**詳細**\n初心者歓迎\n**場所**: 渋谷\nラベルなし", got)
}

func TestTextTruncatesToLimit(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("あいうえお", 50)

	got := []rune(render.Text(render.DiscordSyntax{}, &asobellv1.Message{Text: long}, 20))

	assert.Len(t, got, 20)
	assert.Equal(t, '…', got[19])
}

func TestTextSkipsEmptyParts(t *testing.T) {
	t.Parallel()

	msg := &asobellv1.Message{Sections: []*asobellv1.Section{{}, {Body: "本文だけ"}}}

	assert.Equal(t, "本文だけ", render.Text(render.SlackSyntax{}, msg, render.SlackTextLimit))
}
