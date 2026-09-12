package rpcerr_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bear-san/aso-bell/internal/shared/rpcerr"
)

func TestNewCarriesReasonAndMetadata(t *testing.T) {
	err := rpcerr.New(
		codes.AlreadyExists,
		rpcerr.ReasonNameTaken,
		"channel name taken",
		map[string]string{"slack_error": "name_taken"},
	)

	assert.Equal(t, codes.AlreadyExists, rpcerr.Code(err))
	assert.Equal(t, rpcerr.ReasonNameTaken, rpcerr.Reason(err))
	assert.Equal(t, map[string]string{"slack_error": "name_taken"}, rpcerr.Metadata(err))
}

func TestReasonOfPlainStatus(t *testing.T) {
	err := status.Error(codes.Unavailable, "down")

	assert.Empty(t, rpcerr.Reason(err))
	assert.Nil(t, rpcerr.Metadata(err))
	assert.Equal(t, codes.Unavailable, rpcerr.Code(err))
}

func TestReasonOfNonStatusError(t *testing.T) {
	assert.Empty(t, rpcerr.Reason(errors.New("boom")))
	assert.Empty(t, rpcerr.Reason(nil))
	assert.Equal(t, codes.OK, rpcerr.Code(nil))
}

func TestCodeOfContextErrors(t *testing.T) {
	assert.Equal(t, codes.Canceled, rpcerr.Code(context.Canceled))
	assert.Equal(t, codes.DeadlineExceeded, rpcerr.Code(context.DeadlineExceeded))
}
