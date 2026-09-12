package bot

import (
	"time"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/manager/domain"
)

// Renderer は文言テンプレートを usecase.MessageRenderer として公開するアダプタ。
// usecase は文言そのものを知らずに投稿できる(docs/02-architecture.md §3.5)。
type Renderer struct{}

// Announcement は立ち上げメッセージを組み立てる。
func (Renderer) Announcement(ev *domain.Event, peek time.Duration) *asobellv1.Message {
	return Announcement(ev, peek)
}

// Summary は概要メッセージを組み立てる。
func (Renderer) Summary(ev *domain.Event) *asobellv1.Message {
	return Summary(ev)
}

// Reminder はイベントチャンネル向けのリマインドを組み立てる。
func (Renderer) Reminder(ev *domain.Event, now time.Time, participantIDs []string) *asobellv1.Message {
	return Reminder(ev, now, participantIDs)
}

// RecruitReminder は募集チャンネル向けのリマインドを組み立てる。
func (Renderer) RecruitReminder(ev *domain.Event, now time.Time, peek time.Duration) *asobellv1.Message {
	return RecruitReminder(ev, now, peek)
}

// Closing は終了・中止メッセージを組み立てる。
func (Renderer) Closing(ev *domain.Event) *asobellv1.Message {
	return Closing(ev)
}

// Transition は参加状況の変化メッセージを組み立てる。
func (Renderer) Transition(
	kind domain.TransitionKind,
	userID string,
	count int,
	expiresAt time.Time,
) *asobellv1.Message {
	return Transition(kind, userID, count, expiresAt)
}

// Left は退出メッセージを組み立てる。
func (Renderer) Left(userID string, count int) *asobellv1.Message {
	return Left(userID, count)
}

// RemovedByOrganizer は主催者による除外メッセージを組み立てる。
func (Renderer) RemovedByOrganizer(userID string) *asobellv1.Message {
	return RemovedByOrganizer(userID)
}
