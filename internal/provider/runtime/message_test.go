package runtime_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/provider/adapter"
	"github.com/bear-san/aso-bell/internal/shared/rpcerr"
)

func TestMessageLifecycleThroughRPC(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ch := h.createChannel(t, "ev-0920-msg")

	posted, err := h.provider.PostMessage(t.Context(), &asobellv1.PostMessageRequest{
		Workspace: workspace(),
		ChannelId: ch.GetId(),
		Message:   &asobellv1.Message{Text: "集合は 19:00 です"},
	})
	require.NoError(t, err)
	ref := posted.GetRef()
	require.Equal(t, ch.GetId(), ref.GetChannelId())
	require.NotEmpty(t, ref.GetMessageId())

	_, err = h.provider.UpdateMessage(t.Context(), &asobellv1.UpdateMessageRequest{
		Workspace: workspace(),
		Ref:       ref,
		Message:   &asobellv1.Message{Text: "集合は 19:30 に変更"},
	})
	require.NoError(t, err)

	_, err = h.provider.PinMessage(t.Context(), &asobellv1.PinMessageRequest{Workspace: workspace(), Ref: ref})
	require.NoError(t, err)

	pinned := h.adapter.Posts()
	require.Len(t, pinned, 1)
	assert.Equal(t, "集合は 19:30 に変更", pinned[0].Message.GetText())
	assert.True(t, pinned[0].Pinned)

	_, err = h.provider.UnpinMessage(t.Context(), &asobellv1.UnpinMessageRequest{Workspace: workspace(), Ref: ref})
	require.NoError(t, err)

	unpinned := h.adapter.Posts()
	require.Len(t, unpinned, 1)
	assert.False(t, unpinned[0].Pinned)
}

func TestMessageRPCsReportMissingMessage(t *testing.T) {
	t.Parallel()

	missing := &asobellv1.MessageRef{ChannelId: "C-missing", MessageId: "M-missing"}

	tests := []struct {
		name string
		call func(t *testing.T, h *harness) error
	}{
		{"更新", func(t *testing.T, h *harness) error {
			t.Helper()
			_, err := h.provider.UpdateMessage(t.Context(), &asobellv1.UpdateMessageRequest{
				Workspace: workspace(), Ref: missing, Message: &asobellv1.Message{Text: "x"},
			})

			return err
		}},
		{"ピン解除", func(t *testing.T, h *harness) error {
			t.Helper()
			_, err := h.provider.UnpinMessage(t.Context(), &asobellv1.UnpinMessageRequest{
				Workspace: workspace(), Ref: missing,
			})

			return err
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.call(t, newHarness(t))
			require.Error(t, err)
			assert.Equal(t, codes.NotFound, status.Code(err))
			assert.Equal(t, rpcerr.ReasonMessageNotFound, rpcerr.Reason(err))
		})
	}
}

func TestSendDirectMessageReturnsRef(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	res, err := h.provider.SendDirectMessage(t.Context(), &asobellv1.SendDirectMessageRequest{
		Workspace: workspace(),
		UserId:    guest,
		Message:   &asobellv1.Message{Text: "連携が完了しました"},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, res.GetRef().GetMessageId())

	posts := h.adapter.Posts()
	require.Len(t, posts, 1)
	assert.Equal(t, guest, posts[0].DirectTo)
}

func TestSendDirectMessageReportsBlockedUser(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.adapter.FailNext("SendDirectMessage", adapter.ErrDMBlocked)

	_, err := h.provider.SendDirectMessage(t.Context(), &asobellv1.SendDirectMessageRequest{
		Workspace: workspace(), UserId: guest, Message: &asobellv1.Message{Text: "連携 URL"},
	})
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Equal(t, rpcerr.ReasonDMBlocked, rpcerr.Reason(err))
}

func TestArchiveChannelIsIdempotent(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ch := h.createChannel(t, "ev-0920-archive")

	first, err := h.provider.ArchiveChannel(t.Context(), &asobellv1.ArchiveChannelRequest{
		Workspace: workspace(), ChannelId: ch.GetId(), ArchiveCategoryId: "CAT-archive",
	})
	require.NoError(t, err)
	assert.True(t, first.GetChannel().GetArchived())

	again, err := h.provider.ArchiveChannel(t.Context(), &asobellv1.ArchiveChannelRequest{
		Workspace: workspace(), ChannelId: ch.GetId(),
	})
	require.NoError(t, err, "アーカイブ済みでも成功扱い")
	assert.Equal(t, first.GetChannel().GetName(), again.GetChannel().GetName(), "名前は二重に加工しない")
}

func TestArchiveChannelReportsMissingChannel(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	_, err := h.provider.ArchiveChannel(t.Context(), &asobellv1.ArchiveChannelRequest{
		Workspace: workspace(), ChannelId: "C-missing",
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
	assert.Equal(t, rpcerr.ReasonChannelNotFound, rpcerr.Reason(err))
}

func TestShutdownMakesHealthNotServing(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.start(t)

	serving, err := h.health.Check(t.Context(), &healthpb.HealthCheckRequest{})
	require.NoError(t, err)
	require.Equal(t, healthpb.HealthCheckResponse_SERVING, serving.GetStatus())

	h.runtime.Shutdown()

	stopped, err := h.health.Check(t.Context(), &healthpb.HealthCheckRequest{})
	require.NoError(t, err)
	assert.Equal(t, healthpb.HealthCheckResponse_NOT_SERVING, stopped.GetStatus(),
		"Shutdown 後は NOT_SERVING を返し、停止中であることを Compose のヘルスチェックに伝える")
}
