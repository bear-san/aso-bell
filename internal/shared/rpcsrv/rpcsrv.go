// Package rpcsrv は Manager と Provider の gRPC サーバーに共通するインターセプタと
// キープアライブ設定を提供する(docs/16-grpc.md §5.3)。
package rpcsrv

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"
)

// キープアライブはクライアントの Time: 60s と整合させる(docs/16 §5.3)。
// 下回る ping を送るクライアントを切断しないよう、MinTime は余裕を持って短くしておく。
const (
	keepaliveMinTime = 30 * time.Second
	// GracePeriod は GracefulStop の猶予。超過したら Stop で強制的に閉じる。
	GracePeriod = 10 * time.Second
)

// EnforcementPolicy はクライアントの ping 間隔に対するサーバー側の許容設定を返す。
func EnforcementPolicy() keepalive.EnforcementPolicy {
	return keepalive.EnforcementPolicy{MinTime: keepaliveMinTime, PermitWithoutStream: true}
}

// UnaryRecovery はハンドラの panic を INTERNAL に変換する。
// 1 つの RPC の不具合でプロセス全体(ジョブワーカーを含む)を落とさないため。
func UnaryRecovery(logger *slog.Logger) grpc.UnaryServerInterceptor {
	logger = orDefault(logger)

	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (resp any, err error) { //nolint:nonamedreturns // defer で戻り値を差し替えるため、名前付き戻り値が要る
		defer func() {
			if r := recover(); r != nil {
				logPanic(ctx, logger, info.FullMethod, r)

				err = status.Error(codes.Internal, "internal error")
			}
		}()

		return handler(ctx, req)
	}
}

// StreamRecovery は UnaryRecovery のストリーム版。
func StreamRecovery(logger *slog.Logger) grpc.StreamServerInterceptor {
	logger = orDefault(logger)

	return func(
		srv any,
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) (err error) {
		defer func() {
			if r := recover(); r != nil {
				logPanic(ss.Context(), logger, info.FullMethod, r)

				err = status.Error(codes.Internal, "internal error")
			}
		}()

		return handler(srv, ss)
	}
}

// UnaryLogging は RPC の結果とレイテンシを記録する。失敗だけ警告に上げる。
func UnaryLogging(logger *slog.Logger) grpc.UnaryServerInterceptor {
	logger = orDefault(logger)

	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		started := time.Now()
		resp, err := handler(ctx, req)
		attrs := []any{
			"rpc_method", info.FullMethod,
			"rpc_code", status.Code(err).String(),
			"duration_ms", time.Since(started).Milliseconds(),
		}

		if err != nil {
			logger.WarnContext(ctx, "rpc failed", append(attrs, "error", err)...)

			return resp, err
		}

		logger.DebugContext(ctx, "rpc served", attrs...)

		return resp, nil
	}
}

func logPanic(ctx context.Context, logger *slog.Logger, method string, cause any) {
	logger.ErrorContext(ctx, "rpc panic",
		"rpc_method", method, "panic", cause, "stack", string(debug.Stack()))
}

func orDefault(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default()
	}

	return logger
}
