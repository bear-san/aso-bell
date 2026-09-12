// Package storetest は usecase のリポジトリポートが満たすべき契約テストを提供する。
// MongoDB 実装と、将来のインメモリ実装の両方を同じテストで検証する。
package storetest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
)

// Now は契約テストで基準にする固定時刻。
func Now() time.Time {
	return time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC)
}

// SeedWorkspace は既定設定のワークスペースを 1 件作る。
func SeedWorkspace(t *testing.T, repo usecase.Repository, externalID string) *domain.Workspace {
	t.Helper()

	ws, err := repo.Workspaces().Upsert(t.Context(), &domain.Workspace{
		Provider:   domain.ProviderKindSlack,
		ExternalID: externalID,
		Name:       "あそび部",
		Settings:   domain.DefaultWorkspaceSettings(),
	}, Now())
	require.NoError(t, err)

	return ws
}

// SeedEvent は open 状態のイベントを 1 件作る。
func SeedEvent(t *testing.T, repo usecase.Repository, workspaceID string, startsAt time.Time) *domain.Event {
	t.Helper()

	ev, err := domain.NewEvent(domain.NewEventParams{
		WorkspaceID:     workspaceID,
		Title:           "ボドゲ会",
		Description:     "18時集合",
		Location:        "渋谷",
		StartsAt:        startsAt,
		Organizer:       domain.ChatUserRef{UserID: "U0123", DisplayName: "kentaro"},
		OriginChannelID: "C0123",
		ReminderPolicy:  domain.DefaultReminderPolicy(),
		CreatedVia:      domain.CreatedViaChat,
		CreatedBy:       "U0123",
	}, Now())
	require.NoError(t, err)

	created, err := repo.Events().Create(t.Context(), ev)
	require.NoError(t, err)

	return created
}

// SeedConsoleUser は WebConsole 利用者を 1 件作る。
func SeedConsoleUser(t *testing.T, repo usecase.Repository, googleSub string) *domain.ConsoleUser {
	t.Helper()

	user, err := repo.ConsoleUsers().Create(t.Context(), &domain.ConsoleUser{
		GoogleSub:   googleSub,
		Email:       "kentaro@example.com",
		Name:        "Kentaro",
		CreatedVia:  domain.AccountOriginLink,
		CreatedAt:   Now(),
		LastLoginAt: Now(),
	})
	require.NoError(t, err)

	return user
}
