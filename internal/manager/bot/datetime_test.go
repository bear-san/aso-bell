package bot_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bear-san/aso-bell/internal/manager/bot"
	"github.com/bear-san/aso-bell/internal/manager/domain"
)

func tokyo(t *testing.T) *time.Location {
	t.Helper()

	loc, err := time.LoadLocation("Asia/Tokyo")
	require.NoError(t, err)

	return loc
}

func TestParseStartTime(t *testing.T) {
	t.Parallel()

	loc := tokyo(t)
	now := time.Date(2026, 9, 12, 10, 0, 0, 0, loc)
	defaultTime := domain.TimeOfDay{Hour: 19}

	tests := []struct {
		name  string
		input string
		want  time.Time
	}{
		{"絶対日時", "2026-09-20 19:00", time.Date(2026, 9, 20, 19, 0, 0, 0, loc)},
		{"スラッシュ区切りの年付き", "2026/09/20 19:30", time.Date(2026, 9, 20, 19, 30, 0, 0, loc)},
		{"年省略", "9/20 19:00", time.Date(2026, 9, 20, 19, 0, 0, 0, loc)},
		{"年省略・過去日は翌年", "3/1 19:00", time.Date(2027, 3, 1, 19, 0, 0, 0, loc)},
		{"時刻省略は既定時刻", "9/20", time.Date(2026, 9, 20, 19, 0, 0, 0, loc)},
		{"日付のみ年付き", "2026-12-31", time.Date(2026, 12, 31, 19, 0, 0, 0, loc)},
		{"明日", "明日 19:00", time.Date(2026, 9, 13, 19, 0, 0, 0, loc)},
		{"tomorrow", "tomorrow 9:30", time.Date(2026, 9, 13, 9, 30, 0, 0, loc)},
		{"tomorrow のみ", "tomorrow", time.Date(2026, 9, 13, 19, 0, 0, 0, loc)},
		{"今日", "今日 23:00", time.Date(2026, 9, 12, 23, 0, 0, 0, loc)},
		{"明後日", "明後日", time.Date(2026, 9, 14, 19, 0, 0, 0, loc)},
		{"空白なしの相対指定", "明日19:00", time.Date(2026, 9, 13, 19, 0, 0, 0, loc)},
		{"全角入力", "９/２０　１９：００", time.Date(2026, 9, 20, 19, 0, 0, 0, loc)},
		{"前後の空白", "  9/20 19:00  ", time.Date(2026, 9, 20, 19, 0, 0, 0, loc)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := bot.ParseStartTime(tc.input, now, loc, defaultTime)
			require.NoError(t, err)
			assert.True(t, tc.want.Equal(got), "want %s, got %s", tc.want, got)
		})
	}
}

func TestParseStartTimeRejectsUnknownInput(t *testing.T) {
	t.Parallel()

	loc := tokyo(t)
	now := time.Date(2026, 9, 12, 10, 0, 0, 0, loc)

	for _, input := range []string{"", "あとで", "2026-13-01", "9/31", "9/20 25:00", "9/20 19:0", "+3h"} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()

			_, err := bot.ParseStartTime(input, now, loc, domain.TimeOfDay{Hour: 19})
			require.ErrorIs(t, err, bot.ErrInvalidDateTime)
		})
	}
}

func TestParseEndTime(t *testing.T) {
	t.Parallel()

	loc := tokyo(t)
	startsAt := time.Date(2026, 9, 20, 19, 0, 0, 0, loc)

	tests := []struct {
		name  string
		input string
		want  time.Time
	}{
		{"開始からの相対", "+3h", time.Date(2026, 9, 20, 22, 0, 0, 0, loc)},
		{"日単位の相対", "+1d", time.Date(2026, 9, 21, 19, 0, 0, 0, loc)},
		{"同日の時刻", "22:30", time.Date(2026, 9, 20, 22, 30, 0, 0, loc)},
		{"開始以前の時刻は翌日", "01:00", time.Date(2026, 9, 21, 1, 0, 0, 0, loc)},
		{"日付と時刻", "9/21 12:00", time.Date(2026, 9, 21, 12, 0, 0, 0, loc)},
		{"年付き", "2026-09-22 12:00", time.Date(2026, 9, 22, 12, 0, 0, 0, loc)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := bot.ParseEndTime(tc.input, startsAt, loc)
			require.NoError(t, err)
			assert.True(t, tc.want.Equal(got), "want %s, got %s", tc.want, got)
		})
	}
}

func TestParseEndTimeRejectsUnknownInput(t *testing.T) {
	t.Parallel()

	loc := tokyo(t)
	startsAt := time.Date(2026, 9, 20, 19, 0, 0, 0, loc)

	for _, input := range []string{"", "+0h", "+-3h", "そのうち", "9/31 12:00"} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()

			_, err := bot.ParseEndTime(input, startsAt, loc)
			require.ErrorIs(t, err, bot.ErrInvalidDateTime)
		})
	}
}

func TestParseStartTimeAcrossDST(t *testing.T) {
	t.Parallel()

	loc, err := time.LoadLocation("America/Los_Angeles")
	require.NoError(t, err)

	now := time.Date(2026, 3, 1, 10, 0, 0, 0, loc)

	got, err := bot.ParseStartTime("3/8 19:00", now, loc, domain.TimeOfDay{Hour: 19})
	require.NoError(t, err)

	assert.Equal(t, "2026-03-08T19:00:00-07:00", got.Format(time.RFC3339))
}
