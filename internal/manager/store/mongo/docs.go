package mongo

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type reminderPolicyDoc struct {
	Mode        string   `bson:"mode"`
	Offsets     []string `bson:"offsets,omitempty"`
	Every       string   `bson:"every,omitempty"`
	At          string   `bson:"at,omitempty"`
	FinalOffset string   `bson:"finalOffset,omitempty"`
}

type discordSettingsDoc struct {
	EventCategoryID   string `bson:"eventCategoryId"`
	ArchiveCategoryID string `bson:"archiveCategoryId"`
}

type workspaceSettingsDoc struct {
	RecruitChannelID      string             `bson:"recruitChannelId"`
	Timezone              string             `bson:"timezone"`
	DefaultReminderPolicy reminderPolicyDoc  `bson:"defaultReminderPolicy"`
	PeekDuration          string             `bson:"peekDuration"`
	AutoEndGrace          string             `bson:"autoEndGrace"`
	ChannelNamePrefix     string             `bson:"channelNamePrefix"`
	DefaultStartTime      string             `bson:"defaultStartTime"`
	Discord               discordSettingsDoc `bson:"discord"`
}

type workspaceDoc struct {
	ID         bson.ObjectID        `bson:"_id,omitempty"`
	Provider   string               `bson:"provider"`
	ExternalID string               `bson:"externalId"`
	Name       string               `bson:"name"`
	Settings   workspaceSettingsDoc `bson:"settings"`
	CreatedAt  time.Time            `bson:"createdAt"`
	UpdatedAt  time.Time            `bson:"updatedAt"`
}

type chatUserRefDoc struct {
	UserID      string `bson:"userId"`
	DisplayName string `bson:"displayName"`
}

type channelRefDoc struct {
	ChannelID string `bson:"channelId"`
	Name      string `bson:"name"`
	Archived  bool   `bson:"archived"`
}

type messageRefDoc struct {
	ChannelID string `bson:"channelId"`
	MessageID string `bson:"messageId"`
}

type messageRefsDoc struct {
	Announcement        *messageRefDoc `bson:"announcement,omitempty"`
	RecruitAnnouncement *messageRefDoc `bson:"recruitAnnouncement,omitempty"`
	Summary             *messageRefDoc `bson:"summary,omitempty"`
}

type eventDoc struct {
	ID               bson.ObjectID     `bson:"_id,omitempty"`
	WorkspaceID      bson.ObjectID     `bson:"workspaceId"`
	Title            string            `bson:"title"`
	Description      string            `bson:"description"`
	Location         string            `bson:"location"`
	StartsAt         time.Time         `bson:"startsAt"`
	EndsAt           *time.Time        `bson:"endsAt"`
	Status           string            `bson:"status"`
	Organizer        chatUserRefDoc    `bson:"organizer"`
	OriginChannelID  string            `bson:"originChannelId"`
	Channel          channelRefDoc     `bson:"channel"`
	Messages         messageRefsDoc    `bson:"messages"`
	ReminderPolicy   reminderPolicyDoc `bson:"reminderPolicy"`
	ParticipantCount int               `bson:"participantCount"`
	EndedAt          *time.Time        `bson:"endedAt"`
	EndReason        string            `bson:"endReason"`
	CreatedVia       string            `bson:"createdVia"`
	CreatedBy        string            `bson:"createdBy"`
	CreatedAt        time.Time         `bson:"createdAt"`
	UpdatedAt        time.Time         `bson:"updatedAt"`
}

type participationDoc struct {
	ID          bson.ObjectID `bson:"_id,omitempty"`
	EventID     bson.ObjectID `bson:"eventId"`
	WorkspaceID bson.ObjectID `bson:"workspaceId"`
	ChatUserID  string        `bson:"chatUserId"`
	DisplayName string        `bson:"displayName"`
	Role        string        `bson:"role"`
	Status      string        `bson:"status"`
	JoinedAt    time.Time     `bson:"joinedAt"`
	ExpiresAt   *time.Time    `bson:"expiresAt"`
	PeekCount   int           `bson:"peekCount"`
	UpdatedAt   time.Time     `bson:"updatedAt"`
}

type jobPayloadDoc struct {
	EventID         string    `bson:"eventId,omitempty"`
	ParticipationID string    `bson:"participationId,omitempty"`
	Sequence        int       `bson:"sequence,omitempty"`
	ScheduledFor    time.Time `bson:"scheduledFor,omitempty"`
}

type jobDoc struct {
	ID          bson.ObjectID  `bson:"_id,omitempty"`
	Kind        string         `bson:"kind"`
	RunAt       time.Time      `bson:"runAt"`
	DedupeKey   string         `bson:"dedupeKey,omitempty"`
	EventID     *bson.ObjectID `bson:"eventId"`
	Payload     jobPayloadDoc  `bson:"payload"`
	Status      string         `bson:"status"`
	Attempts    int            `bson:"attempts"`
	MaxAttempts int            `bson:"maxAttempts"`
	LeaseUntil  *time.Time     `bson:"leaseUntil"`
	LastError   string         `bson:"lastError"`
	CreatedAt   time.Time      `bson:"createdAt"`
	UpdatedAt   time.Time      `bson:"updatedAt"`
	FinishedAt  *time.Time     `bson:"finishedAt"`
}

type reminderTargetDoc struct {
	Kind      string `bson:"kind"`
	ChannelID string `bson:"channelId"`
	MessageID string `bson:"messageId"`
	OK        bool   `bson:"ok"`
	Error     string `bson:"error,omitempty"`
}

type reminderLogDoc struct {
	ID           bson.ObjectID       `bson:"_id,omitempty"`
	EventID      bson.ObjectID       `bson:"eventId"`
	Sequence     int                 `bson:"sequence"`
	ScheduledFor time.Time           `bson:"scheduledFor"`
	SentAt       time.Time           `bson:"sentAt"`
	Targets      []reminderTargetDoc `bson:"targets"`
}

type consoleUserDoc struct {
	ID          bson.ObjectID `bson:"_id,omitempty"`
	GoogleSub   string        `bson:"googleSub"`
	Email       string        `bson:"email"`
	Name        string        `bson:"name"`
	Picture     string        `bson:"picture"`
	CreatedVia  string        `bson:"createdVia"`
	CreatedAt   time.Time     `bson:"createdAt"`
	LastLoginAt time.Time     `bson:"lastLoginAt"`
}

type chatIdentityDoc struct {
	ID                  bson.ObjectID `bson:"_id,omitempty"`
	ConsoleUserID       bson.ObjectID `bson:"consoleUserId"`
	WorkspaceID         bson.ObjectID `bson:"workspaceId"`
	Provider            string        `bson:"provider"`
	WorkspaceExternalID string        `bson:"workspaceExternalId"`
	ChatUserID          string        `bson:"chatUserId"`
	DisplayName         string        `bson:"displayName"`
	LinkedAt            time.Time     `bson:"linkedAt"`
}

type linkTokenDoc struct {
	ID          string        `bson:"_id"`
	WorkspaceID bson.ObjectID `bson:"workspaceId"`
	ChatUserID  string        `bson:"chatUserId"`
	DisplayName string        `bson:"displayName"`
	ExpiresAt   time.Time     `bson:"expiresAt"`
	ConsumedAt  *time.Time    `bson:"consumedAt"`
}

type sessionDoc struct {
	ID         string        `bson:"_id"`
	UserID     bson.ObjectID `bson:"userId"`
	CSRFToken  string        `bson:"csrfToken"`
	CreatedAt  time.Time     `bson:"createdAt"`
	ExpiresAt  time.Time     `bson:"expiresAt"`
	LastSeenAt time.Time     `bson:"lastSeenAt"`
}

type oauthStateDoc struct {
	ID           string    `bson:"_id"`
	CodeVerifier string    `bson:"codeVerifier"`
	RedirectTo   string    `bson:"redirectTo"`
	ExpiresAt    time.Time `bson:"expiresAt"`
}

type capabilitiesDoc struct {
	Forms         bool `bson:"forms"`
	Ephemeral     bool `bson:"ephemeral"`
	DirectMessage bool `bson:"directMessage"`
}

type providerDoc struct {
	ID           string          `bson:"_id"`
	Kind         string          `bson:"kind"`
	Address      string          `bson:"address"`
	Capabilities capabilitiesDoc `bson:"capabilities"`
	Version      string          `bson:"version"`
	BotUserID    string          `bson:"botUserId"`
	Connected    bool            `bson:"connected"`
	Status       string          `bson:"status"`
	Failures     int             `bson:"failures"`
	FirstSeenAt  time.Time       `bson:"firstSeenAt"`
	LastSeenAt   time.Time       `bson:"lastSeenAt"`
	UpdatedAt    time.Time       `bson:"updatedAt"`
}

type schemaDoc struct {
	ID      string `bson:"_id"`
	Version int    `bson:"version"`
}
