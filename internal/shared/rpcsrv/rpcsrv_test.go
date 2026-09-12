package rpcsrv_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bear-san/aso-bell/internal/shared/rpcsrv"
)

const method = "/asobell.v1.ManagerService/HandleCommand"

func bufLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer

	return slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), &buf
}

type fakeStream struct {
	grpc.ServerStream
}

func (fakeStream) Context() context.Context { return context.Background() }

func TestUnaryRecoveryConvertsPanicToInternal(t *testing.T) {
	t.Parallel()

	logger, buf := bufLogger()
	interceptor := rpcsrv.UnaryRecovery(logger)

	res, err := interceptor(
		t.Context(), "req", &grpc.UnaryServerInfo{FullMethod: method},
		func(context.Context, any) (any, error) { panic("boom") },
	)

	require.Error(t, err)
	assert.Nil(t, res)
	assert.Equal(t, codes.Internal, status.Code(err))
	assert.NotContains(t, status.Convert(err).Message(), "boom", "panic の内容は呼び出し元へ返さない")
	assert.Contains(t, buf.String(), "rpc panic")
}

func TestUnaryRecoveryPassesResultThrough(t *testing.T) {
	t.Parallel()

	logger, _ := bufLogger()
	want := errors.New("handler failed")

	res, err := rpcsrv.UnaryRecovery(logger)(
		t.Context(), "req", &grpc.UnaryServerInfo{FullMethod: method},
		func(context.Context, any) (any, error) { return "ok", want },
	)

	require.ErrorIs(t, err, want)
	assert.Equal(t, "ok", res)
}

func TestStreamRecoveryConvertsPanicToInternal(t *testing.T) {
	t.Parallel()

	logger, buf := bufLogger()

	err := rpcsrv.StreamRecovery(logger)(
		nil, fakeStream{}, &grpc.StreamServerInfo{FullMethod: method},
		func(any, grpc.ServerStream) error { panic("boom") },
	)

	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
	assert.Contains(t, buf.String(), "rpc panic")
}

func TestStreamRecoveryPassesResultThrough(t *testing.T) {
	t.Parallel()

	logger, _ := bufLogger()
	want := errors.New("handler failed")

	err := rpcsrv.StreamRecovery(logger)(
		nil, fakeStream{}, &grpc.StreamServerInfo{FullMethod: method},
		func(any, grpc.ServerStream) error { return want },
	)

	require.ErrorIs(t, err, want)
}

func TestUnaryLoggingRecordsOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{"成功", nil, "rpc served"},
		{"失敗", status.Error(codes.NotFound, "missing"), "rpc failed"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			logger, buf := bufLogger()

			_, err := rpcsrv.UnaryLogging(logger)(
				t.Context(), "req", &grpc.UnaryServerInfo{FullMethod: method},
				func(context.Context, any) (any, error) { return "ok", tc.err },
			)

			require.ErrorIs(t, err, tc.err)
			assert.Contains(t, buf.String(), tc.want)
			assert.Contains(t, buf.String(), "rpc_method="+method)
		})
	}
}

func TestEnforcementPolicyAllowsClientKeepalive(t *testing.T) {
	t.Parallel()

	policy := rpcsrv.EnforcementPolicy()

	assert.True(t, policy.PermitWithoutStream)
	assert.Positive(t, policy.MinTime, "クライアントの Time: 60s を下回る値にする")
}
