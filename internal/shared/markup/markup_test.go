package markup_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bear-san/aso-bell/internal/shared/markup"
)

func TestBuilders(t *testing.T) {
	at := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)

	assert.Equal(t, "{{user:U123}}", markup.User("U123"))
	assert.Equal(t, "{{channel:C456}}", markup.Channel("C456"))
	assert.Equal(t, "{{time:1789898400}}", markup.Time(at))
	assert.Equal(t, "{{url:https://example.com|例}}", markup.Link("https://example.com", "例"))
	assert.Equal(t, "**太字**", markup.Bold("太字"))
}

func TestLinkStripsSeparatorFromLabel(t *testing.T) {
	assert.Equal(t, "{{url:https://example.com|ab}}", markup.Link("https://example.com", "a|b"))
}

func TestParse(t *testing.T) {
	at := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		in   string
		want []markup.Token
	}{
		{
			name: "plain",
			in:   "こんにちは",
			want: []markup.Token{{Kind: markup.KindText, Text: "こんにちは"}},
		},
		{
			name: "mention with surrounding text",
			in:   "🙋 " + markup.User("U123") + " が参加しました",
			want: []markup.Token{
				{Kind: markup.KindText, Text: "🙋 "},
				{Kind: markup.KindUser, ID: "U123"},
				{Kind: markup.KindText, Text: " が参加しました"},
			},
		},
		{
			name: "channel",
			in:   markup.Channel("C456"),
			want: []markup.Token{{Kind: markup.KindChannel, ID: "C456"}},
		},
		{
			name: "time",
			in:   markup.Time(at),
			want: []markup.Token{{Kind: markup.KindTime, At: at}},
		},
		{
			name: "link",
			in:   markup.Link("https://example.com/link/abc", "連携する"),
			want: []markup.Token{
				{Kind: markup.KindLink, URL: "https://example.com/link/abc", Text: "連携する"},
			},
		},
		{
			name: "bold",
			in:   "📌 " + markup.Bold("ボドゲ会"),
			want: []markup.Token{
				{Kind: markup.KindText, Text: "📌 "},
				{Kind: markup.KindBold, Text: "ボドゲ会"},
			},
		},
		{
			name: "unknown token stays text",
			in:   "{{mystery:1}}",
			want: []markup.Token{{Kind: markup.KindText, Text: "{{mystery:1}}"}},
		},
		{
			name: "unclosed token stays text",
			in:   "{{user:U123",
			want: []markup.Token{{Kind: markup.KindText, Text: "{{user:U123"}},
		},
		{
			name: "unpaired bold stays text",
			in:   "**not bold",
			want: []markup.Token{{Kind: markup.KindText, Text: "**not bold"}},
		},
		{
			name: "empty",
			in:   "",
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, markup.Parse(tc.in))
		})
	}
}

func TestEscapeNeutralizesUserInput(t *testing.T) {
	escaped := markup.Escape("俺は" + markup.User("U999") + "だ")

	for _, tok := range markup.Parse(escaped) {
		assert.Equal(t, markup.KindText, tok.Kind)
	}
}

func TestPlainText(t *testing.T) {
	jst, err := time.LoadLocation("Asia/Tokyo")
	require.NoError(t, err)

	at := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	in := markup.Bold("会") + " " + markup.Time(at) + " " + markup.User("U1") + " " +
		markup.Link("https://example.com", "こちら")

	assert.Equal(t, "会 2026-09-20 19:00 U1 こちら", markup.PlainText(in, jst))
}

func TestTruncate(t *testing.T) {
	assert.Equal(t, "あいう", markup.Truncate("あいう", 5))
	assert.Equal(t, "あい…", markup.Truncate("あいうえお", 3))
	assert.Empty(t, markup.Truncate("あいう", 0))
}
