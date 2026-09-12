package runtime

import (
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"

	"github.com/bear-san/aso-bell/internal/shared/rpcauth"
)

// managerServiceConfig は冪等な RPC にだけ UNAVAILABLE の自動リトライを設定する(docs/16-grpc.md §2.2)。
// HandleCommand などは副作用を伴うため、二重処理を避けて再試行しない。
const managerServiceConfig = `{
  "methodConfig": [
    {
      "name": [
        {"service": "asobell.v1.ManagerService", "method": "ReportWorkspaces"}
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

// キープアライブは Manager 側の EnforcementPolicy(MinTime 30s)と整合させる(docs/16 §5.3)。
const (
	keepaliveTime    = 60 * time.Second
	keepaliveTimeout = 20 * time.Second
)

// DialConfig は Manager への接続設定。
type DialConfig struct {
	// Address は gRPC の解決可能なターゲット(例 "dns:///manager:9090")。
	Address string
	// Token は相互認証の共有トークン(ASOBELL_RPC_TOKEN)。
	Token string
	TLS   rpcauth.TLSFiles
	// ServerName は mTLS 時の検証対象ホスト名。空なら Address のホスト名を使う。
	ServerName string
}

// Dial は Manager への ClientConn を作る。NewClient は I/O を行わないため、Manager 未起動でも成功する。
// 実際の疎通確認は Runtime.Start の ReportWorkspaces で行う(docs/06-provider.md §3.3)。
func Dial(cfg DialConfig) (*grpc.ClientConn, error) {
	if cfg.Address == "" {
		return nil, errors.New("manager address is empty")
	}

	creds, err := cfg.TLS.TransportCredentials(cfg.ServerName)
	if err != nil {
		return nil, fmt.Errorf("manager transport credentials: %w", err)
	}

	conn, err := grpc.NewClient(
		cfg.Address,
		grpc.WithTransportCredentials(creds),
		grpc.WithChainUnaryInterceptor(rpcauth.UnaryClientInterceptor(cfg.Token)),
		grpc.WithChainStreamInterceptor(rpcauth.StreamClientInterceptor(cfg.Token)),
		grpc.WithDefaultServiceConfig(managerServiceConfig),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                keepaliveTime,
			Timeout:             keepaliveTimeout,
			PermitWithoutStream: true,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("dial manager %s: %w", cfg.Address, err)
	}

	return conn, nil
}
