package domain

import (
	"strings"
	"time"
	"unicode/utf8"
)

// EventStatus はイベントの状態(docs/04-domain-model.md §2.3.2)。
type EventStatus string

// イベントの状態。
const (
	EventStatusOpen     EventStatus = "open"
	EventStatusEnded    EventStatus = "ended"
	EventStatusCanceled EventStatus = "canceled"
)

// EndReason はイベントが終了・中止に至った理由。
type EndReason string

// 終了・中止の理由。
const (
	EndReasonManual          EndReason = "manual"
	EndReasonAuto            EndReason = "auto"
	EndReasonCanceled        EndReason = "canceled"
	EndReasonProvisionFailed EndReason = "provision_failed"
	EndReasonChannelLost     EndReason = "channel_lost"
)

// CreatedVia はイベントの作成経路。
type CreatedVia string

// イベントの作成経路。
const (
	CreatedViaChat    CreatedVia = "chat"
	CreatedViaConsole CreatedVia = "console"
)

// タイトル・説明の文字数制限(docs/04-domain-model.md §3)。
const (
	TitleMaxLen       = 80
	DescriptionMaxLen = 1000
	LocationMaxLen    = 200
)

// ChatUserRef はチャットツール上のユーザー参照。
type ChatUserRef struct {
	UserID      string
	DisplayName string
}

// ChannelRef はイベント用チャンネルの参照。
type ChannelRef struct {
	ChannelID string
	Name      string
	Archived  bool
}

// MessageRef は投稿済みメッセージの参照。Slack は ts、Discord は snowflake。
type MessageRef struct {
	ChannelID string
	MessageID string
}

// MessageRefs はイベントに紐づく投稿の参照(docs/04-domain-model.md §2.3.1)。
type MessageRefs struct {
	Announcement        *MessageRef
	RecruitAnnouncement *MessageRef
	Summary             *MessageRef
}

// Event は遊びの予定そのもの。
type Event struct {
	ID               string
	WorkspaceID      string
	Title            string
	Description      string
	Location         string
	StartsAt         time.Time
	EndsAt           *time.Time
	Status           EventStatus
	Organizer        ChatUserRef
	OriginChannelID  string
	Channel          ChannelRef
	Messages         MessageRefs
	ReminderPolicy   ReminderPolicy
	ParticipantCount int
	EndedAt          *time.Time
	EndReason        EndReason
	CreatedVia       CreatedVia
	CreatedBy        string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewEventParams は新規イベントの入力。
type NewEventParams struct {
	WorkspaceID     string
	Title           string
	Description     string
	Location        string
	StartsAt        time.Time
	EndsAt          *time.Time
	Organizer       ChatUserRef
	OriginChannelID string
	ReminderPolicy  ReminderPolicy
	CreatedVia      CreatedVia
	CreatedBy       string
}

// EventUpdate は編集で変更するフィールド。nil のフィールドは変更しない。
type EventUpdate struct {
	Title          *string
	Description    *string
	Location       *string
	StartsAt       *time.Time
	EndsAt         *time.Time
	ClearEndsAt    bool
	ReminderPolicy *ReminderPolicy
}

// NewEvent は入力を検証して open 状態のイベントを作る。開始日時は作成時点で未来でなければならない。
func NewEvent(p NewEventParams, now time.Time) (*Event, error) {
	title := strings.TrimSpace(p.Title)
	description := strings.TrimSpace(p.Description)
	location := strings.TrimSpace(p.Location)

	v := &ValidationError{}
	validateTitle(v, title)
	validateDescription(v, description)
	validateLocation(v, location)

	if !IsID(p.WorkspaceID) {
		v.addf("workspace_id", "must be an object id")
	}

	if p.Organizer.UserID == "" {
		v.addf("organizer", "must not be empty")
	}

	if !p.StartsAt.After(now) {
		v.addf("starts_at", "must be in the future")
	}

	validateEndsAt(v, p.StartsAt, p.EndsAt)
	v.merge("reminder_policy", p.ReminderPolicy.Validate("reminder_policy"))

	if p.CreatedVia != CreatedViaChat && p.CreatedVia != CreatedViaConsole {
		v.addf("created_via", "unknown origin %q", string(p.CreatedVia))
	}

	if err := v.err(); err != nil {
		return nil, err
	}

	return &Event{
		WorkspaceID:     p.WorkspaceID,
		Title:           title,
		Description:     description,
		Location:        location,
		StartsAt:        p.StartsAt.UTC(),
		EndsAt:          utcOrNil(p.EndsAt),
		Status:          EventStatusOpen,
		Organizer:       p.Organizer,
		OriginChannelID: p.OriginChannelID,
		ReminderPolicy:  p.ReminderPolicy,
		CreatedVia:      p.CreatedVia,
		CreatedBy:       p.CreatedBy,
		CreatedAt:       now.UTC(),
		UpdatedAt:       now.UTC(),
	}, nil
}

// IsOpen は参加・編集を受け付ける状態かを返す。
func (e *Event) IsOpen() bool {
	return e.Status == EventStatusOpen
}

// Apply は編集を適用する。終了・中止済みのイベントは編集できない。
// 開始日時は編集時に限り過去も許容する(docs/04-domain-model.md §3)。
func (e *Event) Apply(u EventUpdate, now time.Time) error {
	if !e.IsOpen() {
		return ErrEventClosed
	}

	next := *e
	v := &ValidationError{}

	if u.Title != nil {
		next.Title = strings.TrimSpace(*u.Title)
		validateTitle(v, next.Title)
	}

	if u.Description != nil {
		next.Description = strings.TrimSpace(*u.Description)
		validateDescription(v, next.Description)
	}

	if u.Location != nil {
		next.Location = strings.TrimSpace(*u.Location)
		validateLocation(v, next.Location)
	}

	if u.StartsAt != nil {
		next.StartsAt = u.StartsAt.UTC()
	}

	switch {
	case u.ClearEndsAt:
		next.EndsAt = nil
	case u.EndsAt != nil:
		next.EndsAt = utcOrNil(u.EndsAt)
	}

	validateEndsAt(v, next.StartsAt, next.EndsAt)

	if u.ReminderPolicy != nil {
		next.ReminderPolicy = *u.ReminderPolicy
		v.merge("reminder_policy", next.ReminderPolicy.Validate("reminder_policy"))
	}

	if err := v.err(); err != nil {
		return err
	}

	next.UpdatedAt = now.UTC()
	*e = next

	return nil
}

// End はイベントを終了状態にする。すでに終了・中止済みなら何もせず false を返す(冪等)。
func (e *Event) End(reason EndReason, now time.Time) bool {
	return e.close(EventStatusEnded, reason, now)
}

// Cancel はイベントを中止状態にする。すでに終了・中止済みなら何もせず false を返す(冪等)。
func (e *Event) Cancel(reason EndReason, now time.Time) bool {
	return e.close(EventStatusCanceled, reason, now)
}

// AutoEndAt は自動終了の予定時刻を返す。endsAt が未設定なら startsAt から grace 経過後とする。
func (e *Event) AutoEndAt(grace time.Duration) time.Time {
	if e.EndsAt != nil {
		return *e.EndsAt
	}

	return e.StartsAt.Add(grace)
}

func (e *Event) close(status EventStatus, reason EndReason, now time.Time) bool {
	if !e.IsOpen() {
		return false
	}

	at := now.UTC()
	e.Status = status
	e.EndReason = reason
	e.EndedAt = &at
	e.UpdatedAt = at

	return true
}

func validateTitle(v *ValidationError, title string) {
	switch {
	case title == "":
		v.addf("title", "must not be empty")
	case utf8.RuneCountInString(title) > TitleMaxLen:
		v.addf("title", "must not exceed %d characters", TitleMaxLen)
	}
}

func validateDescription(v *ValidationError, description string) {
	if utf8.RuneCountInString(description) > DescriptionMaxLen {
		v.addf("description", "must not exceed %d characters", DescriptionMaxLen)
	}
}

func validateLocation(v *ValidationError, location string) {
	if utf8.RuneCountInString(location) > LocationMaxLen {
		v.addf("location", "must not exceed %d characters", LocationMaxLen)
	}
}

func validateEndsAt(v *ValidationError, startsAt time.Time, endsAt *time.Time) {
	if endsAt != nil && !endsAt.After(startsAt) {
		v.addf("ends_at", "must be after starts_at")
	}
}

func utcOrNil(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}

	utc := t.UTC()

	return &utc
}
