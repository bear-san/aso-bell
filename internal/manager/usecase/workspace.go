package usecase

import (
	"context"
	"fmt"

	"github.com/bear-san/aso-bell/internal/manager/domain"
)

// WorkspaceService はワークスペースの同期と設定を扱う。
type WorkspaceService struct {
	deps Deps
}

// NewWorkspaceService は WorkspaceService を作る。
func NewWorkspaceService(deps Deps) *WorkspaceService {
	return &WorkspaceService{deps: deps.withDefaults()}
}

// Sync は Provider から報告されたワークスペースを登録・名前更新する(ReportWorkspaces / 起動時同期)。
// 報告に含まれないワークスペースは削除しない。一時的な接続断で過去のイベントを失わないため。
func (s *WorkspaceService) Sync(
	ctx context.Context,
	kind domain.ProviderKind,
	reported []ProviderWorkspace,
) ([]*domain.Workspace, error) {
	now := s.deps.now()
	out := make([]*domain.Workspace, 0, len(reported))

	for _, rep := range reported {
		ws, err := s.deps.Repo.Workspaces().Upsert(ctx, &domain.Workspace{
			Provider:   kind,
			ExternalID: rep.ExternalID,
			Name:       rep.Name,
			Settings:   domain.DefaultWorkspaceSettings(),
		}, now)
		if err != nil {
			return nil, fmt.Errorf("sync workspace %s: %w", rep.ExternalID, err)
		}

		out = append(out, ws)
	}

	return out, nil
}

// SyncFromProvider は Provider に接続済みワークスペースを問い合わせて同期する(Manager 起動時)。
func (s *WorkspaceService) SyncFromProvider(ctx context.Context) ([]*domain.Workspace, error) {
	info, err := s.deps.Provider.GetInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("get provider info: %w", err)
	}

	reported, err := s.deps.Provider.ListConnectedWorkspaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("list connected workspaces: %w", err)
	}

	return s.Sync(ctx, info.Kind, reported)
}

// List は登録済みのワークスペースを返す。
func (s *WorkspaceService) List(ctx context.Context) ([]*domain.Workspace, error) {
	return s.deps.Repo.Workspaces().List(ctx)
}

// Get はワークスペースを 1 件返す。
func (s *WorkspaceService) Get(ctx context.Context, id string) (*domain.Workspace, error) {
	return s.deps.workspace(ctx, id)
}

// GetByExternalID は Provider 側の ID でワークスペースを引く。
func (s *WorkspaceService) GetByExternalID(
	ctx context.Context,
	kind domain.ProviderKind,
	externalID string,
) (*domain.Workspace, error) {
	return s.deps.Repo.Workspaces().GetByExternalID(ctx, kind, externalID)
}

// UpdateSettings は設定を差し替える。未設定の項目は既定値で補う。
func (s *WorkspaceService) UpdateSettings(
	ctx context.Context,
	id string,
	settings domain.WorkspaceSettings,
) (*domain.Workspace, error) {
	settings.Normalize()

	if err := settings.Validate(); err != nil {
		return nil, err
	}

	return s.deps.Repo.Workspaces().UpdateSettings(ctx, id, settings, s.deps.now())
}

// SetRecruitChannel は募集チャンネルを設定する(`/asobell recruit set`)。
func (s *WorkspaceService) SetRecruitChannel(
	ctx context.Context,
	id, channelID string,
) (*domain.Workspace, error) {
	return s.updateRecruitChannel(ctx, id, channelID)
}

// ClearRecruitChannel は募集チャンネルの設定を解除する(`/asobell recruit clear`)。
func (s *WorkspaceService) ClearRecruitChannel(ctx context.Context, id string) (*domain.Workspace, error) {
	return s.updateRecruitChannel(ctx, id, "")
}

func (s *WorkspaceService) updateRecruitChannel(
	ctx context.Context,
	id, channelID string,
) (*domain.Workspace, error) {
	ws, err := s.deps.workspace(ctx, id)
	if err != nil {
		return nil, err
	}

	settings := ws.Settings
	settings.RecruitChannelID = channelID

	return s.UpdateSettings(ctx, id, settings)
}

// Resolve は Provider から届いた参照を Manager 内部のワークスペースへ解決する。
// Provider は自身の ID 体系しか知らないため、未登録の外部 ID は「まだ同期していない」を意味する。
func (s *WorkspaceService) Resolve(ctx context.Context, ref WorkspaceRef) (*domain.Workspace, error) {
	if ref.WorkspaceID != "" {
		return s.deps.workspace(ctx, ref.WorkspaceID)
	}

	if ref.ExternalID == "" {
		return nil, domain.ErrNotFound
	}

	return s.deps.Repo.Workspaces().GetByExternalID(ctx, s.deps.Status.State().Kind, ref.ExternalID)
}
