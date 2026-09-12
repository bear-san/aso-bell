// Package usecase は Manager のアプリケーションサービスと、それが必要とする外部依存の
// インターフェース(ポート)を定義する。実装は store / providerclient 側に置く。
package usecase

import (
	"context"
	"time"

	"github.com/bear-san/aso-bell/internal/manager/domain"
)

// Clock は現在時刻の取得元。テストでは固定時刻の Fake を注入する。
type Clock interface {
	Now() time.Time
}

// SystemClock は実時刻を UTC で返す Clock。
type SystemClock struct{}

// Now は現在時刻を UTC で返す。
func (SystemClock) Now() time.Time {
	return time.Now().UTC()
}

// Repository は永続化層の入口。コレクションごとのリポジトリをまとめて注入する。
type Repository interface {
	Meta() MetaRepository
	Workspaces() WorkspaceRepository
	Events() EventRepository
	Participations() ParticipationRepository
	Jobs() JobRepository
	ReminderLogs() ReminderLogRepository
	ConsoleUsers() ConsoleUserRepository
	ChatIdentities() ChatIdentityRepository
	LinkTokens() LinkTokenRepository
	Sessions() SessionRepository
	OAuthStates() OAuthStateRepository
}

// MetaRepository はスキーマ情報と、対になる Provider の状態を保持する。
type MetaRepository interface {
	GetProvider(ctx context.Context) (*domain.ProviderState, error)
	SaveProvider(ctx context.Context, state *domain.ProviderState) error
	SchemaVersion(ctx context.Context) (int, error)
	SetSchemaVersion(ctx context.Context, version int) error
}

// WorkspaceRepository はワークスペースと設定を保持する。
type WorkspaceRepository interface {
	Get(ctx context.Context, id string) (*domain.Workspace, error)
	GetByExternalID(ctx context.Context, provider domain.ProviderKind, externalID string) (*domain.Workspace, error)
	List(ctx context.Context) ([]*domain.Workspace, error)
	// Upsert は (provider, externalId) をキーに登録・名前更新する。既存の設定は保持する。
	Upsert(ctx context.Context, ws *domain.Workspace, now time.Time) (*domain.Workspace, error)
	UpdateSettings(
		ctx context.Context,
		id string,
		settings domain.WorkspaceSettings,
		now time.Time,
	) (*domain.Workspace, error)
}

// EventFilter はイベント一覧の絞り込み条件。
type EventFilter struct {
	WorkspaceID string
	Statuses    []domain.EventStatus
	IDs         []string
	StartsAfter *time.Time
	Limit       int
	Cursor      string
}

// EventPage はカーソル付きのイベント一覧。並び順は startsAt の降順。
type EventPage struct {
	Events     []*domain.Event
	NextCursor string
}

// EventRepository はイベントを保持する。
type EventRepository interface {
	Create(ctx context.Context, ev *domain.Event) (*domain.Event, error)
	Get(ctx context.Context, id string) (*domain.Event, error)
	GetByChannelID(ctx context.Context, channelID string) (*domain.Event, error)
	List(ctx context.Context, filter EventFilter) (EventPage, error)
	// Update は編集・終了で変わるフィールドを保存する。チャンネルとメッセージ参照は含まない。
	Update(ctx context.Context, ev *domain.Event) error
	SetChannel(ctx context.Context, id string, channel domain.ChannelRef, now time.Time) error
	SetMessages(ctx context.Context, id string, messages domain.MessageRefs, now time.Time) error
	AddParticipantCount(ctx context.Context, id string, delta int, now time.Time) error
}

// ParticipationUpsert は参加・チラ見の登録内容。
type ParticipationUpsert struct {
	EventID     string
	WorkspaceID string
	ChatUserID  string
	DisplayName string
	Role        domain.ParticipationRole
	ExpiresAt   *time.Time
	Now         time.Time
}

// ParticipationChange は Upsert の前後のレコード。Before が nil なら新規登録。
type ParticipationChange struct {
	Before *domain.Participation
	After  *domain.Participation
}

// ParticipationRepository は参加レコードを保持する。
type ParticipationRepository interface {
	// Upsert は (eventId, chatUserId) を単一の原子的更新で登録・更新し、更新前後のレコードを返す。
	// 連打時も副作用を 1 度だけ実行できるよう、遷移判定は Before を用いて行う。
	Upsert(ctx context.Context, in ParticipationUpsert) (ParticipationChange, error)
	Get(ctx context.Context, id string) (*domain.Participation, error)
	GetByUser(ctx context.Context, eventID, chatUserID string) (*domain.Participation, error)
	SetStatus(ctx context.Context, id string, status domain.ParticipationStatus, now time.Time) error
	ListByEvent(
		ctx context.Context,
		eventID string,
		statuses ...domain.ParticipationStatus,
	) ([]*domain.Participation, error)
	CountActive(ctx context.Context, eventID string, role domain.ParticipationRole) (int, error)
}

// JobRepository は永続ジョブキュー。
type JobRepository interface {
	// Enqueue は dedupeKey が重複した場合 domain.ErrAlreadyExists を返す。
	Enqueue(ctx context.Context, job domain.Job, now time.Time) (*domain.Job, error)
	Get(ctx context.Context, id string) (*domain.Job, error)
	// Claim は実行可能なジョブを 1 件だけ排他的に取得する。対象が無ければ domain.ErrNotFound を返す。
	Claim(ctx context.Context, now time.Time, lease time.Duration) (*domain.Job, error)
	Complete(ctx context.Context, id string, now time.Time) error
	Retry(ctx context.Context, id string, runAt time.Time, lastErr string, now time.Time) error
	Fail(ctx context.Context, id, lastErr string, now time.Time) error
	Reschedule(ctx context.Context, id string, runAt time.Time, now time.Time) error
	ExtendLease(ctx context.Context, id string, until time.Time, now time.Time) error
	CancelByEvent(ctx context.Context, eventID string, kinds []domain.JobKind, now time.Time) (int, error)
	ListByEvent(ctx context.Context, eventID string, statuses ...domain.JobStatus) ([]*domain.Job, error)
}

// ReminderLogRepository は送信済みリマインドの履歴を保持する。
type ReminderLogRepository interface {
	Append(ctx context.Context, log *domain.ReminderLog) (*domain.ReminderLog, error)
	ListByEvent(ctx context.Context, eventID string) ([]*domain.ReminderLog, error)
	// SentAt はポリシー再生成時に、同じ予定時刻のリマインドを二重送信しないための判定に使う。
	SentAt(ctx context.Context, eventID string, scheduledFor time.Time) (bool, error)
	SentSequence(ctx context.Context, eventID string, sequence int) (bool, error)
}

// ConsoleUserRepository は WebConsole 利用者を保持する。
type ConsoleUserRepository interface {
	Get(ctx context.Context, id string) (*domain.ConsoleUser, error)
	GetByGoogleSub(ctx context.Context, sub string) (*domain.ConsoleUser, error)
	Create(ctx context.Context, user *domain.ConsoleUser) (*domain.ConsoleUser, error)
	TouchLogin(ctx context.Context, id string, now time.Time) error
}

// ChatIdentityRepository はチャットユーザーと ConsoleUser の紐づけを保持する。
type ChatIdentityRepository interface {
	// Link は (workspaceId, chatUserId) が既に別のユーザーへ紐づいていれば
	// domain.ErrIdentityLinkedElsewhere を返す。
	Link(ctx context.Context, identity *domain.ChatIdentity) (*domain.ChatIdentity, error)
	Get(ctx context.Context, workspaceID, chatUserID string) (*domain.ChatIdentity, error)
	ListByUser(ctx context.Context, consoleUserID string) ([]*domain.ChatIdentity, error)
	Unlink(ctx context.Context, id string) error
}

// LinkTokenRepository は `/asobell login` の連携トークンを保持する。
type LinkTokenRepository interface {
	Create(ctx context.Context, token domain.LinkToken) error
	// Get はトークンを消費しない。リンクプレビューによる先取り消費を防ぐため(ADR 0009)。
	Get(ctx context.Context, id string) (*domain.LinkToken, error)
	// Consume は未消費かつ有効期限内のトークンを原子的に消費する。
	Consume(ctx context.Context, id string, now time.Time) (*domain.LinkToken, error)
}

// SessionRepository は WebConsole のセッションを保持する。
type SessionRepository interface {
	Create(ctx context.Context, session domain.Session) error
	Get(ctx context.Context, id string) (*domain.Session, error)
	Touch(ctx context.Context, id string, now time.Time) error
	Delete(ctx context.Context, id string) error
	DeleteByUser(ctx context.Context, userID string) error
}

// OAuthStateRepository は Google ログインの state と PKCE verifier を保持する。
type OAuthStateRepository interface {
	Create(ctx context.Context, state domain.OAuthState) error
	Consume(ctx context.Context, state string, now time.Time) (*domain.OAuthState, error)
}
