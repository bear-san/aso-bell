package domain

import "time"

// ReminderTargetKind はリマインドの投稿先種別。
type ReminderTargetKind string

// リマインドの投稿先種別。
const (
	ReminderTargetEvent   ReminderTargetKind = "event"
	ReminderTargetRecruit ReminderTargetKind = "recruit"
)

// ReminderTarget は 1 チャンネルへの投稿結果。片方だけ失敗してもジョブは成功扱いとし、ここに記録する。
type ReminderTarget struct {
	Kind      ReminderTargetKind
	ChannelID string
	MessageID string
	OK        bool
	Error     string
}

// ReminderLog は送信済みリマインドの履歴。ScheduledFor はポリシー再生成時の重複判定に使う。
type ReminderLog struct {
	ID           string
	EventID      string
	Sequence     int
	ScheduledFor time.Time
	SentAt       time.Time
	Targets      []ReminderTarget
}
