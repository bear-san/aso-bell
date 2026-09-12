package runtime_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/provider/adapter"
	"github.com/bear-san/aso-bell/internal/provider/fake"
)

func reply(text string) *asobellv1.Reply {
	return &asobellv1.Reply{
		Message:    &asobellv1.Message{Text: text},
		Visibility: asobellv1.Visibility_VISIBILITY_EPHEMERAL,
	}
}

func TestStartReportsWorkspacesBeforeConnecting(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.start(t)

	reported := h.manager.Reported()
	require.Len(t, reported, 1)
	require.Len(t, reported[0], 1)
	assert.Equal(t, externalID, reported[0][0].GetExternalId())
	assert.True(t, h.adapter.Connected())
}

func TestStartRetriesUntilManagerIsReachable(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.manager.FailNext("ReportWorkspaces", status.Error(codes.Unavailable, "starting"))
	h.manager.FailNext("ReportWorkspaces", status.Error(codes.Unavailable, "starting"))

	h.start(t)

	assert.Len(t, h.manager.Reported(), 1, "成功した分だけ記録される")
	assert.True(t, h.adapter.Connected(), "疎通できてからチャットツールへ接続する")
}

func TestStartStopsWhenContextIsCanceled(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	for range 100 {
		h.manager.FailNext("ReportWorkspaces", status.Error(codes.Unavailable, "down"))
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	require.Error(t, h.runtime.Start(ctx))
	assert.False(t, h.adapter.Connected())
}

func TestOnCommandAcksBeforeForwarding(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.start(t)
	h.manager.SetReply(reply("イベントを作成しました"))

	responder := fake.NewResponder()
	h.adapter.Sink().OnCommand(t.Context(), command("list", responder))

	require.Len(t, h.manager.Commands(), 1)
	assert.Equal(t, "list", h.manager.Commands()[0].GetName())
	assert.Equal(t, []string{"ack", "followup"}, responder.Order(), "先に Ack してから返信する")
	require.Len(t, responder.Followups(), 1)
	assert.Equal(t, "イベントを作成しました", responder.Followups()[0].GetMessage().GetText())
}

func TestOnCommandOpensFormInsteadOfFollowup(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.start(t)
	h.manager.SetReply(&asobellv1.Reply{OpenForm: &asobellv1.Form{FormId: "event_new"}})

	responder := fake.NewResponder()
	h.adapter.Sink().OnCommand(t.Context(), command("new", responder))

	assert.Equal(t, []string{"ack", "form"}, responder.Order())
	require.Len(t, responder.Forms(), 1)
	assert.Equal(t, "event_new", responder.Forms()[0].GetFormId())
}

func TestOnCommandFallsBackWhenFormCannotOpen(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.start(t)
	h.manager.SetReply(&asobellv1.Reply{OpenForm: &asobellv1.Form{FormId: "event_new"}})

	responder := fake.NewResponder()
	responder.FailNext("OpenForm", adapter.ErrUnsupported)

	h.adapter.Sink().OnCommand(t.Context(), command("new", responder))

	require.Len(t, responder.Followups(), 1)
	assert.Contains(t, responder.Followups()[0].GetMessage().GetText(), "ただいま利用できません")
}

func TestOnCommandRepliesWhenManagerIsUnavailable(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.start(t)
	h.manager.FailNext("HandleCommand", status.Error(codes.Unavailable, "down"))

	responder := fake.NewResponder()
	h.adapter.Sink().OnCommand(t.Context(), command("list", responder))

	require.Len(t, responder.Followups(), 1)
	assert.Contains(t, responder.Followups()[0].GetMessage().GetText(), "ただいま利用できません")
	assert.Equal(t,
		asobellv1.Visibility_VISIBILITY_EPHEMERAL, responder.Followups()[0].GetVisibility(),
		"障害の案内は本人にだけ見せる")
}

func TestOnCommandSkipsFollowupWithoutMessage(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.start(t)

	responder := fake.NewResponder()
	h.adapter.Sink().OnCommand(t.Context(), command("list", responder))

	assert.Equal(t, []string{"ack"}, responder.Order(), "返す内容が無ければ追加応答しない")
}

func TestOnActionForwardsToManager(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.start(t)
	h.manager.SetReply(reply("参加しました"))

	responder := fake.NewResponder()
	h.adapter.Sink().OnAction(t.Context(), adapter.Action{
		Payload: &asobellv1.Action{
			Workspace: workspace(),
			ChannelId: originChan,
			UserId:    guest,
			ActionId:  "asobell:join:66e0a1b2c3d4e5f607182930",
		},
		Responder: responder,
	})

	require.Len(t, h.manager.Actions(), 1)
	assert.Equal(t, "asobell:join:66e0a1b2c3d4e5f607182930", h.manager.Actions()[0].GetActionId())
	assert.Equal(t, []string{"ack", "followup"}, responder.Order())
}

func TestOnFormSubmitAcksWithResult(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.start(t)
	h.manager.SetReply(&asobellv1.Reply{FormErrors: map[string]string{"title": "必須です"}})

	responder := fake.NewResponder()
	h.adapter.Sink().OnFormSubmit(t.Context(), adapter.FormSubmission{
		Payload: &asobellv1.FormSubmission{
			Workspace: workspace(),
			UserId:    organizer,
			FormId:    "event_new",
			Values:    map[string]string{"title": ""},
		},
		Responder: responder,
	})

	require.Len(t, h.manager.FormSubmissions(), 1)
	assert.Equal(t, []string{"ack"}, responder.Order(), "モーダルへはバリデーション結果を Ack で返す")
	require.Len(t, responder.Acks(), 1)
	assert.Equal(t, "必須です", responder.Acks()[0].GetFormErrors()["title"])
}

func TestOnFormSubmitAcksWhenManagerIsUnavailable(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.start(t)
	h.manager.FailNext("HandleFormSubmit", status.Error(codes.Unavailable, "down"))

	responder := fake.NewResponder()
	h.adapter.Sink().OnFormSubmit(t.Context(), adapter.FormSubmission{
		Payload:   &asobellv1.FormSubmission{Workspace: workspace(), UserId: organizer, FormId: "event_new"},
		Responder: responder,
	})

	require.Len(t, responder.Acks(), 1)
	assert.Contains(t, responder.Acks()[0].GetMessage().GetText(), "ただいま利用できません")
}

func TestDiscoveredWorkspaceIsReported(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.start(t)

	h.adapter.AddWorkspace(t.Context(), adapter.WorkspaceInfo{ExternalID: "G0999", Name: "別のサーバー"})

	reported := h.manager.Reported()
	require.Len(t, reported, 2)
	require.Len(t, reported[1], 1)
	assert.Equal(t, "G0999", reported[1][0].GetExternalId())
}

func TestDiscoveredWorkspaceFailureDoesNotStopProvider(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.start(t)
	h.manager.FailNext("ReportWorkspaces", status.Error(codes.Unavailable, "down"))

	h.adapter.AddWorkspace(t.Context(), adapter.WorkspaceInfo{ExternalID: "G0999", Name: "別のサーバー"})

	assert.True(t, h.adapter.Connected())
}

func TestHealthFollowsChatConnection(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	before, err := h.health.Check(t.Context(), &healthpb.HealthCheckRequest{})
	require.NoError(t, err)
	assert.Equal(t, healthpb.HealthCheckResponse_NOT_SERVING, before.GetStatus())

	h.start(t)

	serving, err := h.health.Check(t.Context(), &healthpb.HealthCheckRequest{})
	require.NoError(t, err)
	assert.Equal(t, healthpb.HealthCheckResponse_SERVING, serving.GetStatus())

	require.NoError(t, h.runtime.Close(t.Context()))

	after, err := h.health.Check(t.Context(), &healthpb.HealthCheckRequest{})
	require.NoError(t, err)
	assert.Equal(t, healthpb.HealthCheckResponse_NOT_SERVING, after.GetStatus())
}

func TestResponderFailuresDoNotStopForwarding(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.start(t)
	h.manager.SetReply(reply("参加しました"))

	responder := fake.NewResponder()
	responder.FailNext("Ack", errors.New("interaction expired"))
	responder.FailNext("Followup", errors.New("webhook token expired"))

	h.adapter.Sink().OnAction(t.Context(), adapter.Action{
		Payload:   &asobellv1.Action{Workspace: workspace(), ChannelId: originChan, UserId: guest, ActionId: "a"},
		Responder: responder,
	})

	require.Len(t, h.manager.Actions(), 1, "応答に失敗しても Manager への転送は行う。状態はすでに変わっているため")
	assert.Equal(t, []string{"ack", "followup"}, responder.Order())
}

func TestOnActionRepliesWhenManagerIsUnavailable(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.start(t)
	h.manager.FailNext("HandleAction", status.Error(codes.Unavailable, "down"))

	responder := fake.NewResponder()
	h.adapter.Sink().OnAction(t.Context(), adapter.Action{
		Payload:   &asobellv1.Action{Workspace: workspace(), ChannelId: originChan, UserId: guest, ActionId: "a"},
		Responder: responder,
	})

	require.Len(t, responder.Followups(), 1)
	assert.Contains(t, responder.Followups()[0].GetMessage().GetText(), "ただいま利用できません")
	assert.Equal(t,
		asobellv1.Visibility_VISIBILITY_EPHEMERAL, responder.Followups()[0].GetVisibility(),
		"障害の告知は本人にだけ見せる")
}

func TestStartFailsWhenChatConnectionFails(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.adapter.FailNext("Connect", errors.New("invalid token"))

	err := h.runtime.Start(t.Context())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "connect adapter")
	assert.False(t, h.adapter.Connected())
}
