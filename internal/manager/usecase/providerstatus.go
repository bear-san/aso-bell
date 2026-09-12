package usecase

import (
	"context"

	"github.com/bear-san/aso-bell/internal/manager/domain"
)

// ProviderStatusService は対になる Provider の観測状態を扱う。
type ProviderStatusService struct {
	deps Deps
}

// NewProviderStatusService は ProviderStatusService を作る。
func NewProviderStatusService(deps Deps) *ProviderStatusService {
	return &ProviderStatusService{deps: deps.withDefaults()}
}

// Current は監視タスクが観測している最新の状態を返す。
// WebConsole の表示に使うため Provider へは問い合わせず、監視結果だけを見る。
func (s *ProviderStatusService) Current(_ context.Context) domain.ProviderState {
	return s.deps.Status.State()
}

// Stored は永続化された状態を返す。監視タスクが起動直後で未観測の場合に使う。
func (s *ProviderStatusService) Stored(ctx context.Context) (*domain.ProviderState, error) {
	return s.deps.Repo.Meta().GetProvider(ctx)
}
