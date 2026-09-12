package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bear-san/aso-bell/internal/manager/domain"
)

func TestReminderPolicyValidate(t *testing.T) {
	t.Parallel()

	tooMany := make([]time.Duration, domain.MaxReminders+1)
	for i := range tooMany {
		tooMany[i] = time.Duration(i+1) * time.Hour
	}

	tests := []struct {
		name      string
		policy    domain.ReminderPolicy
		wantField string
	}{
		{name: "default", policy: domain.DefaultReminderPolicy()},
		{name: "none", policy: domain.ReminderPolicy{Mode: domain.ReminderModeNone}},
		{
			name: "interval",
			policy: domain.ReminderPolicy{
				Mode:        domain.ReminderModeInterval,
				Every:       48 * time.Hour,
				At:          domain.TimeOfDay{Hour: 20},
				FinalOffset: 3 * time.Hour,
			},
		},
		{
			name:      "unknown mode",
			policy:    domain.ReminderPolicy{Mode: "weekly"},
			wantField: "reminder_policy.mode",
		},
		{
			name:      "empty offsets",
			policy:    domain.ReminderPolicy{Mode: domain.ReminderModeOffsets},
			wantField: "reminder_policy.offsets",
		},
		{
			name: "negative offset",
			policy: domain.ReminderPolicy{
				Mode:    domain.ReminderModeOffsets,
				Offsets: []time.Duration{-time.Hour},
			},
			wantField: "reminder_policy.offsets",
		},
		{
			name: "duplicated offset",
			policy: domain.ReminderPolicy{
				Mode:    domain.ReminderModeOffsets,
				Offsets: []time.Duration{time.Hour, time.Hour},
			},
			wantField: "reminder_policy.offsets",
		},
		{
			name:      "too many offsets",
			policy:    domain.ReminderPolicy{Mode: domain.ReminderModeOffsets, Offsets: tooMany},
			wantField: "reminder_policy.offsets",
		},
		{
			name: "interval too short",
			policy: domain.ReminderPolicy{
				Mode:  domain.ReminderModeInterval,
				Every: 30 * time.Minute,
			},
			wantField: "reminder_policy.every",
		},
		{
			name: "invalid at",
			policy: domain.ReminderPolicy{
				Mode:  domain.ReminderModeInterval,
				Every: 24 * time.Hour,
				At:    domain.TimeOfDay{Hour: 25},
			},
			wantField: "reminder_policy.at",
		},
		{
			name: "negative final offset",
			policy: domain.ReminderPolicy{
				Mode:        domain.ReminderModeInterval,
				Every:       24 * time.Hour,
				FinalOffset: -time.Hour,
			},
			wantField: "reminder_policy.final_offset",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.policy.Validate("reminder_policy")
			if tc.wantField == "" {
				require.NoError(t, err)

				return
			}

			var verr *domain.ValidationError

			require.ErrorAs(t, err, &verr)
			assert.True(t, verr.Has(tc.wantField), "want field %q in %v", tc.wantField, verr.Fields)
		})
	}
}

func TestReminderPolicyScheduleOffsets(t *testing.T) {
	t.Parallel()

	loc := time.UTC
	now := time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC)
	startsAt := time.Date(2026, time.September, 20, 19, 0, 0, 0, time.UTC)

	got, err := domain.DefaultReminderPolicy().Schedule(now, startsAt, loc)
	require.NoError(t, err)

	// 168h 前(9/13 19:00)は now より後なので残り、すべて昇順に並ぶ。
	assert.Equal(t, []time.Time{
		startsAt.Add(-168 * time.Hour),
		startsAt.Add(-24 * time.Hour),
		startsAt.Add(-3 * time.Hour),
	}, got)
}

func TestReminderPolicyScheduleOffsetsSkipsPast(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 19, 20, 0, 0, 0, time.UTC)
	startsAt := time.Date(2026, time.September, 20, 19, 0, 0, 0, time.UTC)

	got, err := domain.DefaultReminderPolicy().Schedule(now, startsAt, time.UTC)
	require.NoError(t, err)

	assert.Equal(t, []time.Time{startsAt.Add(-3 * time.Hour)}, got)
}

func TestReminderPolicyScheduleNone(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC)
	got, err := domain.ReminderPolicy{Mode: domain.ReminderModeNone}.
		Schedule(now, now.Add(48*time.Hour), time.UTC)

	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestReminderPolicyScheduleInterval(t *testing.T) {
	t.Parallel()

	loc, err := time.LoadLocation("Asia/Tokyo")
	require.NoError(t, err)

	now := time.Date(2026, time.September, 13, 12, 0, 0, 0, loc)
	startsAt := time.Date(2026, time.September, 20, 19, 0, 0, 0, loc)
	policy := domain.ReminderPolicy{
		Mode:        domain.ReminderModeInterval,
		Every:       48 * time.Hour,
		At:          domain.TimeOfDay{Hour: 20},
		FinalOffset: 3 * time.Hour,
	}

	got, err := policy.Schedule(now, startsAt, loc)
	require.NoError(t, err)

	want := []time.Time{
		time.Date(2026, time.September, 13, 20, 0, 0, 0, loc),
		time.Date(2026, time.September, 15, 20, 0, 0, 0, loc),
		time.Date(2026, time.September, 17, 20, 0, 0, 0, loc),
		time.Date(2026, time.September, 19, 20, 0, 0, 0, loc),
		time.Date(2026, time.September, 20, 16, 0, 0, 0, loc),
	}
	assert.Equal(t, want, got)
}

func TestReminderPolicyScheduleIntervalKeepsWallClockAcrossDST(t *testing.T) {
	t.Parallel()

	loc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	// 2026-03-08 に夏時間へ移行する。暦日で進めることで毎回 20:00 に投稿される。
	now := time.Date(2026, time.March, 5, 12, 0, 0, 0, loc)
	startsAt := time.Date(2026, time.March, 12, 19, 0, 0, 0, loc)
	policy := domain.ReminderPolicy{
		Mode:        domain.ReminderModeInterval,
		Every:       24 * time.Hour,
		At:          domain.TimeOfDay{Hour: 20},
		FinalOffset: time.Hour,
	}

	got, err := policy.Schedule(now, startsAt, loc)
	require.NoError(t, err)

	for _, at := range got[:len(got)-1] {
		assert.Equal(t, 20, at.In(loc).Hour(), "reminder at %s", at)
	}

	assert.Equal(t, time.Date(2026, time.March, 12, 18, 0, 0, 0, loc), got[len(got)-1])
}

func TestReminderPolicyScheduleRejectsTooManyReminders(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	startsAt := now.AddDate(0, 0, 90)
	policy := domain.ReminderPolicy{
		Mode:        domain.ReminderModeInterval,
		Every:       24 * time.Hour,
		At:          domain.TimeOfDay{Hour: 20},
		FinalOffset: time.Hour,
	}

	_, err := policy.Schedule(now, startsAt, time.UTC)

	var verr *domain.ValidationError

	require.ErrorAs(t, err, &verr)
	assert.True(t, verr.Has("reminder_policy"))
}

func TestReminderPolicyScheduleRejectsInvalidPolicy(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	_, err := domain.ReminderPolicy{Mode: "weekly"}.Schedule(now, now.Add(time.Hour), time.UTC)

	require.Error(t, err)
}
