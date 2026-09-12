// Package render は asobell.v1.Message をプラットフォームの表現へ描画する共通部分を提供する
// (docs/06-provider.md §5)。記法ごとの差異は Syntax 実装に閉じ込め、組み立ては共通にする。
package render

import (
	"strconv"
	"strings"
	"time"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/shared/markup"
)

// 本文の上限(docs/06 §5)。切り詰めは描画の最後に行う。
const (
	DiscordTextLimit = 2000
	SlackTextLimit   = 3000
)

// sectionHeadLines はセクションのタイトルと本文の行数。容量の見積もりに使う。
const sectionHeadLines = 2

// Syntax はプラットフォームごとの記法。markup のトークンを表示用の文字列へ変換する。
type Syntax interface {
	User(id string) string
	Channel(id string) string
	Time(t time.Time) string
	Bold(text string) string
	Link(url, label string) string
	// Plain は装飾を持たない本文。記法の制御文字をエスケープする必要があればここで行う。
	Plain(text string) string
}

// Inline は本文 1 行分の記法を変換する。
func Inline(s Syntax, text string) string {
	var b strings.Builder

	for _, tok := range markup.Parse(text) {
		switch tok.Kind {
		case markup.KindText:
			b.WriteString(s.Plain(tok.Text))
		case markup.KindBold:
			b.WriteString(s.Bold(s.Plain(tok.Text)))
		case markup.KindUser:
			b.WriteString(s.User(tok.ID))
		case markup.KindChannel:
			b.WriteString(s.Channel(tok.ID))
		case markup.KindTime:
			b.WriteString(s.Time(tok.At))
		case markup.KindLink:
			b.WriteString(s.Link(tok.URL, s.Plain(tok.Text)))
		}
	}

	return b.String()
}

// Text は Message 全体を 1 本のテキストへ描画し、limit 文字に収める。
// セクションを持たないプラットフォーム(Discord)と、section へ分ける前の素材として使う。
func Text(s Syntax, msg *asobellv1.Message, limit int) string {
	blocks := make([]string, 0, len(msg.GetSections())+1)

	if text := Inline(s, msg.GetText()); text != "" {
		blocks = append(blocks, text)
	}

	for _, section := range msg.GetSections() {
		if rendered := Section(s, section); rendered != "" {
			blocks = append(blocks, rendered)
		}
	}

	return markup.Truncate(strings.Join(blocks, "\n\n"), limit)
}

// Section は 1 セクションをタイトル・本文・フィールドの並びへ描画する。
func Section(s Syntax, section *asobellv1.Section) string {
	lines := make([]string, 0, len(section.GetFields())+sectionHeadLines)

	if title := Inline(s, section.GetTitle()); title != "" {
		lines = append(lines, s.Bold(title))
	}

	if body := Inline(s, section.GetBody()); body != "" {
		lines = append(lines, body)
	}

	for _, field := range section.GetFields() {
		label := Inline(s, field.GetLabel())
		value := Inline(s, field.GetValue())

		switch {
		case label == "" && value == "":
			continue
		case label == "":
			lines = append(lines, value)
		default:
			lines = append(lines, s.Bold(label)+": "+value)
		}
	}

	return strings.Join(lines, "\n")
}

// DiscordSyntax は Discord の記法(docs/06 §5)。
type DiscordSyntax struct{}

// User はユーザーメンションを描画する。
func (DiscordSyntax) User(id string) string { return "<@" + id + ">" }

// Channel はチャンネルリンクを描画する。
func (DiscordSyntax) Channel(id string) string { return "<#" + id + ">" }

// Time は閲覧者のタイムゾーンで表示される時刻を描画する。
func (DiscordSyntax) Time(t time.Time) string {
	return "<t:" + strconv.FormatInt(t.Unix(), 10) + ":F>"
}

// Bold は太字を描画する。
func (DiscordSyntax) Bold(text string) string { return "**" + text + "**" }

// Link は Markdown リンクを描画する。Bot のメッセージでは Markdown リンクが使える。
func (DiscordSyntax) Link(url, label string) string {
	if label == "" {
		return url
	}

	return "[" + label + "](" + url + ")"
}

// discordSpecials は Discord の Markdown で装飾として解釈される記号。
const discordSpecials = "\\*_~`|"

// Plain は Markdown の制御文字を打ち消す。ユーザー入力の記号が装飾として解釈されるのを防ぐ。
func (DiscordSyntax) Plain(text string) string {
	var b strings.Builder

	for _, r := range text {
		if strings.ContainsRune(discordSpecials, r) {
			b.WriteByte('\\')
		}

		b.WriteRune(r)
	}

	return b.String()
}

// SlackSyntax は Slack(mrkdwn)の記法(docs/06 §5)。
type SlackSyntax struct{}

// User はユーザーメンションを描画する。
func (SlackSyntax) User(id string) string { return "<@" + id + ">" }

// Channel はチャンネルリンクを描画する。
func (SlackSyntax) Channel(id string) string { return "<#" + id + ">" }

// Time は閲覧者のタイムゾーンで表示される時刻を描画する。
// Slack の date 記法はフォールバック文字列を必須とするため、UTC 表記を添える。
func (SlackSyntax) Time(t time.Time) string {
	unix := strconv.FormatInt(t.Unix(), 10)
	fallback := t.UTC().Format("2006-01-02 15:04 MST")

	return "<!date^" + unix + "^{date_short_pretty} {time}|" + fallback + ">"
}

// Bold は太字を描画する。mrkdwn の太字はアスタリスク 1 つ。
func (SlackSyntax) Bold(text string) string { return "*" + text + "*" }

// Link はラベル付きリンクを描画する。
func (SlackSyntax) Link(url, label string) string {
	if label == "" {
		return "<" + url + ">"
	}

	return "<" + url + "|" + label + ">"
}

// Plain は Slack がリンク記法として解釈する記号をエスケープする(& < > の 3 つのみ)。
func (SlackSyntax) Plain(text string) string {
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, "<", "&lt;")

	return strings.ReplaceAll(text, ">", "&gt;")
}
