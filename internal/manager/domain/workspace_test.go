package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bear-san/aso-bell/internal/manager/domain"
)

func TestDefaultWorkspaceSettings(t *testing.T) {
	t.Parallel()

	s := domain.DefaultWorkspaceSettings()

	assert.Equal(t, domain.DefaultTimezone, s.Timezone)
	assert.Equal(t, time.Hour, s.PeekDuration)
	assert.Equal(t, 24*time.Hour, s.AutoEndGrace)
	assert.Equal(t, "ev-", s.ChannelNamePrefix)
	assert.Equal(t, "19:00", s.DefaultStartTime.String())
	require.NoError(t, s.Validate())

	loc, err := s.Location()
	require.NoError(t, err)
	assert.Equal(t, "Asia/Tokyo", loc.String())
}

func TestWorkspaceSettingsNormalize(t *testing.T) {
	t.Parallel()

	s := domain.WorkspaceSettings{RecruitChannelID: "C0999"}
	s.Normalize()

	assert.Equal(t, domain.DefaultWorkspaceSettings().Timezone, s.Timezone)
	assert.Equal(t, domain.DefaultWorkspaceSettings().DefaultReminderPolicy, s.DefaultReminderPolicy)
	assert.Equal(t, "C0999", s.RecruitChannelID)
	require.NoError(t, s.Validate())
}

func TestWorkspaceSettingsValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(s *domain.WorkspaceSettings)
		wantField string
	}{
		{
			name:      "unknown timezone",
			mutate:    func(s *domain.WorkspaceSettings) { s.Timezone = "Mars/Olympus" },
			wantField: "settings.timezone",
		},
		{
			name:      "zero peek duration",
			mutate:    func(s *domain.WorkspaceSettings) { s.PeekDuration = 0 },
			wantField: "settings.peek_duration",
		},
		{
			name:      "negative auto end grace",
			mutate:    func(s *domain.WorkspaceSettings) { s.AutoEndGrace = -time.Hour },
			wantField: "settings.auto_end_grace",
		},
		{
			name:      "uppercase prefix",
			mutate:    func(s *domain.WorkspaceSettings) { s.ChannelNamePrefix = "EV-" },
			wantField: "settings.channel_name_prefix",
		},
		{
			name:      "long prefix",
			mutate:    func(s *domain.WorkspaceSettings) { s.ChannelNamePrefix = "aaaaaaaaaaaaaaaaa" },
			wantField: "settings.channel_name_prefix",
		},
		{
			name:      "bad start time",
			mutate:    func(s *domain.WorkspaceSettings) { s.DefaultStartTime = domain.TimeOfDay{Hour: 24} },
			wantField: "settings.default_start_time",
		},
		{
			name: "bad default policy",
			mutate: func(s *domain.WorkspaceSettings) {
				s.DefaultReminderPolicy = domain.ReminderPolicy{Mode: domain.ReminderModeOffsets}
			},
			wantField: "settings.default_reminder_policy.offsets",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := domain.DefaultWorkspaceSettings()
			tc.mutate(&s)

			var verr *domain.ValidationError

			require.ErrorAs(t, s.Validate(), &verr)
			assert.True(t, verr.Has(tc.wantField), "want field %q in %v", tc.wantField, verr.Fields)
		})
	}
}

func TestWorkspaceValidate(t *testing.T) {
	t.Parallel()

	ws := domain.Workspace{
		Provider:   domain.ProviderKindDiscord,
		ExternalID: "123456789012345678",
		Name:       "あそび部",
		Settings:   domain.DefaultWorkspaceSettings(),
	}
	require.NoError(t, ws.Validate())

	broken := domain.Workspace{Provider: "matrix"}

	var verr *domain.ValidationError

	require.ErrorAs(t, broken.Validate(), &verr)
	assert.True(t, verr.Has("provider"))
	assert.True(t, verr.Has("external_id"))
	assert.True(t, verr.Has("name"))
	assert.True(t, verr.Has("settings.timezone"))
}

func TestProviderKindValid(t *testing.T) {
	t.Parallel()

	assert.True(t, domain.ProviderKindSlack.Valid())
	assert.True(t, domain.ProviderKindDiscord.Valid())
	assert.False(t, domain.ProviderKind("matrix").Valid())
}

func TestDecideProviderStatus(t *testing.T) {
	t.Parallel()

	threshold := domain.DefaultProviderOfflineAfterFailures

	assert.Equal(t, domain.ProviderStatusOnline, domain.DecideProviderStatus(true, 0, threshold))
	assert.Equal(t, domain.ProviderStatusOnline, domain.DecideProviderStatus(true, threshold-1, threshold))
	assert.Equal(t, domain.ProviderStatusOffline, domain.DecideProviderStatus(true, threshold, threshold))
	assert.Equal(t, domain.ProviderStatusOffline, domain.DecideProviderStatus(false, 0, threshold))
}
