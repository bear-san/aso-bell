package rpcauth_test

import (
	"context"
	"net"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/shared/rpcauth"
)

const token = "0123456789abcdef0123456789abcdef"

type infoServer struct {
	asobellv1.UnimplementedProviderServiceServer
}

func (infoServer) GetInfo(context.Context, *asobellv1.GetInfoRequest) (*asobellv1.GetInfoResponse, error) {
	return &asobellv1.GetInfoResponse{Kind: asobellv1.ProviderKind_PROVIDER_KIND_SLACK}, nil
}

func startServer(t *testing.T) *bufconn.Listener {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(rpcauth.UnaryServerInterceptor(token, rpcauth.SkipInfra)),
		grpc.ChainStreamInterceptor(rpcauth.StreamServerInterceptor(token, rpcauth.SkipInfra)),
	)
	asobellv1.RegisterProviderServiceServer(srv, infoServer{})
	healthpb.RegisterHealthServer(srv, health.NewServer())
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return lis
}

func dial(t *testing.T, lis *bufconn.Listener, opts ...grpc.DialOption) *grpc.ClientConn {
	t.Helper()
	opts = append(opts,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
	)
	conn, err := grpc.NewClient("passthrough:///bufnet", opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestUnaryServerInterceptor(t *testing.T) {
	lis := startServer(t)
	ctx := t.Context()

	t.Run("missing token", func(t *testing.T) {
		client := asobellv1.NewProviderServiceClient(dial(t, lis))
		_, err := client.GetInfo(ctx, &asobellv1.GetInfoRequest{})
		assert.Equal(t, codes.Unauthenticated, status.Code(err))
	})
	t.Run("wrong token", func(t *testing.T) {
		client := asobellv1.NewProviderServiceClient(
			dial(t, lis, grpc.WithUnaryInterceptor(rpcauth.UnaryClientInterceptor(strings.Repeat("x", 32)))),
		)
		_, err := client.GetInfo(ctx, &asobellv1.GetInfoRequest{})
		assert.Equal(t, codes.Unauthenticated, status.Code(err))
	})
	t.Run("token with wrong scheme", func(t *testing.T) {
		client := asobellv1.NewProviderServiceClient(dial(t, lis))
		mdCtx := metadata.AppendToOutgoingContext(ctx, "authorization", "Basic "+token)
		_, err := client.GetInfo(mdCtx, &asobellv1.GetInfoRequest{})
		assert.Equal(t, codes.Unauthenticated, status.Code(err))
	})
	t.Run("valid token via client interceptor", func(t *testing.T) {
		client := asobellv1.NewProviderServiceClient(
			dial(t, lis, grpc.WithUnaryInterceptor(rpcauth.UnaryClientInterceptor(token))),
		)
		res, err := client.GetInfo(ctx, &asobellv1.GetInfoRequest{})
		require.NoError(t, err)
		assert.Equal(t, asobellv1.ProviderKind_PROVIDER_KIND_SLACK, res.GetKind())
	})
	t.Run("health check needs no token", func(t *testing.T) {
		client := healthpb.NewHealthClient(dial(t, lis))
		res, err := client.Check(ctx, &healthpb.HealthCheckRequest{})
		require.NoError(t, err)
		assert.Equal(t, healthpb.HealthCheckResponse_SERVING, res.GetStatus())
	})
}

func TestStreamServerInterceptor(t *testing.T) {
	lis := startServer(t)
	ctx := t.Context()

	t.Run("health watch is skipped", func(t *testing.T) {
		client := healthpb.NewHealthClient(dial(t, lis))
		stream, err := client.Watch(ctx, &healthpb.HealthCheckRequest{})
		require.NoError(t, err)
		res, err := stream.Recv()
		require.NoError(t, err)
		assert.Equal(t, healthpb.HealthCheckResponse_SERVING, res.GetStatus())
	})
	t.Run("stream client interceptor attaches token", func(t *testing.T) {
		srvLis := bufconn.Listen(1 << 20)
		srv := grpc.NewServer(grpc.ChainStreamInterceptor(rpcauth.StreamServerInterceptor(token, nil)))
		healthpb.RegisterHealthServer(srv, health.NewServer())
		go func() { _ = srv.Serve(srvLis) }()
		t.Cleanup(srv.Stop)

		without := healthpb.NewHealthClient(dial(t, srvLis))
		stream, err := without.Watch(ctx, &healthpb.HealthCheckRequest{})
		require.NoError(t, err)
		_, err = stream.Recv()
		assert.Equal(t, codes.Unauthenticated, status.Code(err))

		with := healthpb.NewHealthClient(
			dial(t, srvLis, grpc.WithStreamInterceptor(rpcauth.StreamClientInterceptor(token))),
		)
		stream, err = with.Watch(ctx, &healthpb.HealthCheckRequest{})
		require.NoError(t, err)
		_, err = stream.Recv()
		require.NoError(t, err)
	})
}

func TestVerify(t *testing.T) {
	t.Run("no metadata", func(t *testing.T) {
		err := rpcauth.Verify(t.Context(), token)
		assert.Equal(t, codes.Unauthenticated, status.Code(err))
	})
	t.Run("case insensitive scheme", func(t *testing.T) {
		ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("authorization", "BEARER "+token))
		assert.NoError(t, rpcauth.Verify(ctx, token))
	})
}

func TestTLSFiles(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		var f rpcauth.TLSFiles
		assert.False(t, f.Enabled())
		require.NoError(t, f.Validate())
		creds, err := f.TransportCredentials("")
		require.NoError(t, err)
		assert.Equal(t, "insecure", creds.Info().SecurityProtocol)
	})
	t.Run("partial", func(t *testing.T) {
		f := rpcauth.TLSFiles{CertFile: "cert.pem"}
		assert.True(t, f.Enabled())
		require.Error(t, f.Validate())
		_, err := f.TransportCredentials("")
		assert.Error(t, err)
	})
	t.Run("missing files", func(t *testing.T) {
		f := rpcauth.TLSFiles{
			CertFile: "/nonexistent/cert.pem",
			KeyFile:  "/nonexistent/key.pem",
			CAFile:   "/nonexistent/ca.pem",
		}
		require.NoError(t, f.Validate())
		_, err := f.TransportCredentials("")
		assert.Error(t, err)
	})
}
