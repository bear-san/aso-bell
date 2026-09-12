package app

import (
	"context"
	"fmt"
	"net"
	"time"

	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/bear-san/aso-bell/internal/provider/config"
)

// healthcheckTimeout は自プロセスへの問い合わせの上限。
const healthcheckTimeout = 5 * time.Second

// Healthcheck は自プロセスの gRPC Health を叩く(`provider-* healthcheck`)。
// distroless イメージにはシェルも curl も無いため、Compose の healthcheck はこれを使う(docs/12 §4)。
func Healthcheck(ctx context.Context, cfg *config.Common) error {
	creds, err := cfg.RPCTLS.TransportCredentials("localhost")
	if err != nil {
		return fmt.Errorf("rpc transport credentials: %w", err)
	}

	conn, err := grpc.NewClient(localAddr(cfg.ListenAddr), grpc.WithTransportCredentials(creds))
	if err != nil {
		return fmt.Errorf("dial self: %w", err)
	}

	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(ctx, healthcheckTimeout)
	defer cancel()

	res, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
	if err != nil {
		return fmt.Errorf("health check: %w", err)
	}

	if res.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		return fmt.Errorf("health check: status is %s", res.GetStatus())
	}

	return nil
}

// localAddr は待ち受けアドレス(":9091" など)を自プロセスへの接続先へ変換する。
func localAddr(listenAddr string) string {
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return listenAddr
	}

	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}

	return net.JoinHostPort(host, port)
}
