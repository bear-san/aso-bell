// Package channelname はイベント用チャンネル名を Provider 非依存の規則で正規化する(docs/06-provider.md §4)。
package channelname

import (
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// チャンネル名の長さ上限。Discord はアーカイブ時に `archived_`(9 文字)を前置するため 9 文字分を残す。
const (
	SlackMaxLen   = 80
	DiscordMaxLen = 90
)

const (
	separator  = '-'
	dateLayout = "0102"
)

// Build は `<prefix><MMDD>-<slug>` 形式のチャンネル名を作る。startsAt はワークスペースの
// タイムゾーンに変換済みの開始日時を渡すこと。
func Build(prefix, title string, startsAt time.Time, maxLen int) string {
	head := prefix + startsAt.Format(dateLayout)

	slug := Slug(title)
	if slug == "" {
		return truncate(head, maxLen)
	}

	return truncate(head+string(separator)+slug, maxLen)
}

// Slug はタイトルをチャンネル名に使える文字だけに正規化する。日本語はそのまま残す。
func Slug(title string) string {
	// 全角英数や互換文字を半角へ揃えるため NFKC で正規化してから小文字化する。
	normalized := strings.ToLower(norm.NFKC.String(title))

	var b strings.Builder

	for _, r := range normalized {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
			b.WriteRune(r)
		default:
			// 空白・`/`・記号・絵文字はすべて区切りに寄せ、後段で連続分を畳む。
			b.WriteRune(separator)
		}
	}

	return collapse(b.String())
}

// WithSuffix は `ErrNameTaken` の再試行で使う連番付きの名前を返す。長さ上限は元の名前を削って守る。
func WithSuffix(name string, n, maxLen int) string {
	suffix := string(separator) + strconv.Itoa(n)

	room := maxLen - len([]rune(suffix))
	if room < 1 {
		return truncate(suffix, maxLen)
	}

	return truncate(name, room) + suffix
}

func collapse(s string) string {
	var b strings.Builder

	prevSep := true

	for _, r := range s {
		if r == separator {
			if !prevSep {
				b.WriteRune(r)
			}

			prevSep = true

			continue
		}

		b.WriteRune(r)

		prevSep = false
	}

	return strings.TrimRight(b.String(), string(separator))
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return strings.TrimRight(s, string(separator))
	}

	return strings.TrimRight(string(runes[:maxLen]), string(separator))
}
