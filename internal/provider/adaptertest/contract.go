// Package adaptertest は adapter.Adapter 実装が満たすべき契約テストを提供する(docs/06-provider.md §8)。
// fake.Adapter と各プラットフォームの Adapter を同じテストに通すことで、Fake と実装の乖離を防ぐ。
package adaptertest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/provider/adapter"
)

// テストで使う固定値。
const (
	organizer = "U0123"
	guest     = "U0GUEST"
	// WorkspaceID は Manager 内部 ID を模した ObjectID。Adapter は解釈しない。
	WorkspaceID = "66e0a1b2c3d4e5f607182930"
	// WorkspaceExternalID はチャットツール上のワークスペース ID。
	WorkspaceExternalID = "T0123"
)

// Workspace は契約テストが操作するワークスペース。
func Workspace() adapter.WorkspaceRef {
	return adapter.WorkspaceRef{WorkspaceID: WorkspaceID, ExternalID: WorkspaceExternalID}
}

// Factory は契約テスト 1 件ごとに新しい Adapter を用意する。
type Factory func(t *testing.T) adapter.Adapter

// sink は契約テストで受信イベントを捨てる InboundSink。
type sink struct{}

func (sink) OnCommand(context.Context, adapter.Command)                   {}
func (sink) OnAction(context.Context, adapter.Action)                     {}
func (sink) OnFormSubmit(context.Context, adapter.FormSubmission)         {}
func (sink) OnWorkspaceDiscovered(context.Context, adapter.WorkspaceInfo) {}
func (sink) OnConnectionStateChanged(bool, error)                         {}

// RunContract は Adapter 実装が docs/06 §2 の契約を満たすかを検証する。
func RunContract(t *testing.T, newAdapter Factory) {
	t.Helper()

	tests := map[string]func(*testing.T, adapter.Adapter){
		"接続":                   testConnect,
		"チャンネル作成":              testCreateChannel,
		"同名チャンネルは作れない":         testDuplicateChannelName,
		"メンバーの追加と除外":           testMembership,
		"存在しないチャンネルは NotFound": testUnknownChannel,
		"アーカイブは冪等":             testArchive,
		"チャンネル一覧のページング":        testListChannels,
		"投稿と更新":                testMessages,
		"ピン留め":                 testPins,
		"DM":                   testDirectMessage,
		"ユーザー解決":               testResolveUser,
	}

	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			a := newAdapter(t)
			connect(t, a)
			run(t, a)
		})
	}
}

func connect(t *testing.T, a adapter.Adapter) {
	t.Helper()

	require.NoError(t, a.Connect(t.Context(), sink{}))
	t.Cleanup(func() { _ = a.Close(context.WithoutCancel(t.Context())) })
}

func testConnect(t *testing.T, a adapter.Adapter) {
	assert.True(t, a.Connected())
	assert.NotEqual(t, asobellv1.ProviderKind_PROVIDER_KIND_UNSPECIFIED, a.Kind())
	assert.NotEmpty(t, a.BotUserID())
	assert.NotEmpty(t, a.Workspaces())
}

// createChannel はテスト用のチャンネルを 1 つ作る。
func createChannel(t *testing.T, a adapter.Adapter, name string) adapter.ChannelInfo {
	t.Helper()

	ch, err := a.CreatePrivateChannel(t.Context(), Workspace(), adapter.CreateChannelInput{
		Name:      name,
		Topic:     "ボドゲ会",
		MemberIDs: []string{organizer},
	})
	require.NoError(t, err)

	return ch
}

func testCreateChannel(t *testing.T, a adapter.Adapter) {
	ch := createChannel(t, a, "ev-0920-boardgame")

	assert.NotEmpty(t, ch.ID)
	assert.Equal(t, "ev-0920-boardgame", ch.Name)
	assert.True(t, ch.IsPrivate)
	assert.False(t, ch.Archived)

	// 主催者は作成時にメンバーへ含まれているため、追加すると ErrAlreadyMember になる。
	require.ErrorIs(t, a.AddMember(t.Context(), Workspace(), ch.ID, organizer), adapter.ErrAlreadyMember)
}

func testDuplicateChannelName(t *testing.T, a adapter.Adapter) {
	ch := createChannel(t, a, "ev-0920-dup")

	_, err := a.CreatePrivateChannel(t.Context(), Workspace(), adapter.CreateChannelInput{Name: ch.Name})
	require.ErrorIs(t, err, adapter.ErrNameTaken)
}

func testMembership(t *testing.T, a adapter.Adapter) {
	ch := createChannel(t, a, "ev-0920-members")

	require.NoError(t, a.AddMember(t.Context(), Workspace(), ch.ID, guest))
	require.ErrorIs(t, a.AddMember(t.Context(), Workspace(), ch.ID, guest), adapter.ErrAlreadyMember)

	require.NoError(t, a.RemoveMember(t.Context(), Workspace(), ch.ID, guest))
	require.ErrorIs(t, a.RemoveMember(t.Context(), Workspace(), ch.ID, guest), adapter.ErrNotMember)
}

func testUnknownChannel(t *testing.T, a adapter.Adapter) {
	require.ErrorIs(t, a.AddMember(t.Context(), Workspace(), "C-missing", guest), adapter.ErrNotFound)

	_, err := a.PostMessage(t.Context(), Workspace(), "C-missing", &asobellv1.Message{Text: "hi"})
	require.ErrorIs(t, err, adapter.ErrNotFound)
}

func testArchive(t *testing.T, a adapter.Adapter) {
	ch := createChannel(t, a, "ev-0920-archive")

	archived, err := a.ArchiveChannel(t.Context(), Workspace(), ch.ID, "")
	require.NoError(t, err)
	assert.True(t, archived.Archived)

	again, err := a.ArchiveChannel(t.Context(), Workspace(), ch.ID, "")
	require.NoError(t, err, "アーカイブ済みでも成功扱い")
	assert.Equal(t, archived.Name, again.Name)
}

func testListChannels(t *testing.T, a adapter.Adapter) {
	for _, name := range []string{"ev-0920-a", "ev-0920-b", "ev-0920-c"} {
		createChannel(t, a, name)
	}

	first, next, err := a.ListChannels(t.Context(), Workspace(), "", 2)
	require.NoError(t, err)
	require.Len(t, first, 2)
	require.NotEmpty(t, next)

	rest, _, err := a.ListChannels(t.Context(), Workspace(), next, 2)
	require.NoError(t, err)
	assert.NotEmpty(t, rest)
	assert.NotEqual(t, first[0].ID, rest[0].ID, "ページをまたいで同じチャンネルを返さない")
}

func testMessages(t *testing.T, a adapter.Adapter) {
	ch := createChannel(t, a, "ev-0920-posts")

	ref, err := a.PostMessage(t.Context(), Workspace(), ch.ID, &asobellv1.Message{Text: "こんにちは"})
	require.NoError(t, err)
	assert.Equal(t, ch.ID, ref.ChannelID)
	assert.NotEmpty(t, ref.MessageID)

	require.NoError(t, a.UpdateMessage(t.Context(), Workspace(), ref, &asobellv1.Message{Text: "更新"}))

	missing := adapter.MessageRef{ChannelID: ch.ID, MessageID: "M-missing"}
	require.ErrorIs(t, a.UpdateMessage(t.Context(), Workspace(), missing, &asobellv1.Message{}), adapter.ErrNotFound)
}

func testPins(t *testing.T, a adapter.Adapter) {
	ch := createChannel(t, a, "ev-0920-pins")

	ref, err := a.PostMessage(t.Context(), Workspace(), ch.ID, &asobellv1.Message{Text: "概要"})
	require.NoError(t, err)

	require.NoError(t, a.PinMessage(t.Context(), Workspace(), ref))
	require.NoError(t, a.UnpinMessage(t.Context(), Workspace(), ref))

	missing := adapter.MessageRef{ChannelID: ch.ID, MessageID: "M-missing"}
	require.ErrorIs(t, a.PinMessage(t.Context(), Workspace(), missing), adapter.ErrNotFound)
}

func testDirectMessage(t *testing.T, a adapter.Adapter) {
	if !a.Capabilities().GetDirectMessage() {
		t.Skip("direct_message 非対応")
	}

	ref, err := a.SendDirectMessage(t.Context(), Workspace(), guest, &asobellv1.Message{Text: "連携 URL"})
	require.NoError(t, err)
	assert.NotEmpty(t, ref.MessageID)
}

func testResolveUser(t *testing.T, a adapter.Adapter) {
	user, err := a.ResolveUser(t.Context(), Workspace(), organizer)
	require.NoError(t, err)
	assert.Equal(t, organizer, user.ID)
	assert.NotEmpty(t, user.DisplayName)
}
