// Package rpcauth は Manager ⇄ Provider 間の共有トークン認証(authorization: Bearer)を
// gRPC インターセプタとして提供する。
package rpcauth

import (
	"context"
	"crypto/subtle"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	headerKey    = "authorization"
	bearerPrefix = "bearer "
)

// MinTokenLength は共有トークンの最小長。短いトークンは推測されうるため起動時に拒否する。
const MinTokenLength = 32

// Skipper は認証を免除する RPC(Health / Reflection など)を判定する。
type Skipper func(fullMethod string) bool

// SkipInfra は gRPC Health と Reflection を認証対象から外す。
func SkipInfra(fullMethod string) bool {
	return strings.HasPrefix(fullMethod, "/grpc.health.v1.Health/") ||
		strings.HasPrefix(fullMethod, "/grpc.reflection.")
}

// Verify は受信メタデータの Bearer トークンを検証する。
// タイミング攻撃でトークン長・内容が漏れないよう定数時間比較を使う。
func Verify(ctx context.Context, token string) error {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "missing metadata")
	}
	for _, v := range md.Get(headerKey) {
		if len(v) < len(bearerPrefix) || !strings.EqualFold(v[:len(bearerPrefix)], bearerPrefix) {
			continue
		}
		got := strings.TrimSpace(v[len(bearerPrefix):])
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1 {
			return nil
		}
		return status.Error(codes.Unauthenticated, "invalid rpc token")
	}
	return status.Error(codes.Unauthenticated, "missing bearer token")
}

// UnaryServerInterceptor は skip に該当しない RPC に対して Verify を要求する。
func UnaryServerInterceptor(token string, skip Skipper) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if skip == nil || !skip(info.FullMethod) {
			if err := Verify(ctx, token); err != nil {
				return nil, err
			}
		}
		return handler(ctx, req)
	}
}

// StreamServerInterceptor は UnaryServerInterceptor のストリーム版。
func StreamServerInterceptor(token string, skip Skipper) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if skip == nil || !skip(info.FullMethod) {
			if err := Verify(ss.Context(), token); err != nil {
				return err
			}
		}
		return handler(srv, ss)
	}
}

// UnaryClientInterceptor はすべての送信 RPC に Bearer トークンを付与する。
func UnaryClientInterceptor(token string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		return invoker(withToken(ctx, token), method, req, reply, cc, opts...)
	}
}

// StreamClientInterceptor は UnaryClientInterceptor のストリーム版。
func StreamClientInterceptor(token string) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return streamer(withToken(ctx, token), desc, cc, method, opts...)
	}
}

func withToken(ctx context.Context, token string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, headerKey, "Bearer "+token)
}
