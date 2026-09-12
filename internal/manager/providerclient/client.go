// Package providerclient は usecase.ProviderPort を対になる Provider への gRPC 呼び出しで実装する
// (docs/16-grpc.md §5.1)。gRPC ステータスと ErrorInfo.reason をドメイン・ポートのエラーへ変換し、
// Provider の実装差をユースケース層から隠す。
package providerclient

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
	"github.com/bear-san/aso-bell/internal/shared/rpcerr"
)

// DefaultTimeout は RPC ごとのデッドライン(ASOBELL_PROVIDER_TIMEOUT の既定値)。
const DefaultTimeout = 25 * time.Second

// Client は ProviderService を呼び出す usecase.ProviderPort 実装。
type Client struct {
	rpc     asobellv1.ProviderServiceClient
	timeout time.Duration
}

// Option は Client の設定を変更する。
type Option func(*Client)

// WithTimeout は RPC ごとのデッドラインを変更する。0 以下ならデッドラインを付けない。
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.timeout = d }
}

// New は接続済みの ClientConn から Client を作る。ClientConn の生成と解放は呼び出し元の責務。
func New(conn grpc.ClientConnInterface, opts ...Option) *Client {
	c := &Client{rpc: asobellv1.NewProviderServiceClient(conn), timeout: DefaultTimeout}
	for _, opt := range opts {
		opt(c)
	}

	return c
}

// GetInfo は Provider の種別・能力・接続状態を取得する。
func (c *Client) GetInfo(ctx context.Context) (usecase.ProviderInfo, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	res, err := c.rpc.GetInfo(ctx, &asobellv1.GetInfoRequest{})
	if err != nil {
		return usecase.ProviderInfo{}, mapError(err, "get info")
	}

	return usecase.ProviderInfo{
		Kind:         providerKind(res.GetKind()),
		Capabilities: capabilities(res.GetCapabilities()),
		Version:      res.GetVersion(),
		BotUserID:    res.GetBotUserId(),
		Connected:    res.GetConnected(),
	}, nil
}

// ListConnectedWorkspaces は Provider が把握しているワークスペースを返す。
func (c *Client) ListConnectedWorkspaces(ctx context.Context) ([]usecase.ProviderWorkspace, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	res, err := c.rpc.ListConnectedWorkspaces(ctx, &asobellv1.ListConnectedWorkspacesRequest{})
	if err != nil {
		return nil, mapError(err, "list connected workspaces")
	}

	out := make([]usecase.ProviderWorkspace, 0, len(res.GetWorkspaces()))
	for _, ws := range res.GetWorkspaces() {
		out = append(out, usecase.ProviderWorkspace{ExternalID: ws.GetExternalId(), Name: ws.GetName()})
	}

	return out, nil
}

func (c *Client) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.timeout <= 0 {
		return context.WithCancel(ctx)
	}

	return context.WithTimeout(ctx, c.timeout)
}

func workspaceRef(ws usecase.WorkspaceRef) *asobellv1.WorkspaceRef {
	return &asobellv1.WorkspaceRef{WorkspaceId: ws.WorkspaceID, ExternalId: ws.ExternalID}
}

func channelInfo(ch *asobellv1.ChannelInfo) usecase.ChannelInfo {
	return usecase.ChannelInfo{
		ID:        ch.GetId(),
		Name:      ch.GetName(),
		IsPrivate: ch.GetIsPrivate(),
		Archived:  ch.GetArchived(),
	}
}

func messageRef(ref *asobellv1.MessageRef) domain.MessageRef {
	return domain.MessageRef{ChannelID: ref.GetChannelId(), MessageID: ref.GetMessageId()}
}

func protoMessageRef(ref domain.MessageRef) *asobellv1.MessageRef {
	return &asobellv1.MessageRef{ChannelId: ref.ChannelID, MessageId: ref.MessageID}
}

func providerKind(kind asobellv1.ProviderKind) domain.ProviderKind {
	switch kind {
	case asobellv1.ProviderKind_PROVIDER_KIND_SLACK:
		return domain.ProviderKindSlack
	case asobellv1.ProviderKind_PROVIDER_KIND_DISCORD:
		return domain.ProviderKindDiscord
	case asobellv1.ProviderKind_PROVIDER_KIND_UNSPECIFIED:
		return ""
	default:
		return ""
	}
}

func capabilities(c *asobellv1.Capabilities) domain.Capabilities {
	return domain.Capabilities{
		Forms:         c.GetForms(),
		Ephemeral:     c.GetEphemeral(),
		DirectMessage: c.GetDirectMessage(),
	}
}

// mapError は gRPC エラーをポートのセンチネルへ変換し、呼び出し文脈を付けて返す。
// reason を優先し、reason を持たない Provider 実装や gRPC ランタイム由来のエラーはコードで判定する。
func mapError(err error, op string) error {
	if err == nil {
		return nil
	}

	if mapped := byReason(rpcerr.Reason(err)); mapped != nil {
		return fmt.Errorf("%s: %w", op, mapped)
	}

	if mapped := byCode(rpcerr.Code(err)); mapped != nil {
		return fmt.Errorf("%s: %w", op, mapped)
	}

	return fmt.Errorf("%s: %w", op, err)
}

func byReason(reason string) error {
	switch reason {
	case rpcerr.ReasonChannelNotFound, rpcerr.ReasonMessageNotFound, rpcerr.ReasonUserNotFound:
		return domain.ErrNotFound
	case rpcerr.ReasonNameTaken:
		return usecase.ErrNameTaken
	case rpcerr.ReasonBotPermission:
		return usecase.ErrBotPermission
	case rpcerr.ReasonRateLimited:
		return usecase.ErrRateLimited
	case rpcerr.ReasonPinLimit:
		return usecase.ErrPinLimit
	case rpcerr.ReasonDMBlocked:
		return usecase.ErrDMBlocked
	case rpcerr.ReasonUnsupported:
		return usecase.ErrUnsupported
	case rpcerr.ReasonPlatformUnavailable:
		return domain.ErrProviderUnavailable
	default:
		return nil
	}
}

func byCode(code codes.Code) error {
	switch code { //nolint:exhaustive // 変換するのは docs/16-grpc.md §3 の表に載るコードだけで、残りは元のエラーのまま渡す
	case codes.NotFound:
		return domain.ErrNotFound
	case codes.AlreadyExists:
		return domain.ErrAlreadyExists
	case codes.PermissionDenied:
		return usecase.ErrBotPermission
	case codes.ResourceExhausted:
		return usecase.ErrRateLimited
	case codes.Unimplemented:
		return usecase.ErrUnsupported
	case codes.Unavailable:
		return domain.ErrProviderUnavailable
	default:
		return nil
	}
}

var _ usecase.ProviderPort = (*Client)(nil)
