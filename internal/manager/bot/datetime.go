// Package bot は Manager 側の Bot Core。コマンドの解釈、フォーム定義、返信文言の組み立てを担う
// (docs/07-bot-ux.md)。プラットフォーム固有の描画は Provider の責務で、ここには持ち込まない。
package bot

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"

	"github.com/bear-san/aso-bell/internal/manager/domain"
)

// ErrInvalidDateTime は日時入力を解釈できなかったことを表す。呼び出し元は受理形式を添えて案内する。
var ErrInvalidDateTime = errors.New("invalid date time")

// 日付・時刻の受理パターン(docs/07-bot-ux.md §1.2)。年を省略した場合は当年・翌年を補う。
var (
	datePattern = regexp.MustCompile(`^(?:(\d{4})[-/])?(\d{1,2})[-/](\d{1,2})$`)
	timePattern = regexp.MustCompile(`^(\d{1,2}):(\d{2})$`)
	deltaPatten = regexp.MustCompile(`^\+(.+)$`)
)

const maxDateTimeParts = 2

// ParseStartTime は `when` の入力を loc のタイムゾーンで解釈する。
// 時刻を省略した場合は 00:00 ではなく defaultTime を使う(docs/07-bot-ux.md §1.2)。
func ParseStartTime(input string, now time.Time, loc *time.Location, defaultTime domain.TimeOfDay) (time.Time, error) {
	s := normalizeInput(input)
	if s == "" {
		return time.Time{}, fmt.Errorf("%w: %q", ErrInvalidDateTime, input)
	}

	if days, rest, ok := relativeDays(s); ok {
		tod, err := timeOrDefault(rest, defaultTime)
		if err != nil {
			return time.Time{}, fmt.Errorf("%w: %q", ErrInvalidDateTime, input)
		}

		return tod.On(now.In(loc).AddDate(0, 0, days), loc), nil
	}

	at, err := parseAbsolute(s, now, loc, defaultTime)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %q", ErrInvalidDateTime, input)
	}

	return at, nil
}

// ParseEndTime は `ends` の入力を解釈する。`+3h` は開始日時からの相対、`HH:MM` は開始日の同じ日の時刻
// (開始以前になる場合は翌日)として扱う。
func ParseEndTime(input string, startsAt time.Time, loc *time.Location) (time.Time, error) {
	s := normalizeInput(input)
	if s == "" {
		return time.Time{}, fmt.Errorf("%w: %q", ErrInvalidDateTime, input)
	}

	if m := deltaPatten.FindStringSubmatch(s); m != nil {
		d, err := domain.ParseDuration(m[1])
		if err != nil || d <= 0 {
			return time.Time{}, fmt.Errorf("%w: %q", ErrInvalidDateTime, input)
		}

		return startsAt.Add(d), nil
	}

	if tod, err := parseTimeOfDay(s); err == nil {
		end := tod.On(startsAt.In(loc), loc)
		if !end.After(startsAt) {
			end = tod.On(startsAt.In(loc).AddDate(0, 0, 1), loc)
		}

		return end, nil
	}

	// 終了日時は開始日時より後と決まっているため、年の推測も開始日時を基準にする。
	at, err := parseAbsolute(s, startsAt, loc, domain.TimeOfDay{})
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %q", ErrInvalidDateTime, input)
	}

	return at, nil
}

func parseAbsolute(s string, base time.Time, loc *time.Location, defaultTime domain.TimeOfDay) (time.Time, error) {
	parts := strings.SplitN(s, " ", maxDateTimeParts)

	m := datePattern.FindStringSubmatch(parts[0])
	if m == nil {
		return time.Time{}, ErrInvalidDateTime
	}

	rest := ""
	if len(parts) == maxDateTimeParts {
		rest = parts[1]
	}

	tod, err := timeOrDefault(rest, defaultTime)
	if err != nil {
		return time.Time{}, err
	}

	month, err := strconv.Atoi(m[2])
	if err != nil {
		return time.Time{}, ErrInvalidDateTime
	}

	day, err := strconv.Atoi(m[3])
	if err != nil {
		return time.Time{}, ErrInvalidDateTime
	}

	if m[1] != "" {
		year, convErr := strconv.Atoi(m[1])
		if convErr != nil {
			return time.Time{}, ErrInvalidDateTime
		}

		return dateAt(year, month, day, tod, loc)
	}

	at, err := dateAt(base.In(loc).Year(), month, day, tod, loc)
	if err != nil {
		return time.Time{}, err
	}

	// 年を省略した入力は「これから来る日付」を指すのが自然なため、過ぎていれば翌年とみなす。
	if at.Before(base) {
		return dateAt(base.In(loc).Year()+1, month, day, tod, loc)
	}

	return at, nil
}

func dateAt(year, month, day int, tod domain.TimeOfDay, loc *time.Location) (time.Time, error) {
	at := time.Date(year, time.Month(month), day, tod.Hour, tod.Minute, 0, 0, loc)

	// time.Date は 2/30 のような存在しない日付を繰り上げるため、入力どおりの日付かを確認する。
	if at.Month() != time.Month(month) || at.Day() != day {
		return time.Time{}, ErrInvalidDateTime
	}

	return at, nil
}

func timeOrDefault(s string, defaultTime domain.TimeOfDay) (domain.TimeOfDay, error) {
	if s == "" {
		return defaultTime, nil
	}

	return parseTimeOfDay(s)
}

func parseTimeOfDay(s string) (domain.TimeOfDay, error) {
	m := timePattern.FindStringSubmatch(s)
	if m == nil {
		return domain.TimeOfDay{}, ErrInvalidDateTime
	}

	hour, err := strconv.Atoi(m[1])
	if err != nil {
		return domain.TimeOfDay{}, ErrInvalidDateTime
	}

	minute, err := strconv.Atoi(m[2])
	if err != nil {
		return domain.TimeOfDay{}, ErrInvalidDateTime
	}

	tod := domain.TimeOfDay{Hour: hour, Minute: minute}
	if !tod.Valid() {
		return domain.TimeOfDay{}, ErrInvalidDateTime
	}

	return tod, nil
}

// relativeDays は「明日」「tomorrow」などの相対指定を日数と残りの文字列に分ける。
func relativeDays(s string) (int, string, bool) {
	for _, kw := range []struct {
		word string
		days int
	}{
		{"明後日", 2},
		{"あさって", 2},
		{"day after tomorrow", 2},
		{"tomorrow", 1},
		{"明日", 1},
		{"あした", 1},
		{"today", 0},
		{"今日", 0},
		{"本日", 0},
	} {
		if rest, ok := cutPrefixFold(s, kw.word); ok {
			return kw.days, rest, true
		}
	}

	return 0, "", false
}

func cutPrefixFold(s, prefix string) (string, bool) {
	if len(s) < len(prefix) || !strings.EqualFold(s[:len(prefix)], prefix) {
		return "", false
	}

	return strings.TrimSpace(s[len(prefix):]), true
}

// normalizeInput は全角の数字・記号・空白を含む入力を比較しやすい形へ揃える。
func normalizeInput(s string) string {
	return strings.Join(strings.Fields(norm.NFKC.String(s)), " ")
}
