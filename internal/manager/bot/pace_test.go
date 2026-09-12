package bot_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bear-san/aso-bell/internal/manager/bot"
	"github.com/bear-san/aso-bell/internal/manager/domain"
)

func TestParseReminderPace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  domain.ReminderPolicy
	}{
		{
			"オフセット",
			"7d,1d,3h",
			domain.ReminderPolicy{
				Mode:    domain.ReminderModeOffsets,
				Offsets: []time.Duration{7 * 24 * time.Hour, 24 * time.Hour, 3 * time.Hour},
			},
		},
		{
			"読点区切り",
			"7d、1d",
			domain.ReminderPolicy{
				Mode:    domain.ReminderModeOffsets,
				Offsets: []time.Duration{7 * 24 * time.Hour, 24 * time.Hour},
			},
		},
		{
			"空白区切り",
			"2d 3h",
			domain.ReminderPolicy{
				Mode:    domain.ReminderModeOffsets,
				Offsets: []time.Duration{2 * 24 * time.Hour, 3 * time.Hour},
			},
		},
		{
			"間隔と時刻",
			"every 2d 20:00",
			domain.ReminderPolicy{
				Mode:        domain.ReminderModeInterval,
				Every:       2 * 24 * time.Hour,
				At:          domain.TimeOfDay{Hour: 20},
				FinalOffset: domain.DefaultFinalOffset,
			},
		},
		{
			"間隔のみ",
			"every 1d",
			domain.ReminderPolicy{
				Mode:        domain.ReminderModeInterval,
				Every:       24 * time.Hour,
				FinalOffset: domain.DefaultFinalOffset,
			},
		},
		{"なし", "off", domain.ReminderPolicy{Mode: domain.ReminderModeNone}},
		{"日本語のなし", "なし", domain.ReminderPolicy{Mode: domain.ReminderModeNone}},
		{"大文字", "OFF", domain.ReminderPolicy{Mode: domain.ReminderModeNone}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := bot.ParseReminderPace(tc.input)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestParseReminderPaceRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"", "そのうち", "7x", "every", "every 30m", "every 2d 20:00 extra", "0h", "7d,7d"} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()

			_, err := bot.ParseReminderPace(input)
			require.ErrorIs(t, err, bot.ErrInvalidPace)
		})
	}
}

func TestFormatReminderPace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		give domain.ReminderPolicy
		want string
	}{
		{"既定", domain.DefaultReminderPolicy(), "7d,1d,3h"},
		{"なし", domain.ReminderPolicy{Mode: domain.ReminderModeNone}, "off"},
		{
			"間隔",
			domain.ReminderPolicy{
				Mode:  domain.ReminderModeInterval,
				Every: 2 * 24 * time.Hour,
				At:    domain.TimeOfDay{Hour: 20},
			},
			"every 2d 20:00",
		},
		{
			"分単位",
			domain.ReminderPolicy{Mode: domain.ReminderModeOffsets, Offsets: []time.Duration{90 * time.Minute}},
			"90m",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, bot.FormatReminderPace(tc.give))
		})
	}
}

func TestReminderPaceRoundTrip(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"7d,1d,3h", "every 2d 20:00", "off"} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()

			policy, err := bot.ParseReminderPace(input)
			require.NoError(t, err)
			assert.Equal(t, input, bot.FormatReminderPace(policy))
		})
	}
}
