// Package rpcerr は Manager ⇄ Provider 間のエラー契約(docs/16-grpc.md §3)を表す。
// Provider はプラットフォーム固有のエラーをここで定義した reason 付きの gRPC ステータスへ変換し、
// Manager は reason からドメインエラーを復元する。internal/manager と internal/provider は
// 互いに import できないため、契約は共有パッケージに置く。
package rpcerr

import (
	"context"
	"errors"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Domain は ErrorInfo.domain に入れる識別子。
const Domain = "asobell.dev"

// ErrorInfo.reason に入れる値(docs/16-grpc.md §3)。
const (
	ReasonChannelNotFound     = "CHANNEL_NOT_FOUND"
	ReasonMessageNotFound     = "MESSAGE_NOT_FOUND"
	ReasonUserNotFound        = "USER_NOT_FOUND"
	ReasonNameTaken           = "NAME_TAKEN"
	ReasonBotPermission       = "BOT_PERMISSION"
	ReasonRateLimited         = "RATE_LIMITED"
	ReasonPinLimit            = "PIN_LIMIT"
	ReasonDMBlocked           = "DM_BLOCKED"
	ReasonUnsupported         = "UNSUPPORTED"
	ReasonPlatformUnavailable = "PLATFORM_UNAVAILABLE"
	ReasonInvalidInput        = "INVALID_INPUT"
)

// New は reason 付きの gRPC エラーを作る。metadata にはプラットフォームの生エラーコードを入れる。
func New(code codes.Code, reason, message string, metadata map[string]string) error {
	st := status.New(code, message)

	withDetails, err := st.WithDetails(&errdetails.ErrorInfo{
		Reason:   reason,
		Domain:   Domain,
		Metadata: metadata,
	})
	if err != nil {
		// 詳細を付けられない場合でも、コードとメッセージだけは呼び出し元へ伝える。
		return st.Err()
	}

	return withDetails.Err()
}

// Reason は gRPC エラーに含まれる ErrorInfo.reason を返す。無い場合は空文字。
func Reason(err error) string {
	if err == nil {
		return ""
	}

	st, ok := status.FromError(err)
	if !ok {
		return ""
	}

	for _, detail := range st.Details() {
		info, isInfo := detail.(*errdetails.ErrorInfo)
		if isInfo && info.GetDomain() == Domain {
			return info.GetReason()
		}
	}

	return ""
}

// Metadata は ErrorInfo.metadata を返す。無い場合は nil。
func Metadata(err error) map[string]string {
	if err == nil {
		return nil
	}

	st, ok := status.FromError(err)
	if !ok {
		return nil
	}

	for _, detail := range st.Details() {
		info, isInfo := detail.(*errdetails.ErrorInfo)
		if isInfo && info.GetDomain() == Domain {
			return info.GetMetadata()
		}
	}

	return nil
}

// Code は gRPC のステータスコードを返す。gRPC 由来でないエラーは codes.Unknown。
func Code(err error) codes.Code {
	if err == nil {
		return codes.OK
	}

	// context のキャンセルやデッドライン超過は status.FromError が Unknown にしないよう先に判定する。
	if errors.Is(err, context.Canceled) {
		return codes.Canceled
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return codes.DeadlineExceeded
	}

	return status.Code(err)
}
