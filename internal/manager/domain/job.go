package domain

import (
	"fmt"
	"time"
)

// JobKind は非同期ジョブの種別(docs/04-domain-model.md §2.6.1)。
type JobKind string

// ジョブの種別。
const (
	JobReminder       JobKind = "reminder"
	JobPeekExpire     JobKind = "peek_expire"
	JobEventAutoEnd   JobKind = "event_auto_end"
	JobArchiveChannel JobKind = "archive_channel"
)

// JobStatus はジョブの状態。
type JobStatus string

// ジョブの状態。
const (
	JobPending  JobStatus = "pending"
	JobRunning  JobStatus = "running"
	JobDone     JobStatus = "done"
	JobFailed   JobStatus = "failed"
	JobCanceled JobStatus = "canceled"
)

// リトライの既定値(docs/11-scheduler.md §3.2)。
const (
	DefaultMaxAttempts = 5
	baseBackoff        = 30 * time.Second
	maxBackoff         = 30 * time.Minute
	backoffFactor      = 2
)

// JobPayload はジョブ種別ごとの入力。使わないフィールドは空にする。
type JobPayload struct {
	EventID         string
	ParticipationID string
	Sequence        int
	ScheduledFor    time.Time
}

// Job は永続キューに積まれる 1 件のジョブ。
type Job struct {
	ID          string
	Kind        JobKind
	RunAt       time.Time
	DedupeKey   string
	EventID     string
	Payload     JobPayload
	Status      JobStatus
	Attempts    int
	MaxAttempts int
	LeaseUntil  *time.Time
	LastError   string

	CreatedAt  time.Time
	UpdatedAt  time.Time
	FinishedAt *time.Time
}

// NewReminderJob はリマインド投稿ジョブを作る。sequence はイベント内の連番。
func NewReminderJob(eventID string, sequence int, runAt time.Time) Job {
	return newJob(JobReminder, eventID, runAt, fmt.Sprintf("reminder:%s:%d", eventID, sequence), JobPayload{
		EventID:      eventID,
		Sequence:     sequence,
		ScheduledFor: runAt.UTC(),
	})
}

// NewPeekExpireJob はチラ見の期限切れ処理ジョブを作る。再チラ見のたびに peekCount が変わり別ジョブになる。
func NewPeekExpireJob(eventID, participationID string, peekCount int, runAt time.Time) Job {
	return newJob(
		JobPeekExpire,
		eventID,
		runAt,
		fmt.Sprintf("peek:%s:%d", participationID, peekCount),
		JobPayload{EventID: eventID, ParticipationID: participationID},
	)
}

// NewAutoEndJob はイベントの自動終了ジョブを作る。
func NewAutoEndJob(eventID string, runAt time.Time) Job {
	return newJob(JobEventAutoEnd, eventID, runAt, "auto_end:"+eventID, JobPayload{EventID: eventID})
}

// NewArchiveChannelJob はイベントチャンネルのアーカイブジョブを作る。
func NewArchiveChannelJob(eventID string, runAt time.Time) Job {
	return newJob(JobArchiveChannel, eventID, runAt, "archive:"+eventID, JobPayload{EventID: eventID})
}

// Backoff は attempts 回目の失敗後に待つ時間を返す(30s * 2^(attempts-1)、上限 30 分)。
func Backoff(attempts int) time.Duration {
	d := baseBackoff

	for range max(attempts-1, 0) {
		d *= backoffFactor
		if d >= maxBackoff {
			return maxBackoff
		}
	}

	return d
}

// Exhausted は再試行回数を使い切ったかを返す。
func (j *Job) Exhausted() bool {
	return j.Attempts >= j.MaxAttempts
}

func newJob(kind JobKind, eventID string, runAt time.Time, dedupeKey string, payload JobPayload) Job {
	return Job{
		Kind:        kind,
		RunAt:       runAt.UTC(),
		DedupeKey:   dedupeKey,
		EventID:     eventID,
		Payload:     payload,
		Status:      JobPending,
		MaxAttempts: DefaultMaxAttempts,
	}
}
