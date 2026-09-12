// Package runtime は Adapter と Manager をつなぐ Provider の中核(docs/06-provider.md §3)。
// ProviderService の実装、受信イベントの転送と ACK 制御、エフェメラル → DM フォールバック、
// Manager との疎通確認、gRPC Health の切り替えを担う。
package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/provider/adapter"
)

// 既定値(docs/06 §3.1、§3.3)。
const (
	// DefaultTimeout は Manager 呼び出しの上限。
	DefaultTimeout = 25 * time.Second
	// DefaultFastTimeout はプラットフォームの 3 秒制限に間に合わせる必要がある呼び出しの上限。
	DefaultFastTimeout = 2500 * time.Millisecond
	// DefaultMinBackoff / DefaultMaxBackoff は Manager 疎通の再試行間隔。
	DefaultMinBackoff = time.Second
	DefaultMaxBackoff = 30 * time.Second
)

// backoffFactor は再試行間隔を指数的に伸ばす倍率。
const backoffFactor = 2

// msgManagerUnavailable は Manager に到達できないときにユーザーへ返す固定文言(docs/06 §3.1)。
const msgManagerUnavailable = "ただいま利用できません。しばらくしてからお試しください"

// Config は Runtime の依存と設定。
type Config struct {
	Adapter adapter.Adapter
	Manager asobellv1.ManagerServiceClient
	Logger  *slog.Logger
	// Version は GetInfo で返す Provider のバージョン。
	Version string
	// Timeout は Manager 呼び出しの上限。
	Timeout time.Duration
	// FastTimeout はフォーム送信のように 3 秒以内に応答しなければならない呼び出しの上限。
	FastTimeout time.Duration
	MinBackoff  time.Duration
	MaxBackoff  time.Duration
}

func (c Config) withDefaults() Config {
	if c.Logger == nil {
		c.Logger = slog.Default()
	}

	if c.Timeout <= 0 {
		c.Timeout = DefaultTimeout
	}

	if c.FastTimeout <= 0 {
		c.FastTimeout = DefaultFastTimeout
	}

	if c.MinBackoff <= 0 {
		c.MinBackoff = DefaultMinBackoff
	}

	if c.MaxBackoff < c.MinBackoff {
		c.MaxBackoff = max(DefaultMaxBackoff, c.MinBackoff)
	}

	return c
}

// Runtime は ProviderService サーバーであり、Adapter の InboundSink でもある。
type Runtime struct {
	asobellv1.UnimplementedProviderServiceServer

	cfg    Config
	health *health.Server
}

// New は Runtime を作る。
func New(cfg Config) *Runtime {
	return &Runtime{cfg: cfg.withDefaults(), health: health.NewServer()}
}

// Register は gRPC サーバーへ ProviderService と Health を登録する。
func (r *Runtime) Register(srv *grpc.Server) {
	asobellv1.RegisterProviderServiceServer(srv, r)
	healthpb.RegisterHealthServer(srv, r.health)
	// チャットツールへ接続するまでは応答できない。Compose の再起動判定はこの状態を見る(docs/06 §3.3)。
	r.health.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
}

// Start は Manager との疎通を確認してからチャットツールへ接続する(docs/06 §3.3)。
// 疎通前に接続すると、受け取ったコマンドを転送できずユーザーに無反応と映るため、順序を守る。
func (r *Runtime) Start(ctx context.Context) error {
	if err := r.reportUntilReachable(ctx); err != nil {
		return err
	}

	if err := r.cfg.Adapter.Connect(ctx, r); err != nil {
		return fmt.Errorf("connect adapter: %w", err)
	}

	return nil
}

// Close はチャットツールとの接続を閉じる。
func (r *Runtime) Close(ctx context.Context) error {
	r.health.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)

	return r.cfg.Adapter.Close(ctx)
}

// Shutdown は Health を停止状態にする。gRPC サーバーを閉じる直前に呼ぶ。
func (r *Runtime) Shutdown() { r.health.Shutdown() }

// reportUntilReachable は ReportWorkspaces が成功するまで backoff しながら再試行する。
// 接続前のワークスペースが空でも呼ぶ。疎通確認そのものが目的のため(docs/06 §3.3)。
func (r *Runtime) reportUntilReachable(ctx context.Context) error {
	wait := r.cfg.MinBackoff

	for {
		err := r.reportWorkspaces(ctx, r.cfg.Adapter.Workspaces())
		if err == nil {
			return nil
		}

		r.cfg.Logger.WarnContext(ctx, "report workspaces failed", "error", err, "retry_in", wait)

		if sleepErr := sleep(ctx, wait); sleepErr != nil {
			return sleepErr
		}

		wait = min(wait*backoffFactor, r.cfg.MaxBackoff)
	}
}

func (r *Runtime) reportWorkspaces(ctx context.Context, workspaces []adapter.WorkspaceInfo) error {
	infos := make([]*asobellv1.WorkspaceInfo, 0, len(workspaces))
	for _, ws := range workspaces {
		infos = append(infos, &asobellv1.WorkspaceInfo{ExternalId: ws.ExternalID, Name: ws.Name})
	}

	ctx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	defer cancel()

	if _, err := r.cfg.Manager.ReportWorkspaces(
		ctx,
		&asobellv1.ReportWorkspacesRequest{Workspaces: infos},
	); err != nil {
		return fmt.Errorf("report workspaces: %w", err)
	}

	return nil
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func workspaceRef(ref *asobellv1.WorkspaceRef) adapter.WorkspaceRef {
	return adapter.WorkspaceRef{WorkspaceID: ref.GetWorkspaceId(), ExternalID: ref.GetExternalId()}
}

func messageRef(ref *asobellv1.MessageRef) adapter.MessageRef {
	return adapter.MessageRef{ChannelID: ref.GetChannelId(), MessageID: ref.GetMessageId()}
}

func protoMessageRef(ref adapter.MessageRef) *asobellv1.MessageRef {
	return &asobellv1.MessageRef{ChannelId: ref.ChannelID, MessageId: ref.MessageID}
}

func protoChannel(ch adapter.ChannelInfo) *asobellv1.ChannelInfo {
	return &asobellv1.ChannelInfo{Id: ch.ID, Name: ch.Name, IsPrivate: ch.IsPrivate, Archived: ch.Archived}
}

// isMissing は「対象が無い」を表すエラーかを返す。エフェメラル投稿の DM フォールバック判定に使う。
func isMissing(err error) bool {
	return errors.Is(err, adapter.ErrUnsupported) || errors.Is(err, adapter.ErrNotFound)
}
