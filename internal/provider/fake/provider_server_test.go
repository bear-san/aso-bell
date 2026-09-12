package fake_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/provider/fake"
	"github.com/bear-san/aso-bell/internal/shared/rpcerr"
)

const externalID = "T0001"

func newClient(t *testing.T, srv *fake.ProviderServer) asobellv1.ProviderServiceClient {
	t.Helper()

	conn, stop, err := fake.Serve(srv)
	require.NoError(t, err)
	t.Cleanup(stop)

	return asobellv1.NewProviderServiceClient(conn)
}

func workspace() *asobellv1.WorkspaceRef {
	return &asobellv1.WorkspaceRef{WorkspaceId: "66e0a1b2c3d4e5f607182930", ExternalId: externalID}
}

func createChannel(t *testing.T, client asobellv1.ProviderServiceClient, name string) *asobellv1.ChannelInfo {
	t.Helper()

	res, err := client.CreatePrivateChannel(t.Context(), &asobellv1.CreatePrivateChannelRequest{
		Workspace: workspace(),
		Name:      name,
		MemberIds: []string{"U001"},
	})
	require.NoError(t, err)

	return res.GetChannel()
}

func TestGetInfo(t *testing.T) {
	srv := fake.New(fake.WithKind(asobellv1.ProviderKind_PROVIDER_KIND_DISCORD), fake.WithConnected(false))
	client := newClient(t, srv)

	res, err := client.GetInfo(t.Context(), &asobellv1.GetInfoRequest{})
	require.NoError(t, err)

	assert.Equal(t, asobellv1.ProviderKind_PROVIDER_KIND_DISCORD, res.GetKind())
	assert.False(t, res.GetConnected())

	srv.SetConnected(true)

	res, err = client.GetInfo(t.Context(), &asobellv1.GetInfoRequest{})
	require.NoError(t, err)
	assert.True(t, res.GetConnected())
}

func TestListConnectedWorkspaces(t *testing.T) {
	srv := fake.New(fake.WithWorkspaces(&asobellv1.WorkspaceInfo{ExternalId: externalID, Name: "遊び場"}))
	client := newClient(t, srv)

	res, err := client.ListConnectedWorkspaces(t.Context(), &asobellv1.ListConnectedWorkspacesRequest{})
	require.NoError(t, err)

	require.Len(t, res.GetWorkspaces(), 1)
	assert.Equal(t, "遊び場", res.GetWorkspaces()[0].GetName())
}

func TestCreatePrivateChannelRejectsDuplicateName(t *testing.T) {
	client := newClient(t, fake.New())

	ch := createChannel(t, client, "ev-0920-ボドゲ会")
	assert.True(t, ch.GetIsPrivate())

	_, err := client.CreatePrivateChannel(t.Context(), &asobellv1.CreatePrivateChannelRequest{
		Workspace: workspace(),
		Name:      "ev-0920-ボドゲ会",
	})

	assert.Equal(t, codes.AlreadyExists, rpcerr.Code(err))
	assert.Equal(t, rpcerr.ReasonNameTaken, rpcerr.Reason(err))
}

func TestAddAndRemoveMemberAreIdempotent(t *testing.T) {
	srv := fake.New()
	client := newClient(t, srv)
	ch := createChannel(t, client, "ev-0920-会")

	added, err := client.AddMember(t.Context(), &asobellv1.AddMemberRequest{
		Workspace: workspace(),
		ChannelId: ch.GetId(),
		UserId:    "U002",
	})
	require.NoError(t, err)
	assert.False(t, added.GetAlreadyMember())

	again, err := client.AddMember(t.Context(), &asobellv1.AddMemberRequest{
		Workspace: workspace(),
		ChannelId: ch.GetId(),
		UserId:    "U002",
	})
	require.NoError(t, err)
	assert.True(t, again.GetAlreadyMember())

	removed, err := client.RemoveMember(t.Context(), &asobellv1.RemoveMemberRequest{
		Workspace: workspace(),
		ChannelId: ch.GetId(),
		UserId:    "U002",
	})
	require.NoError(t, err)
	assert.True(t, removed.GetWasMember())

	againRemoved, err := client.RemoveMember(t.Context(), &asobellv1.RemoveMemberRequest{
		Workspace: workspace(),
		ChannelId: ch.GetId(),
		UserId:    "U002",
	})
	require.NoError(t, err)
	assert.False(t, againRemoved.GetWasMember())
}

func TestUnknownChannelIsNotFound(t *testing.T) {
	client := newClient(t, fake.New())

	_, err := client.AddMember(t.Context(), &asobellv1.AddMemberRequest{
		Workspace: workspace(),
		ChannelId: "CZZZ",
		UserId:    "U002",
	})

	assert.Equal(t, codes.NotFound, rpcerr.Code(err))
	assert.Equal(t, rpcerr.ReasonChannelNotFound, rpcerr.Reason(err))
}

func TestPostUpdateAndPin(t *testing.T) {
	srv := fake.New()
	client := newClient(t, srv)
	ch := createChannel(t, client, "ev-0920-会")

	posted, err := client.PostMessage(t.Context(), &asobellv1.PostMessageRequest{
		Workspace: workspace(),
		ChannelId: ch.GetId(),
		Message:   &asobellv1.Message{Text: "募集中"},
	})
	require.NoError(t, err)

	_, err = client.UpdateMessage(t.Context(), &asobellv1.UpdateMessageRequest{
		Workspace: workspace(),
		Ref:       posted.GetRef(),
		Message:   &asobellv1.Message{Text: "更新後"},
	})
	require.NoError(t, err)

	_, err = client.PinMessage(t.Context(), &asobellv1.PinMessageRequest{
		Workspace: workspace(),
		Ref:       posted.GetRef(),
	})
	require.NoError(t, err)

	posts := srv.PostsIn(ch.GetId())
	require.Len(t, posts, 1)
	assert.Equal(t, "更新後", posts[0].Message.GetText())
	assert.True(t, posts[0].Pinned)

	_, err = client.UnpinMessage(t.Context(), &asobellv1.UnpinMessageRequest{
		Workspace: workspace(),
		Ref:       posted.GetRef(),
	})
	require.NoError(t, err)
	assert.False(t, srv.PostsIn(ch.GetId())[0].Pinned)
}

func TestUpdateUnknownMessageIsNotFound(t *testing.T) {
	client := newClient(t, fake.New())

	_, err := client.UpdateMessage(t.Context(), &asobellv1.UpdateMessageRequest{
		Workspace: workspace(),
		Ref:       &asobellv1.MessageRef{ChannelId: "C001", MessageId: "M999"},
		Message:   &asobellv1.Message{Text: "x"},
	})

	assert.Equal(t, rpcerr.ReasonMessageNotFound, rpcerr.Reason(err))
}

func TestPostEphemeralUnsupported(t *testing.T) {
	srv := fake.New(fake.WithCapabilities(&asobellv1.Capabilities{DirectMessage: true}))
	client := newClient(t, srv)

	_, err := client.PostEphemeral(t.Context(), &asobellv1.PostEphemeralRequest{
		Workspace: workspace(),
		ChannelId: "C001",
		UserId:    "U002",
		Message:   &asobellv1.Message{Text: "秘密"},
	})

	assert.Equal(t, codes.Unimplemented, rpcerr.Code(err))
	assert.Equal(t, rpcerr.ReasonUnsupported, rpcerr.Reason(err))
}

func TestSendDirectMessage(t *testing.T) {
	srv := fake.New()
	client := newClient(t, srv)

	_, err := client.SendDirectMessage(t.Context(), &asobellv1.SendDirectMessageRequest{
		Workspace: workspace(),
		UserId:    "U002",
		Message:   &asobellv1.Message{Text: "連携 URL"},
	})
	require.NoError(t, err)

	posts := srv.Posts()
	require.Len(t, posts, 1)
	assert.Equal(t, "U002", posts[0].DirectTo)
}

func TestArchiveChannelIsIdempotent(t *testing.T) {
	srv := fake.New()
	client := newClient(t, srv)
	ch := createChannel(t, client, "ev-0920-会")

	for range 2 {
		_, err := client.ArchiveChannel(t.Context(), &asobellv1.ArchiveChannelRequest{
			Workspace: workspace(),
			ChannelId: ch.GetId(),
		})
		require.NoError(t, err)
	}

	stored, ok := srv.Channel(ch.GetId())
	require.True(t, ok)
	assert.True(t, stored.Archived)
}

func TestResolveUser(t *testing.T) {
	srv := fake.New()
	srv.SetUser(&asobellv1.UserInfo{Id: "U002", DisplayName: "けんたろ"})
	client := newClient(t, srv)

	known, err := client.ResolveUser(t.Context(), &asobellv1.ResolveUserRequest{
		Workspace: workspace(),
		UserId:    "U002",
	})
	require.NoError(t, err)
	assert.Equal(t, "けんたろ", known.GetUser().GetDisplayName())

	unknown, err := client.ResolveUser(t.Context(), &asobellv1.ResolveUserRequest{
		Workspace: workspace(),
		UserId:    "U999",
	})
	require.NoError(t, err)
	assert.Equal(t, "U999", unknown.GetUser().GetDisplayName())
}

func TestFailNextIsConsumedOnce(t *testing.T) {
	srv := fake.New()
	client := newClient(t, srv)
	srv.FailNext("PostMessage", rpcerr.New(codes.ResourceExhausted, rpcerr.ReasonRateLimited, "slow down", nil))

	_, err := client.PostMessage(t.Context(), &asobellv1.PostMessageRequest{
		Workspace: workspace(),
		ChannelId: "C001",
		Message:   &asobellv1.Message{Text: "1"},
	})
	assert.Equal(t, rpcerr.ReasonRateLimited, rpcerr.Reason(err))

	_, err = client.PostMessage(t.Context(), &asobellv1.PostMessageRequest{
		Workspace: workspace(),
		ChannelId: "C001",
		Message:   &asobellv1.Message{Text: "2"},
	})
	require.NoError(t, err)

	assert.Len(t, srv.CallsOf("PostMessage"), 2)
}
