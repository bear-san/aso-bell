package providerclient_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/manager/providerclient"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
	"github.com/bear-san/aso-bell/internal/provider/fake"
	"github.com/bear-san/aso-bell/internal/shared/rpcerr"
)

func workspace() usecase.WorkspaceRef {
	return usecase.WorkspaceRef{WorkspaceID: "66e0a1b2c3d4e5f607182930", ExternalID: "T0001"}
}

func newClient(t *testing.T, opts ...fake.Option) (*providerclient.Client, *fake.ProviderServer) {
	t.Helper()

	srv := fake.New(opts...)
	conn, stop, err := fake.Serve(srv)
	require.NoError(t, err)
	t.Cleanup(stop)

	return providerclient.New(conn), srv
}

func createChannel(t *testing.T, client *providerclient.Client, name string) usecase.ChannelInfo {
	t.Helper()

	ch, err := client.CreatePrivateChannel(t.Context(), workspace(), usecase.CreateChannelInput{
		Name:      name,
		Topic:     "連絡用",
		MemberIDs: []string{"U001"},
	})
	require.NoError(t, err)

	return ch
}

func TestGetInfo(t *testing.T) {
	client, _ := newClient(t, fake.WithKind(asobellv1.ProviderKind_PROVIDER_KIND_DISCORD))

	info, err := client.GetInfo(t.Context())
	require.NoError(t, err)

	assert.Equal(t, domain.ProviderKindDiscord, info.Kind)
	assert.True(t, info.Connected)
	assert.True(t, info.Capabilities.DirectMessage)
	assert.Equal(t, "UBOT", info.BotUserID)
}

func TestListConnectedWorkspaces(t *testing.T) {
	client, _ := newClient(t, fake.WithWorkspaces(&asobellv1.WorkspaceInfo{ExternalId: "T0001", Name: "遊び場"}))

	list, err := client.ListConnectedWorkspaces(t.Context())
	require.NoError(t, err)

	require.Len(t, list, 1)
	assert.Equal(t, usecase.ProviderWorkspace{ExternalID: "T0001", Name: "遊び場"}, list[0])
}

func TestChannelLifecycle(t *testing.T) {
	client, srv := newClient(t)

	ch := createChannel(t, client, "ev-0920-ボドゲ会")
	assert.True(t, ch.IsPrivate)
	assert.False(t, ch.Archived)

	already, err := client.AddMember(t.Context(), workspace(), ch.ID, "U002")
	require.NoError(t, err)
	assert.False(t, already)

	already, err = client.AddMember(t.Context(), workspace(), ch.ID, "U002")
	require.NoError(t, err)
	assert.True(t, already)

	was, err := client.RemoveMember(t.Context(), workspace(), ch.ID, "U002")
	require.NoError(t, err)
	assert.True(t, was)

	was, err = client.RemoveMember(t.Context(), workspace(), ch.ID, "U002")
	require.NoError(t, err)
	assert.False(t, was)

	archived, err := client.ArchiveChannel(t.Context(), workspace(), ch.ID, "")
	require.NoError(t, err)
	assert.True(t, archived.Archived)

	assert.Len(t, srv.CallsOf("AddMember"), 2)
}

func TestListChannels(t *testing.T) {
	client, _ := newClient(t)
	createChannel(t, client, "ev-0920-会")
	createChannel(t, client, "ev-0921-会")

	channels, next, err := client.ListChannels(t.Context(), workspace(), "", 10)
	require.NoError(t, err)

	assert.Len(t, channels, 2)
	assert.Empty(t, next)
}

func TestMessageLifecycle(t *testing.T) {
	client, srv := newClient(t)
	ch := createChannel(t, client, "ev-0920-会")

	ref, err := client.PostMessage(t.Context(), workspace(), ch.ID, &asobellv1.Message{Text: "募集中"})
	require.NoError(t, err)
	assert.Equal(t, ch.ID, ref.ChannelID)

	require.NoError(t, client.UpdateMessage(t.Context(), workspace(), ref, &asobellv1.Message{Text: "更新後"}))
	require.NoError(t, client.PinMessage(t.Context(), workspace(), ref))
	require.NoError(t, client.UnpinMessage(t.Context(), workspace(), ref))
	require.NoError(t, client.PostEphemeral(t.Context(), workspace(), ch.ID, "U002", &asobellv1.Message{Text: "hi"}))

	dm, err := client.SendDirectMessage(t.Context(), workspace(), "U002", &asobellv1.Message{Text: "連携 URL"})
	require.NoError(t, err)
	assert.NotEmpty(t, dm.MessageID)

	posts := srv.PostsIn(ch.ID)
	require.Len(t, posts, 2)
	assert.Equal(t, "更新後", posts[0].Message.GetText())
	assert.True(t, posts[1].Ephemeral)
}

func TestResolveUser(t *testing.T) {
	client, srv := newClient(t)
	srv.SetUser(&asobellv1.UserInfo{Id: "U002", DisplayName: "けんたろ"})

	user, err := client.ResolveUser(t.Context(), workspace(), "U002")
	require.NoError(t, err)

	assert.Equal(t, usecase.UserInfo{ID: "U002", DisplayName: "けんたろ"}, user)
}

func TestDuplicateChannelNameIsNameTaken(t *testing.T) {
	client, _ := newClient(t)
	createChannel(t, client, "ev-0920-会")

	_, err := client.CreatePrivateChannel(t.Context(), workspace(), usecase.CreateChannelInput{Name: "ev-0920-会"})

	require.ErrorIs(t, err, usecase.ErrNameTaken)
	assert.Contains(t, err.Error(), "create private channel")
}

func TestUnknownChannelIsNotFound(t *testing.T) {
	client, _ := newClient(t)

	_, err := client.AddMember(t.Context(), workspace(), "CZZZ", "U002")

	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestErrorMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		give error
		want error
	}{
		{
			"reason bot permission",
			rpcerr.New(codes.PermissionDenied, rpcerr.ReasonBotPermission, "no", nil),
			usecase.ErrBotPermission,
		},
		{
			"reason rate limited",
			rpcerr.New(codes.ResourceExhausted, rpcerr.ReasonRateLimited, "slow", nil),
			usecase.ErrRateLimited,
		},
		{
			"reason pin limit",
			rpcerr.New(codes.FailedPrecondition, rpcerr.ReasonPinLimit, "full", nil),
			usecase.ErrPinLimit,
		},
		{
			"reason dm blocked",
			rpcerr.New(codes.FailedPrecondition, rpcerr.ReasonDMBlocked, "blocked", nil),
			usecase.ErrDMBlocked,
		},
		{
			"reason unsupported",
			rpcerr.New(codes.Unimplemented, rpcerr.ReasonUnsupported, "no", nil),
			usecase.ErrUnsupported,
		},
		{
			"reason platform unavailable",
			rpcerr.New(codes.Unavailable, rpcerr.ReasonPlatformUnavailable, "down", nil),
			domain.ErrProviderUnavailable,
		},
		{
			"reason message not found",
			rpcerr.New(codes.NotFound, rpcerr.ReasonMessageNotFound, "gone", nil),
			domain.ErrNotFound,
		},
		{"code only unavailable", status.Error(codes.Unavailable, "down"), domain.ErrProviderUnavailable},
		{"code only permission denied", status.Error(codes.PermissionDenied, "no"), usecase.ErrBotPermission},
		{"code only already exists", status.Error(codes.AlreadyExists, "dup"), domain.ErrAlreadyExists},
		{"code only not found", status.Error(codes.NotFound, "gone"), domain.ErrNotFound},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client, srv := newClient(t)
			srv.FailNext("PostMessage", tc.give)

			_, err := client.PostMessage(t.Context(), workspace(), "C001", &asobellv1.Message{Text: "x"})

			require.ErrorIs(t, err, tc.want)
		})
	}
}

func TestUnmappedErrorIsWrapped(t *testing.T) {
	client, srv := newClient(t)
	srv.FailNext("GetInfo", status.Error(codes.Internal, "boom"))

	_, err := client.GetInfo(t.Context())

	require.Error(t, err)
	assert.Equal(t, codes.Internal, rpcerr.Code(err))
	assert.Contains(t, err.Error(), "get info")
}
