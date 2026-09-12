package runtime

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"

	"github.com/bear-san/aso-bell/internal/provider/adapter"
	"github.com/bear-san/aso-bell/internal/shared/rpcerr"
)

// notFound は ErrNotFound をどの reason へ変換するかを表す。対象によって Manager 側の扱いが変わるため。
type notFound string

const (
	notFoundChannel notFound = rpcerr.ReasonChannelNotFound
	notFoundMessage notFound = rpcerr.ReasonMessageNotFound
	notFoundUser    notFound = rpcerr.ReasonUserNotFound
)

// toStatus は Adapter のセンチネルを gRPC ステータスへ変換する(docs/16-grpc.md §3)。
// 表に無いエラーは Internal にする。Manager 側はコードで判定して呼び出し文脈を付け直す。
func toStatus(err error, subject notFound) error {
	if err == nil {
		return nil
	}

	code, reason := classify(err, subject)

	return rpcerr.New(code, reason, err.Error(), nil)
}

func classify(err error, subject notFound) (codes.Code, string) {
	switch {
	case errors.Is(err, adapter.ErrNotFound):
		return codes.NotFound, string(subject)
	case errors.Is(err, adapter.ErrNameTaken):
		return codes.AlreadyExists, rpcerr.ReasonNameTaken
	case errors.Is(err, adapter.ErrPermission):
		return codes.PermissionDenied, rpcerr.ReasonBotPermission
	case errors.Is(err, adapter.ErrRateLimited):
		return codes.ResourceExhausted, rpcerr.ReasonRateLimited
	case errors.Is(err, adapter.ErrPinLimit):
		return codes.FailedPrecondition, rpcerr.ReasonPinLimit
	case errors.Is(err, adapter.ErrDMBlocked):
		return codes.FailedPrecondition, rpcerr.ReasonDMBlocked
	case errors.Is(err, adapter.ErrUnsupported):
		return codes.Unimplemented, rpcerr.ReasonUnsupported
	case errors.Is(err, adapter.ErrUnavailable), errors.Is(err, context.DeadlineExceeded):
		return codes.Unavailable, rpcerr.ReasonPlatformUnavailable
	default:
		return codes.Internal, ""
	}
}
