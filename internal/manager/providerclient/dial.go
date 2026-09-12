package providerclient

import (
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"

	"github.com/bear-san/aso-bell/internal/shared/rpcauth"
)

// providerServiceConfig は冪等な RPC にだけ UNAVAILABLE の自動リトライを設定する(docs/16-grpc.md §2.2)。
// 非冪等な RPC(CreatePrivateChannel、PostMessage など)は二重実行を避けるため、リトライをジョブ層に委ねる。
const providerServiceConfig = `{
  "methodConfig": [
    {
      "name": [
        {"service": "asobell.v1.ProviderService", "method": "GetInfo"},
        {"service": "asobell.v1.ProviderService", "method": "ListConnectedWorkspaces"},
        {"service": "asobell.v1.ProviderService", "method": "AddMember"},
        {"service": "asobell.v1.ProviderService", "method": "RemoveMember"},
        {"service": "asobell.v1.ProviderService", "method": "ArchiveChannel"},
        {"service": "asobell.v1.ProviderService", "method": "ListChannels"},
        {"service": "asobell.v1.ProviderService", "method": "UpdateMessage"},
        {"service": "asobell.v1.ProviderService", "method": "PinMessage"},
        {"service": "asobell.v1.ProviderService", "method": "UnpinMessage"},
        {"service": "asobell.v1.ProviderService", "method": "ResolveUser"}
      ],
      "retryPolicy": {
        "maxAttempts": 3,
        "initialBackoff": "0.2s",
        "maxBackoff": "2s",
        "backoffMultiplier": 2,
        "retryableStatusCodes": ["UNAVAILABLE"]
      }
    }
  ]
}`

// キープアライブは Provider 側の EnforcementPolicy(MinTime 30s)と整合させる(docs/16-grpc.md §5.3)。
const (
	keepaliveTime    = 60 * time.Second
	keepaliveTimeout = 20 * time.Second
)

// DialConfig は Provider への接続設定。
type DialConfig struct {
	// Address は gRPC の解決可能なターゲット(例 "dns:///provider:9091")。
	Address string
	// Token は相互認証の共有トークン(ASOBELL_RPC_TOKEN)。
	Token string
	TLS   rpcauth.TLSFiles
	// ServerName は mTLS 時の検証対象ホスト名。空なら Address のホスト名を使う。
	ServerName string
}

// Dial は Provider への ClientConn を作る。NewClient は I/O を行わないため、Provider 未起動でも成功する。
// 接続の解放は呼び出し元の責務。
func Dial(cfg DialConfig) (*grpc.ClientConn, error) {
	if cfg.Address == "" {
		return nil, errors.New("provider address is empty")
	}

	creds, err := cfg.TLS.TransportCredentials(cfg.ServerName)
	if err != nil {
		return nil, fmt.Errorf("provider transport credentials: %w", err)
	}

	conn, err := grpc.NewClient(
		cfg.Address,
		grpc.WithTransportCredentials(creds),
		grpc.WithChainUnaryInterceptor(rpcauth.UnaryClientInterceptor(cfg.Token)),
		grpc.WithChainStreamInterceptor(rpcauth.StreamClientInterceptor(cfg.Token)),
		grpc.WithDefaultServiceConfig(providerServiceConfig),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                keepaliveTime,
			Timeout:             keepaliveTimeout,
			PermitWithoutStream: true,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("dial provider %s: %w", cfg.Address, err)
	}

	return conn, nil
}
