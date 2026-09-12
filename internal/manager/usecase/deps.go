package usecase

import (
	"context"
	"errors"
	"log/slog"
	"time"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/manager/domain"
)

// Deps は各ユースケースが共有する依存。app が組み立てて注入する。
type Deps struct {
	Repo     Repository
	Provider ProviderPort
	Status   ProviderStatusPort
	Messages MessageRenderer
	Clock    Clock
	Logger   *slog.Logger
}

func (d Deps) withDefaults() Deps {
	if d.Clock == nil {
		d.Clock = SystemClock{}
	}

	if d.Logger == nil {
		d.Logger = slog.Default()
	}

	return d
}

func (d Deps) now() time.Time {
	return d.Clock.Now().UTC()
}

// requireProviderOnline は Provider が offline のときに操作を断る。
// offline のまま進めるとチャンネル作成や投稿が中途半端に失敗し、イベントが不完全な状態で残るため(docs/07 §2.3)。
func (d Deps) requireProviderOnline() error {
	if d.Status.State().Status != domain.ProviderStatusOnline {
		return domain.ErrProviderUnavailable
	}

	return nil
}

func (d Deps) workspace(ctx context.Context, id string) (*domain.Workspace, error) {
	return d.Repo.Workspaces().Get(ctx, id)
}

func (d Deps) event(ctx context.Context, id string) (*domain.Event, error) {
	return d.Repo.Events().Get(ctx, id)
}

// post は投稿し、失敗しても処理を止めない箇所で使う。投稿できたかどうかは呼び出し側が参照しない。
func (d Deps) post(ctx context.Context, ws *domain.Workspace, channelID string, msg *asobellv1.Message) {
	if channelID == "" || msg == nil {
		return
	}

	if _, err := d.Provider.PostMessage(ctx, workspaceRefOf(ws), channelID, msg); err != nil {
		d.Logger.WarnContext(ctx, "post message failed", "channel_id", channelID, "error", err)
	}
}

// updateMessage は投稿済みメッセージを更新する。手動で削除されたメッセージは更新できないため、
// 見つからない場合は警告に留めて処理を続ける(docs/07 §8)。
func (d Deps) updateMessage(
	ctx context.Context,
	ws *domain.Workspace,
	ref *domain.MessageRef,
	msg *asobellv1.Message,
) {
	if ref == nil || msg == nil {
		return
	}

	if err := d.Provider.UpdateMessage(ctx, workspaceRefOf(ws), *ref, msg); err != nil {
		d.Logger.WarnContext(ctx, "update message failed", "message_id", ref.MessageID, "error", err)
	}
}

// pin はピン留めする。上限超過や権限不足は警告ログのみで続行する(docs/07 §2.3)。
func (d Deps) pin(ctx context.Context, ws *domain.Workspace, ref domain.MessageRef) {
	if err := d.Provider.PinMessage(ctx, workspaceRefOf(ws), ref); err != nil {
		d.Logger.WarnContext(ctx, "pin message failed", "message_id", ref.MessageID, "error", err)
	}
}

func (d Deps) unpin(ctx context.Context, ws *domain.Workspace, ref *domain.MessageRef) {
	if ref == nil {
		return
	}

	if err := d.Provider.UnpinMessage(ctx, workspaceRefOf(ws), *ref); err != nil {
		d.Logger.WarnContext(ctx, "unpin message failed", "message_id", ref.MessageID, "error", err)
	}
}

// enqueue はジョブを登録する。dedupeKey の重複は「同じジョブが既にある」を意味するため成功として扱う。
func (d Deps) enqueue(ctx context.Context, job domain.Job) error {
	if _, err := d.Repo.Jobs().Enqueue(ctx, job, d.now()); err != nil && !errors.Is(err, domain.ErrAlreadyExists) {
		return err
	}

	return nil
}

func workspaceRefOf(ws *domain.Workspace) WorkspaceRef {
	return WorkspaceRef{WorkspaceID: ws.ID, ExternalID: ws.ExternalID}
}
