package runtime_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/provider/adapter"
	"github.com/bear-san/aso-bell/internal/provider/fake"
	"github.com/bear-san/aso-bell/internal/shared/rpcerr"
)

func TestGetInfoReportsAdapterState(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	before, err := h.provider.GetInfo(t.Context(), &asobellv1.GetInfoRequest{})
	require.NoError(t, err)
	assert.False(t, before.GetConnected(), "接続前は connected=false")

	h.start(t)

	after, err := h.provider.GetInfo(t.Context(), &asobellv1.GetInfoRequest{})
	require.NoError(t, err)
	assert.True(t, after.GetConnected())
	assert.Equal(t, asobellv1.ProviderKind_PROVIDER_KIND_SLACK, after.GetKind())
	assert.Equal(t, "test", after.GetVersion())
	assert.Equal(t, "UBOT", after.GetBotUserId())
	assert.True(t, after.GetCapabilities().GetForms())
}

func TestListConnectedWorkspacesReturnsAdapterWorkspaces(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	res, err := h.provider.ListConnectedWorkspaces(t.Context(), &asobellv1.ListConnectedWorkspacesRequest{})
	require.NoError(t, err)
	require.Len(t, res.GetWorkspaces(), 1)
	assert.Equal(t, externalID, res.GetWorkspaces()[0].GetExternalId())
}

func TestMembershipIsIdempotent(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ch := h.createChannel(t, "ev-0920-members")

	added, err := h.provider.AddMember(t.Context(), &asobellv1.AddMemberRequest{
		Workspace: workspace(), ChannelId: ch.GetId(), UserId: guest,
	})
	require.NoError(t, err)
	assert.False(t, added.GetAlreadyMember())

	again, err := h.provider.AddMember(t.Context(), &asobellv1.AddMemberRequest{
		Workspace: workspace(), ChannelId: ch.GetId(), UserId: guest,
	})
	require.NoError(t, err, "既にメンバーでも成功扱い")
	assert.True(t, again.GetAlreadyMember())

	removed, err := h.provider.RemoveMember(t.Context(), &asobellv1.RemoveMemberRequest{
		Workspace: workspace(), ChannelId: ch.GetId(), UserId: guest,
	})
	require.NoError(t, err)
	assert.True(t, removed.GetWasMember())

	notMember, err := h.provider.RemoveMember(t.Context(), &asobellv1.RemoveMemberRequest{
		Workspace: workspace(), ChannelId: ch.GetId(), UserId: guest,
	})
	require.NoError(t, err, "メンバーでなくても成功扱い")
	assert.False(t, notMember.GetWasMember())
}

func TestAdapterErrorsBecomeStatusWithReason(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		fail   error
		code   codes.Code
		reason string
	}{
		{"権限不足", adapter.ErrPermission, codes.PermissionDenied, rpcerr.ReasonBotPermission},
		{"レート制限", adapter.ErrRateLimited, codes.ResourceExhausted, rpcerr.ReasonRateLimited},
		{"チャンネル不明", adapter.ErrNotFound, codes.NotFound, rpcerr.ReasonChannelNotFound},
		{"プラットフォーム停止", adapter.ErrUnavailable, codes.Unavailable, rpcerr.ReasonPlatformUnavailable},
		{"名前衝突", adapter.ErrNameTaken, codes.AlreadyExists, rpcerr.ReasonNameTaken},
		{"ピン上限", adapter.ErrPinLimit, codes.FailedPrecondition, rpcerr.ReasonPinLimit},
		{"非対応", adapter.ErrUnsupported, codes.Unimplemented, rpcerr.ReasonUnsupported},
		{"期限切れ", context.DeadlineExceeded, codes.Unavailable, rpcerr.ReasonPlatformUnavailable},
		{"表に無いエラー", errors.New("boom"), codes.Internal, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			h.adapter.FailNext("CreatePrivateChannel", tc.fail)

			_, err := h.provider.CreatePrivateChannel(t.Context(), &asobellv1.CreatePrivateChannelRequest{
				Workspace: workspace(), Name: "ev-0920-fail",
			})
			require.Error(t, err)
			assert.Equal(t, tc.code, status.Code(err))
			assert.Equal(t, tc.reason, rpcerr.Reason(err))
		})
	}
}

func TestUnknownMessageIsMessageNotFound(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ch := h.createChannel(t, "ev-0920-pin")

	_, err := h.provider.PinMessage(t.Context(), &asobellv1.PinMessageRequest{
		Workspace: workspace(),
		Ref:       &asobellv1.MessageRef{ChannelId: ch.GetId(), MessageId: "M-missing"},
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
	assert.Equal(t, rpcerr.ReasonMessageNotFound, rpcerr.Reason(err), "対象で reason を使い分ける")
}

func TestPostEphemeralFallsBackToDirectMessage(t *testing.T) {
	t.Parallel()

	// ephemeral 非対応の Adapter(Discord 相当)。
	h := newHarness(t, fake.WithAdapterCapabilities(&asobellv1.Capabilities{DirectMessage: true}))
	ch := h.createChannel(t, "ev-0920-dm")

	res, err := h.provider.PostEphemeral(t.Context(), &asobellv1.PostEphemeralRequest{
		Workspace: workspace(),
		ChannelId: ch.GetId(),
		UserId:    guest,
		Message:   &asobellv1.Message{Text: "連携 URL"},
	})
	require.NoError(t, err)
	assert.True(t, res.GetDeliveredViaDm())
	assert.Len(t, h.adapter.CallsOf("SendDirectMessage"), 1)
}

func TestPostEphemeralFallsBackWhenChannelIsGone(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.adapter.FailNext("PostEphemeral", adapter.ErrNotFound)

	res, err := h.provider.PostEphemeral(t.Context(), &asobellv1.PostEphemeralRequest{
		Workspace: workspace(),
		ChannelId: "C-missing",
		UserId:    guest,
		Message:   &asobellv1.Message{Text: "お知らせ"},
	})
	require.NoError(t, err)
	assert.True(t, res.GetDeliveredViaDm())
}

func TestPostEphemeralReturnsDMBlocked(t *testing.T) {
	t.Parallel()

	h := newHarness(t, fake.WithAdapterCapabilities(&asobellv1.Capabilities{DirectMessage: true}))
	h.adapter.FailNext("SendDirectMessage", adapter.ErrDMBlocked)

	_, err := h.provider.PostEphemeral(t.Context(), &asobellv1.PostEphemeralRequest{
		Workspace: workspace(), ChannelId: "C-any", UserId: guest, Message: &asobellv1.Message{Text: "連携 URL"},
	})
	require.Error(t, err, "DM も送れないなら Manager へ返す。機微な内容を公開投稿しないため")
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Equal(t, rpcerr.ReasonDMBlocked, rpcerr.Reason(err))
}

func TestListChannelsPages(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	for _, name := range []string{"ev-a", "ev-b", "ev-c"} {
		h.createChannel(t, name)
	}

	first, err := h.provider.ListChannels(t.Context(), &asobellv1.ListChannelsRequest{
		Workspace: workspace(), PageSize: 2,
	})
	require.NoError(t, err)
	require.Len(t, first.GetChannels(), 2)
	require.NotEmpty(t, first.GetNextPageToken())

	rest, err := h.provider.ListChannels(t.Context(), &asobellv1.ListChannelsRequest{
		Workspace: workspace(), PageSize: 2, PageToken: first.GetNextPageToken(),
	})
	require.NoError(t, err)
	assert.Len(t, rest.GetChannels(), 1)
}

func TestResolveUserReturnsDisplayName(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.adapter.SetUser(adapter.UserInfo{ID: organizer, DisplayName: "けんたろう"})

	res, err := h.provider.ResolveUser(t.Context(), &asobellv1.ResolveUserRequest{
		Workspace: workspace(), UserId: organizer,
	})
	require.NoError(t, err)
	assert.Equal(t, "けんたろう", res.GetUser().GetDisplayName())
}
