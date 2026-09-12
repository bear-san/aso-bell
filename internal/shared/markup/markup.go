// Package markup は Provider 非依存のメッセージ本文記法を組み立て・解析する(docs/06-provider.md §5)。
// Manager の Bot がトークンを含む本文を組み立て、各 Provider の描画層が Parse して
// プラットフォーム固有の表現(Slack の <@ID>、Discord の <t:UNIX:F> など)へ変換する。
package markup

import (
	"strconv"
	"strings"
	"time"
)

// Kind は解析結果のトークン種別。
type Kind string

// トークン種別。
const (
	KindText    Kind = "text"
	KindBold    Kind = "bold"
	KindUser    Kind = "user"
	KindChannel Kind = "channel"
	KindTime    Kind = "time"
	KindLink    Kind = "link"
)

// Token は解析された本文の断片。
type Token struct {
	Kind Kind
	// Text は text / bold の本文、link のラベル。
	Text string
	// ID は user / channel の ID。
	ID string
	// At は time の時刻。
	At time.Time
	// URL は link の URL。
	URL string
}

const (
	openToken  = "{{"
	closeToken = "}}"
	boldToken  = "**"
	// 不可視の WORD JOINER。ユーザー入力に含まれる "{{" を無害化するために挟む。
	wordJoiner = "\u2060"
)

// User はユーザーメンションを表すトークンを返す。
func User(id string) string { return openToken + "user:" + id + closeToken }

// Channel はチャンネルリンクを表すトークンを返す。
func Channel(id string) string { return openToken + "channel:" + id + closeToken }

// Time は閲覧者のタイムゾーンで表示される時刻トークンを返す。
func Time(t time.Time) string {
	return openToken + "time:" + strconv.FormatInt(t.Unix(), 10) + closeToken
}

// Link はラベル付きリンクのトークンを返す。ラベル中の "|" は表示できないため除去する。
func Link(url, label string) string {
	return openToken + "url:" + url + "|" + strings.ReplaceAll(label, "|", "") + closeToken
}

// Bold は太字を表す記法を返す。
func Bold(s string) string { return boldToken + s + boldToken }

// Escape はユーザー入力をそのまま本文へ埋め込めるようにする。
// タイトルなどに含まれる "{{" がメンションとして解釈されると、他人へのメンションを
// 詐称できてしまうため、不可視文字を挟んでトークンとして成立しないようにする。
func Escape(s string) string {
	return strings.ReplaceAll(s, openToken, "{"+wordJoiner+"{")
}

// Parse は本文をトークン列へ分解する。未知の記法や壊れたトークンはそのまま text として扱う。
func Parse(s string) []Token {
	var (
		tokens []Token
		buf    strings.Builder
	)

	flush := func() {
		if buf.Len() > 0 {
			tokens = append(tokens, Token{Kind: KindText, Text: buf.String()})
			buf.Reset()
		}
	}

	for i := 0; i < len(s); {
		switch {
		case strings.HasPrefix(s[i:], openToken):
			tok, width, ok := parseToken(s[i:])
			if !ok {
				buf.WriteByte(s[i])
				i++

				continue
			}

			flush()
			tokens = append(tokens, tok)
			i += width
		case strings.HasPrefix(s[i:], boldToken):
			body, width, ok := parseBold(s[i:])
			if !ok {
				buf.WriteByte(s[i])
				i++

				continue
			}

			flush()
			tokens = append(tokens, Token{Kind: KindBold, Text: body})
			i += width
		default:
			buf.WriteByte(s[i])
			i++
		}
	}

	flush()

	return tokens
}

// PlainText はトークンを装飾なしの文字列へ戻す。ログや検索向けの近似表現。
func PlainText(s string, loc *time.Location) string {
	var b strings.Builder

	for _, tok := range Parse(s) {
		switch tok.Kind {
		case KindText, KindBold:
			b.WriteString(tok.Text)
		case KindUser, KindChannel:
			b.WriteString(tok.ID)
		case KindTime:
			b.WriteString(tok.At.In(loc).Format("2006-01-02 15:04"))
		case KindLink:
			b.WriteString(tok.Text)
		}
	}

	return b.String()
}

func parseToken(s string) (Token, int, bool) {
	end := strings.Index(s, closeToken)
	if end < 0 {
		return Token{}, 0, false
	}

	body := s[len(openToken):end]
	width := end + len(closeToken)

	name, arg, found := strings.Cut(body, ":")
	if !found || arg == "" {
		return Token{}, 0, false
	}

	switch name {
	case "user":
		return Token{Kind: KindUser, ID: arg}, width, true
	case "channel":
		return Token{Kind: KindChannel, ID: arg}, width, true
	case "time":
		unix, err := strconv.ParseInt(arg, 10, 64)
		if err != nil {
			return Token{}, 0, false
		}

		return Token{Kind: KindTime, At: time.Unix(unix, 0).UTC()}, width, true
	case "url":
		url, label, hasLabel := strings.Cut(arg, "|")
		if !hasLabel || url == "" {
			return Token{}, 0, false
		}

		return Token{Kind: KindLink, URL: url, Text: label}, width, true
	default:
		return Token{}, 0, false
	}
}

func parseBold(s string) (string, int, bool) {
	rest := s[len(boldToken):]

	end := strings.Index(rest, boldToken)
	if end <= 0 {
		return "", 0, false
	}

	return rest[:end], len(boldToken)*2 + end, true
}

// Truncate は表示上の長さを rune 単位で limit に収め、切り詰めた場合は末尾に "…" を付ける。
func Truncate(s string, limit int) string {
	if limit <= 0 {
		return ""
	}

	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}

	return string(runes[:limit-1]) + "…"
}
