package fake

import (
	"context"
	"fmt"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
)

const bufSize = 1 << 20

// Serve は ProviderService 実装を bufconn 上のインプロセス gRPC サーバーとして起動し、
// 接続済みクライアントと停止関数を返す。テストからネットワークを使わずに RPC を往復させる。
func Serve(srv asobellv1.ProviderServiceServer, opts ...grpc.ServerOption) (*grpc.ClientConn, func(), error) {
	return serve(func(server *grpc.Server) {
		asobellv1.RegisterProviderServiceServer(server, srv)
	}, opts...)
}

// ServeManager は ManagerService 実装を bufconn 上で起動する。Provider Runtime のテストに使う。
func ServeManager(srv asobellv1.ManagerServiceServer, opts ...grpc.ServerOption) (*grpc.ClientConn, func(), error) {
	return serve(func(server *grpc.Server) {
		asobellv1.RegisterManagerServiceServer(server, srv)
	}, opts...)
}

func serve(register func(*grpc.Server), opts ...grpc.ServerOption) (*grpc.ClientConn, func(), error) {
	lis := bufconn.Listen(bufSize)
	server := grpc.NewServer(opts...)
	register(server)

	go func() { _ = server.Serve(lis) }()

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
	)
	if err != nil {
		server.Stop()

		return nil, nil, fmt.Errorf("dial bufconn: %w", err)
	}

	stop := func() {
		_ = conn.Close()
		server.Stop()
		_ = lis.Close()
	}

	return conn, stop, nil
}
