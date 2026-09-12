package runtime

import (
	"context"

	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/provider/adapter"
)

// フォームを開く可能性があるサブコマンド。trigger_id の有効期限があるため短いデッドラインで扱う(docs/06 §3.1)。
const (
	subcommandNew  = "new"
	subcommandEdit = "edit"
)

// OnCommand はスラッシュコマンドを Manager へ転送する。
// 先に Ack を返してから転送する。プラットフォームの 3 秒制限は Manager の応答を待てないため。
func (r *Runtime) OnCommand(ctx context.Context, cmd adapter.Command) {
	r.ack(ctx, cmd.Responder, nil, "command")

	ctx, cancel := r.callContext(ctx, cmd.Payload.GetName() == subcommandNew || cmd.Payload.GetName() == subcommandEdit)
	defer cancel()

	res, err := r.cfg.Manager.HandleCommand(ctx, &asobellv1.HandleCommandRequest{Command: cmd.Payload})
	if err != nil {
		r.cfg.Logger.ErrorContext(ctx, "handle command failed",
			"error", err, "subcommand", cmd.Payload.GetName(), "user_id", cmd.Payload.GetUserId())
		r.followup(ctx, cmd.Responder, unavailableReply())

		return
	}

	reply := res.GetReply()
	if form := reply.GetOpenForm(); form != nil {
		if openErr := cmd.Responder.OpenForm(ctx, form); openErr != nil {
			r.cfg.Logger.ErrorContext(ctx, "open form failed", "error", openErr, "form_id", form.GetFormId())
			r.followup(ctx, cmd.Responder, unavailableReply())
		}

		return
	}

	r.followup(ctx, cmd.Responder, reply)
}

// OnAction はボタン押下を Manager へ転送する。
func (r *Runtime) OnAction(ctx context.Context, act adapter.Action) {
	r.ack(ctx, act.Responder, nil, "action")

	ctx, cancel := r.callContext(ctx, false)
	defer cancel()

	res, err := r.cfg.Manager.HandleAction(ctx, &asobellv1.HandleActionRequest{Action: act.Payload})
	if err != nil {
		r.cfg.Logger.ErrorContext(ctx, "handle action failed",
			"error", err, "action_id", act.Payload.GetActionId(), "user_id", act.Payload.GetUserId())
		r.followup(ctx, act.Responder, unavailableReply())

		return
	}

	r.followup(ctx, act.Responder, res.GetReply())
}

// OnFormSubmit はフォーム送信を Manager へ転送する。
// バリデーション結果をモーダルへ返す必要があるため、Ack より先に Manager を呼ぶ(docs/06 §3.1)。
func (r *Runtime) OnFormSubmit(ctx context.Context, sub adapter.FormSubmission) {
	ctx, cancel := r.callContext(ctx, true)
	defer cancel()

	res, err := r.cfg.Manager.HandleFormSubmit(ctx, &asobellv1.HandleFormSubmitRequest{Submission: sub.Payload})
	if err != nil {
		r.cfg.Logger.ErrorContext(ctx, "handle form submit failed",
			"error", err, "form_id", sub.Payload.GetFormId(), "user_id", sub.Payload.GetUserId())
		r.ack(ctx, sub.Responder, unavailableReply(), "form submit")

		return
	}

	r.ack(ctx, sub.Responder, res.GetReply(), "form submit")
}

// OnWorkspaceDiscovered は接続後に判明したワークスペースを Manager へ通知する。
func (r *Runtime) OnWorkspaceDiscovered(ctx context.Context, ws adapter.WorkspaceInfo) {
	if err := r.reportWorkspaces(ctx, []adapter.WorkspaceInfo{ws}); err != nil {
		// 次の起動時か、Manager 側の ListConnectedWorkspaces で同期されるため、ここでは再試行しない。
		r.cfg.Logger.WarnContext(ctx, "report discovered workspace failed", "error", err, "external_id", ws.ExternalID)
	}
}

// OnConnectionStateChanged はチャットツールへの接続状態を Health へ反映する(docs/06 §3.3)。
func (r *Runtime) OnConnectionStateChanged(connected bool, cause error) {
	status := healthpb.HealthCheckResponse_NOT_SERVING
	if connected {
		status = healthpb.HealthCheckResponse_SERVING
	}

	r.health.SetServingStatus("", status)

	attrs := []any{"connected", connected}
	if cause != nil {
		attrs = append(attrs, "error", cause)
	}

	r.cfg.Logger.Info("chat connection state changed", attrs...)
}

// callContext は Manager 呼び出し用の ctx を作る。
// fast はプラットフォームの 3 秒制限に間に合わせる必要がある呼び出し(フォームを開く・送信する)。
func (r *Runtime) callContext(ctx context.Context, fast bool) (context.Context, context.CancelFunc) {
	timeout := r.cfg.Timeout
	if fast {
		timeout = r.cfg.FastTimeout
	}

	return context.WithTimeout(ctx, timeout)
}

func (r *Runtime) ack(ctx context.Context, responder adapter.Responder, reply *asobellv1.Reply, what string) {
	if err := responder.Ack(ctx, reply); err != nil {
		r.cfg.Logger.ErrorContext(ctx, "ack failed", "error", err, "inbound", what)
	}
}

// followup は Ack 後の返信を送る。返す内容が無ければ何もしない。
func (r *Runtime) followup(ctx context.Context, responder adapter.Responder, reply *asobellv1.Reply) {
	if reply.GetMessage() == nil {
		return
	}

	if err := responder.Followup(ctx, reply); err != nil {
		r.cfg.Logger.ErrorContext(ctx, "followup failed", "error", err, "visibility", reply.GetVisibility().String())
	}
}

// unavailableReply は Manager に到達できないときの固定の返信(docs/06 §3.1)。
func unavailableReply() *asobellv1.Reply {
	return &asobellv1.Reply{
		Message:    &asobellv1.Message{Text: msgManagerUnavailable},
		Visibility: asobellv1.Visibility_VISIBILITY_EPHEMERAL,
	}
}
