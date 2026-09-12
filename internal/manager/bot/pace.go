package bot

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bear-san/aso-bell/internal/manager/domain"
)

// ErrInvalidPace はリマインドペースの入力を解釈できなかったことを表す。
var ErrInvalidPace = errors.New("invalid reminder pace")

const (
	intervalKeyword   = "every"
	intervalKeywordJA = "毎"
	offsetSeparators  = ",、 "
	// intervalWithTimeParts は `every 2d 20:00` のように時刻まで指定した場合の語数。
	intervalWithTimeParts = 2
)

// ParseReminderPace は `/asobell remind` の引数を ReminderPolicy に変換する(docs/07-bot-ux.md §1.1)。
// `off` は none、`every 2d 20:00` は interval、それ以外はカンマ区切りの offsets として解釈する。
func ParseReminderPace(input string) (domain.ReminderPolicy, error) {
	s := normalizeInput(input)
	if s == "" {
		return domain.ReminderPolicy{}, fmt.Errorf("%w: %q", ErrInvalidPace, input)
	}

	if isNonePace(s) {
		return domain.ReminderPolicy{Mode: domain.ReminderModeNone}, nil
	}

	policy, err := parsePace(s)
	if err != nil {
		return domain.ReminderPolicy{}, fmt.Errorf("%w: %q", ErrInvalidPace, input)
	}

	if validateErr := policy.Validate("reminder_policy"); validateErr != nil {
		return domain.ReminderPolicy{}, fmt.Errorf("%w: %q: %w", ErrInvalidPace, input, validateErr)
	}

	return policy, nil
}

// FormatReminderPace は ReminderPolicy を `/asobell remind` に渡せる表記へ戻す。
func FormatReminderPace(p domain.ReminderPolicy) string {
	switch p.Mode {
	case domain.ReminderModeNone:
		return "off"
	case domain.ReminderModeInterval:
		return fmt.Sprintf("every %s %s", formatOffset(p.Every), p.At.String())
	case domain.ReminderModeOffsets:
		parts := make([]string, 0, len(p.Offsets))
		for _, off := range p.Offsets {
			parts = append(parts, formatOffset(off))
		}

		return strings.Join(parts, ",")
	default:
		return ""
	}
}

func isNonePace(s string) bool {
	switch strings.ToLower(s) {
	case "off", "none", "なし", "no", "オフ":
		return true
	default:
		return false
	}
}

func parsePace(s string) (domain.ReminderPolicy, error) {
	if rest, ok := cutPrefixFold(s, intervalKeyword); ok {
		return parseInterval(rest)
	}

	if rest, ok := cutPrefixFold(s, intervalKeywordJA); ok {
		return parseInterval(rest)
	}

	return parseOffsets(s)
}

func parseInterval(rest string) (domain.ReminderPolicy, error) {
	fields := strings.Fields(rest)
	if len(fields) == 0 || len(fields) > intervalWithTimeParts {
		return domain.ReminderPolicy{}, ErrInvalidPace
	}

	every, err := domain.ParseDuration(fields[0])
	if err != nil {
		return domain.ReminderPolicy{}, ErrInvalidPace
	}

	policy := domain.ReminderPolicy{
		Mode:        domain.ReminderModeInterval,
		Every:       every,
		FinalOffset: domain.DefaultFinalOffset,
	}

	if len(fields) == intervalWithTimeParts {
		at, todErr := parseTimeOfDay(fields[1])
		if todErr != nil {
			return domain.ReminderPolicy{}, ErrInvalidPace
		}

		policy.At = at
	}

	return policy, nil
}

func parseOffsets(s string) (domain.ReminderPolicy, error) {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return strings.ContainsRune(offsetSeparators, r)
	})
	if len(fields) == 0 {
		return domain.ReminderPolicy{}, ErrInvalidPace
	}

	offsets := make([]time.Duration, 0, len(fields))

	for _, field := range fields {
		d, err := domain.ParseDuration(field)
		if err != nil {
			return domain.ReminderPolicy{}, ErrInvalidPace
		}

		offsets = append(offsets, d)
	}

	return domain.ReminderPolicy{Mode: domain.ReminderModeOffsets, Offsets: offsets}, nil
}

// formatOffset は 24 時間の倍数を `7d`、時・分の倍数を `3h` `30m` のように、入力で使える表記へ整形する。
func formatOffset(d time.Duration) string {
	const day = 24 * time.Hour

	switch {
	case d >= day && d%day == 0:
		return fmt.Sprintf("%dd", d/day)
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	default:
		return d.String()
	}
}
