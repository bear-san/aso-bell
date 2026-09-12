// Package adapter は Provider プロセス内でチャットツール操作を抽象化する(docs/06-provider.md §2)。
// Slack / Discord の差異はこのインターフェースの実装に閉じ込め、Runtime は種別を意識しない。
package adapter

import (
	"context"
	"time"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
)

// WorkspaceRef は操作対象のワークスペース。Adapter は ExternalID だけを解釈する。
type WorkspaceRef struct {
	// WorkspaceID は Manager 内部 ID。Adapter は使わず、応答でそのまま返すためだけに持つ。
	WorkspaceID string
	ExternalID  string
}

// WorkspaceInfo はチャットツール上のワークスペース(Slack の team、Discord の guild)。
type WorkspaceInfo struct {
	ExternalID string
	Name       string
}

// ChannelInfo はチャットツール上のチャンネル。
type ChannelInfo struct {
	ID        string
	Name      string
	IsPrivate bool
	Archived  bool
}

// MessageRef は投稿済みメッセージの参照。
type MessageRef struct {
	ChannelID string
	MessageID string
}

// UserInfo はチャットツール上のユーザー。
type UserInfo struct {
	ID          string
	DisplayName string
	IsBot       bool
}

// CreateChannelInput はプライベートチャンネルの作成条件。
type CreateChannelInput struct {
	// Name は Manager 側で正規化済み。Adapter は長さ・文字種の最終調整だけを行う。
	Name      string
	Topic     string
	MemberIDs []string
	// CategoryID は Discord のみ有効。空なら未指定。
	CategoryID string
}

// Responder はインタラクションへの応答ハンドル。Adapter がプラットフォームごとに実装する。
type Responder interface {
	// Ack はプラットフォームへの即時応答。1 回だけ呼べる(Slack は 3 秒、Discord も 3 秒の制限)。
	Ack(ctx context.Context, reply *asobellv1.Reply) error
	// Followup は Ack 後の追加応答。
	Followup(ctx context.Context, reply *asobellv1.Reply) error
	// OpenForm はモーダルを開く。forms 非対応の Adapter は ErrUnsupported を返す。
	OpenForm(ctx context.Context, form *asobellv1.Form) error
}

// Command は受信したスラッシュコマンド。
type Command struct {
	// Payload は Manager へそのまま転送する内容。
	Payload *asobellv1.Command
	// Responder は応答先。Runtime が Ack と Followup の順序を決める。
	Responder Responder
}

// Action は受信したボタン押下。
type Action struct {
	Payload   *asobellv1.Action
	Responder Responder
}

// FormSubmission は受信したフォーム送信。
type FormSubmission struct {
	Payload   *asobellv1.FormSubmission
	Responder Responder
}

// InboundSink は Runtime が実装し、Adapter が受信イベントを渡す先(docs/06 §2)。
type InboundSink interface {
	// OnCommand はプラットフォームの制限時間内に返る必要がある。
	OnCommand(ctx context.Context, cmd Command)
	OnAction(ctx context.Context, act Action)
	OnFormSubmit(ctx context.Context, sub FormSubmission)
	// OnWorkspaceDiscovered は接続後に判明したワークスペースを通知する(Discord の GuildCreate)。
	OnWorkspaceDiscovered(ctx context.Context, ws WorkspaceInfo)
	// OnConnectionStateChanged はチャットツールへの接続状態の変化を通知する。Health の切り替えに使う。
	OnConnectionStateChanged(connected bool, cause error)
}

// Adapter はチャットツール 1 つ分の送受信。実装は slack / discord / fake。
type Adapter interface {
	Kind() asobellv1.ProviderKind
	Capabilities() *asobellv1.Capabilities
	// Connect はチャットツールへ接続し、受信イベントを sink へ流す。ブロックしない。
	Connect(ctx context.Context, sink InboundSink) error
	Close(ctx context.Context) error
	// BotUserID はチャットツール上の Bot 自身のユーザー ID。
	BotUserID() string
	// Connected はチャットツールへの接続が確立しているか。
	Connected() bool
	// Workspaces は接続時点で判明しているワークスペース。
	Workspaces() []WorkspaceInfo

	CreatePrivateChannel(ctx context.Context, ws WorkspaceRef, in CreateChannelInput) (ChannelInfo, error)
	// AddMember は既にメンバーなら ErrAlreadyMember を返す。Runtime が成功(already_member)へ変換する。
	AddMember(ctx context.Context, ws WorkspaceRef, channelID, userID string) error
	// RemoveMember はメンバーでなければ ErrNotMember を返す。
	RemoveMember(ctx context.Context, ws WorkspaceRef, channelID, userID string) error
	ArchiveChannel(ctx context.Context, ws WorkspaceRef, channelID, archiveCategoryID string) (ChannelInfo, error)
	ListChannels(ctx context.Context, ws WorkspaceRef, pageToken string, pageSize int) ([]ChannelInfo, string, error)

	PostMessage(ctx context.Context, ws WorkspaceRef, channelID string, msg *asobellv1.Message) (MessageRef, error)
	UpdateMessage(ctx context.Context, ws WorkspaceRef, ref MessageRef, msg *asobellv1.Message) error
	// PostEphemeral は本人にだけ見える投稿。非対応なら ErrUnsupported を返し、Runtime が DM へ切り替える。
	PostEphemeral(ctx context.Context, ws WorkspaceRef, channelID, userID string, msg *asobellv1.Message) error
	SendDirectMessage(ctx context.Context, ws WorkspaceRef, userID string, msg *asobellv1.Message) (MessageRef, error)
	PinMessage(ctx context.Context, ws WorkspaceRef, ref MessageRef) error
	UnpinMessage(ctx context.Context, ws WorkspaceRef, ref MessageRef) error

	ResolveUser(ctx context.Context, ws WorkspaceRef, userID string) (UserInfo, error)
}

// Now は受信時刻の既定値。Adapter がイベントの発生時刻を持たない場合に使う。
func Now() time.Time { return time.Now().UTC() }
