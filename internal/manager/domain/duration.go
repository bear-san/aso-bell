package domain

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const hoursPerDay = 24

// チャット入力の `7d` を Go の time.ParseDuration が解釈できる形へ書き換えるための検出パターン。
var dayUnitPattern = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)d`)

// ParseDuration は Go の duration 文字列に加えて、チャット入力で使われる `d`(日)単位を解釈する
// (docs/04-domain-model.md §3)。`7d` は `168h` として扱う。
func ParseDuration(s string) (time.Duration, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return 0, fmt.Errorf("parse duration %q: empty", s)
	}

	converted, err := expandDayUnit(trimmed)
	if err != nil {
		return 0, err
	}

	d, err := time.ParseDuration(converted)
	if err != nil {
		return 0, fmt.Errorf("parse duration %q: %w", s, err)
	}

	return d, nil
}

// FormatDuration は duration を永続化・API 表現("168h0m0s")に変換する。
func FormatDuration(d time.Duration) string {
	return d.String()
}

func expandDayUnit(s string) (string, error) {
	var convErr error

	replaced := dayUnitPattern.ReplaceAllStringFunc(s, func(match string) string {
		days, err := strconv.ParseFloat(strings.TrimSuffix(match, "d"), 64)
		if err != nil {
			convErr = fmt.Errorf("parse duration %q: %w", s, err)

			return match
		}

		// 浮動小数の丸め誤差を避けるため、時間ではなくナノ秒へ展開する。
		return strconv.FormatInt(int64(days*hoursPerDay*float64(time.Hour)), 10) + "ns"
	})
	if convErr != nil {
		return "", convErr
	}

	return replaced, nil
}
