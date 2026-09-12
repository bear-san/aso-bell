package bot_test

import (
	"maps"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/manager/bot"
	"github.com/bear-san/aso-bell/internal/manager/domain"
)

func fieldByID(t *testing.T, form *asobellv1.Form, id string) *asobellv1.FormField {
	t.Helper()

	for _, f := range form.GetFields() {
		if f.GetId() == id {
			return f
		}
	}

	require.Failf(t, "field not found", "form %s has no field %q", form.GetFormId(), id)

	return nil
}

func TestNewEventForm(t *testing.T) {
	t.Parallel()

	loc := tokyo(t)
	now := time.Date(2026, 9, 12, 10, 0, 0, 0, loc)
	settings := domain.DefaultWorkspaceSettings()

	form, err := bot.NewEventForm(now, settings, "C001")
	require.NoError(t, err)

	assert.Equal(t, bot.FormEventNew, form.GetFormId())
	assert.Equal(t, "C001", form.GetMetadata())
	assert.Equal(t, "2026-09-13", fieldByID(t, form, bot.FieldDate).GetInitialValue())
	assert.Equal(t, "19:00", fieldByID(t, form, bot.FieldTime).GetInitialValue())
	assert.Equal(t, int32(domain.TitleMaxLen), fieldByID(t, form, bot.FieldTitle).GetMaxLength())
	assert.True(t, fieldByID(t, form, bot.FieldTitle).GetRequired())
	assert.False(t, fieldByID(t, form, bot.FieldEnds).GetRequired())
	assert.Len(t, fieldByID(t, form, bot.FieldRemind).GetOptions(), 5)
}

func TestNewEventFormRejectsUnknownTimezone(t *testing.T) {
	t.Parallel()

	settings := domain.DefaultWorkspaceSettings()
	settings.Timezone = "Mars/Olympus"

	_, err := bot.NewEventForm(time.Now(), settings, "C001")

	require.Error(t, err)
}

func TestEditEventFormUsesCurrentValues(t *testing.T) {
	t.Parallel()

	ev := testEvent()
	settings := domain.DefaultWorkspaceSettings()

	form, err := bot.EditEventForm(ev, settings)
	require.NoError(t, err)

	assert.Equal(t, bot.FormEventEdit, form.GetFormId())
	assert.Equal(t, ev.ID, form.GetMetadata())
	assert.Equal(t, "ボドゲ会", fieldByID(t, form, bot.FieldTitle).GetInitialValue())
	assert.Equal(t, "2026-09-20", fieldByID(t, form, bot.FieldDate).GetInitialValue())
	assert.Equal(t, "19:00", fieldByID(t, form, bot.FieldTime).GetInitialValue())
	assert.Equal(t, "22:00", fieldByID(t, form, bot.FieldEnds).GetInitialValue())
	assert.Equal(t, "初心者歓迎", fieldByID(t, form, bot.FieldDescription).GetInitialValue())
}

func TestParseEventForm(t *testing.T) {
	t.Parallel()

	loc := tokyo(t)
	now := time.Date(2026, 9, 12, 10, 0, 0, 0, loc)
	settings := domain.DefaultWorkspaceSettings()

	in, errs := bot.ParseEventForm(map[string]string{
		bot.FieldTitle:       " ボドゲ会 ",
		bot.FieldDate:        "2026-09-20",
		bot.FieldTime:        "19:00",
		bot.FieldEnds:        "+3h",
		bot.FieldDescription: "初心者歓迎",
		bot.FieldRemind:      bot.RemindPresetStandard,
	}, now, settings)

	assert.Empty(t, errs)
	assert.Equal(t, "ボドゲ会", in.Title)
	assert.Equal(t, "初心者歓迎", in.Description)
	assert.True(t, time.Date(2026, 9, 20, 19, 0, 0, 0, loc).Equal(in.StartsAt))
	require.NotNil(t, in.EndsAt)
	assert.True(t, time.Date(2026, 9, 20, 22, 0, 0, 0, loc).Equal(*in.EndsAt))
	require.NotNil(t, in.ReminderPolicy)
	assert.Equal(t, domain.DefaultReminderPolicy(), *in.ReminderPolicy)
}

func TestParseEventFormReportsFieldErrors(t *testing.T) {
	t.Parallel()

	loc := tokyo(t)
	now := time.Date(2026, 9, 12, 10, 0, 0, 0, loc)
	settings := domain.DefaultWorkspaceSettings()

	tests := []struct {
		name      string
		values    map[string]string
		wantField string
	}{
		{"タイトル未入力", map[string]string{bot.FieldDate: "2026-09-20"}, bot.FieldTitle},
		{"日付未入力", map[string]string{bot.FieldTitle: "会"}, bot.FieldDate},
		{
			"過去の日付",
			map[string]string{bot.FieldTitle: "会", bot.FieldDate: "2026-09-01", bot.FieldTime: "19:00"},
			bot.FieldDate,
		},
		{
			"終了予定が不正",
			map[string]string{bot.FieldTitle: "会", bot.FieldDate: "2026-09-20", bot.FieldEnds: "そのうち"},
			bot.FieldEnds,
		},
		{
			"リマインド選択肢が不正",
			map[string]string{bot.FieldTitle: "会", bot.FieldDate: "2026-09-20", bot.FieldRemind: "whenever"},
			bot.FieldRemind,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, errs := bot.ParseEventForm(tc.values, now, settings)
			assert.Contains(t, errs, tc.wantField)
		})
	}
}

func TestParseEventFormRemindPresets(t *testing.T) {
	t.Parallel()

	loc := tokyo(t)
	now := time.Date(2026, 9, 12, 10, 0, 0, 0, loc)
	settings := domain.DefaultWorkspaceSettings()

	base := map[string]string{bot.FieldTitle: "会", bot.FieldDate: "2026-09-20", bot.FieldTime: "19:00"}

	tests := []struct {
		name  string
		value string
		want  *domain.ReminderPolicy
	}{
		{
			"前日と3時間前",
			bot.RemindPresetDayBefore,
			&domain.ReminderPolicy{
				Mode:    domain.ReminderModeOffsets,
				Offsets: []time.Duration{24 * time.Hour, 3 * time.Hour},
			},
		},
		{
			"毎日20時",
			bot.RemindPresetDaily20,
			&domain.ReminderPolicy{
				Mode:        domain.ReminderModeInterval,
				Every:       24 * time.Hour,
				At:          domain.TimeOfDay{Hour: 20},
				FinalOffset: domain.DefaultFinalOffset,
			},
		},
		{"なし", bot.RemindPresetNone, &domain.ReminderPolicy{Mode: domain.ReminderModeNone}},
		{"カスタムは変更しない", bot.RemindPresetCustom, nil},
		{"未選択は変更しない", "", nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			values := maps.Clone(base)
			values[bot.FieldRemind] = tc.value

			in, errs := bot.ParseEventForm(values, now, settings)
			assert.Empty(t, errs)
			assert.Equal(t, tc.want, in.ReminderPolicy)
		})
	}
}

func TestParseEventFormAcceptsFormPolicy(t *testing.T) {
	t.Parallel()

	loc := tokyo(t)
	now := time.Date(2026, 9, 12, 10, 0, 0, 0, loc)
	settings := domain.DefaultWorkspaceSettings()

	in, errs := bot.ParseEventForm(map[string]string{
		bot.FieldTitle:  "会",
		bot.FieldDate:   "2026-09-20",
		bot.FieldTime:   "19:00",
		bot.FieldRemind: bot.RemindPresetStandard,
	}, now, settings)

	require.Empty(t, errs)
	require.NotNil(t, in.ReminderPolicy)
	require.NoError(t, in.ReminderPolicy.Validate("reminder_policy"))

	_, err := domain.NewEvent(domain.NewEventParams{
		WorkspaceID:    "66e0a1b2c3d4e5f607182930",
		Title:          in.Title,
		StartsAt:       in.StartsAt,
		EndsAt:         in.EndsAt,
		Organizer:      domain.ChatUserRef{UserID: "U001"},
		ReminderPolicy: *in.ReminderPolicy,
		CreatedVia:     domain.CreatedViaChat,
	}, now)
	require.NoError(t, err)
}
