package domain

import (
	"regexp"
	"time"
)

// ワークスペース設定の既定値(docs/04-domain-model.md §2.2.1)。
const (
	DefaultTimezone          = "Asia/Tokyo"
	DefaultPeekDuration      = time.Hour
	DefaultAutoEndGrace      = oneDay
	DefaultChannelNamePrefix = "ev-"
)

const (
	channelNamePrefixMaxLen = 16
	defaultStartHour        = 19
)

var channelNamePrefixPattern = regexp.MustCompile(`^[a-z0-9_-]*$`)

// DiscordSettings は Discord 固有のチャンネル配置設定。
type DiscordSettings struct {
	EventCategoryID   string
	ArchiveCategoryID string
}

// WorkspaceSettings はワークスペースごとの動作設定。
type WorkspaceSettings struct {
	RecruitChannelID      string
	Timezone              string
	DefaultReminderPolicy ReminderPolicy
	PeekDuration          time.Duration
	AutoEndGrace          time.Duration
	ChannelNamePrefix     string
	DefaultStartTime      TimeOfDay
	Discord               DiscordSettings
}

// Workspace は Provider が接続している Slack Team / Discord Guild。
type Workspace struct {
	ID         string
	Provider   ProviderKind
	ExternalID string
	Name       string
	Settings   WorkspaceSettings

	CreatedAt time.Time
	UpdatedAt time.Time
}

// DefaultWorkspaceSettings は新規ワークスペースの既定設定を返す。
func DefaultWorkspaceSettings() WorkspaceSettings {
	return WorkspaceSettings{
		Timezone:              DefaultTimezone,
		DefaultReminderPolicy: DefaultReminderPolicy(),
		PeekDuration:          DefaultPeekDuration,
		AutoEndGrace:          DefaultAutoEndGrace,
		ChannelNamePrefix:     DefaultChannelNamePrefix,
		DefaultStartTime:      TimeOfDay{Hour: defaultStartHour},
	}
}

// Location は設定されたタイムゾーンを返す。
func (s *WorkspaceSettings) Location() (*time.Location, error) {
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return nil, err
	}

	return loc, nil
}

// Validate は設定値の不変条件を検査する。
func (s *WorkspaceSettings) Validate() error {
	v := &ValidationError{}

	// time.LoadLocation("") は UTC を返して成功するため、未設定を別に弾く。
	if s.Timezone == "" {
		v.addf("settings.timezone", "must not be empty")
	} else if _, err := time.LoadLocation(s.Timezone); err != nil {
		v.addf("settings.timezone", "unknown timezone %q", s.Timezone)
	}

	if s.PeekDuration <= 0 {
		v.addf("settings.peek_duration", "must be positive")
	}

	if s.AutoEndGrace <= 0 {
		v.addf("settings.auto_end_grace", "must be positive")
	}

	if len(s.ChannelNamePrefix) > channelNamePrefixMaxLen {
		v.addf("settings.channel_name_prefix", "must not exceed %d characters", channelNamePrefixMaxLen)
	}

	if !channelNamePrefixPattern.MatchString(s.ChannelNamePrefix) {
		v.addf("settings.channel_name_prefix", "must consist of lowercase letters, digits, '-' and '_'")
	}

	if !s.DefaultStartTime.Valid() {
		v.addf("settings.default_start_time", "must be HH:MM")
	}

	v.merge("settings.default_reminder_policy", s.DefaultReminderPolicy.Validate("settings.default_reminder_policy"))

	return v.err()
}

// Normalize は未設定のフィールドに既定値を補う。
func (s *WorkspaceSettings) Normalize() {
	defaults := DefaultWorkspaceSettings()

	if s.Timezone == "" {
		s.Timezone = defaults.Timezone
	}

	if s.PeekDuration == 0 {
		s.PeekDuration = defaults.PeekDuration
	}

	if s.AutoEndGrace == 0 {
		s.AutoEndGrace = defaults.AutoEndGrace
	}

	if s.ChannelNamePrefix == "" {
		s.ChannelNamePrefix = defaults.ChannelNamePrefix
	}

	if s.DefaultStartTime == (TimeOfDay{}) {
		s.DefaultStartTime = defaults.DefaultStartTime
	}

	if s.DefaultReminderPolicy.Mode == "" {
		s.DefaultReminderPolicy = defaults.DefaultReminderPolicy
	}
}

// Validate はワークスペースの不変条件を検査する。
func (w Workspace) Validate() error {
	v := &ValidationError{}

	if !w.Provider.Valid() {
		v.addf("provider", "unknown provider %q", string(w.Provider))
	}

	if w.ExternalID == "" {
		v.addf("external_id", "must not be empty")
	}

	if w.Name == "" {
		v.addf("name", "must not be empty")
	}

	v.merge("settings", w.Settings.Validate())

	return v.err()
}
