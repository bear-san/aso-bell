package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
)

func TestSyncWorkspacesAddsAndRenames(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	svc := usecase.NewWorkspaceService(h.deps)

	synced, err := svc.Sync(t.Context(), domain.ProviderKindSlack, []usecase.ProviderWorkspace{
		{ExternalID: "T0123", Name: "あそび部(改名)"},
		{ExternalID: "T0999", Name: "別チーム"},
	})
	require.NoError(t, err)
	require.Len(t, synced, 2)

	all, err := svc.List(t.Context())
	require.NoError(t, err)
	assert.Len(t, all, 2, "既存ワークスペースは重複登録しない")

	existing, err := svc.GetByExternalID(t.Context(), domain.ProviderKindSlack, "T0123")
	require.NoError(t, err)
	assert.Equal(t, "あそび部(改名)", existing.Name)
	assert.Equal(t, h.workspac.ID, existing.ID)
}

func TestSyncWorkspacesKeepsSettings(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.updateSettings(t, func(s *domain.WorkspaceSettings) { s.RecruitChannelID = "C0RECRUIT" })

	svc := usecase.NewWorkspaceService(h.deps)

	_, err := svc.Sync(t.Context(), domain.ProviderKindSlack, []usecase.ProviderWorkspace{
		{ExternalID: "T0123", Name: "あそび部"},
	})
	require.NoError(t, err)

	ws, err := svc.Get(t.Context(), h.workspac.ID)
	require.NoError(t, err)
	assert.Equal(t, "C0RECRUIT", ws.Settings.RecruitChannelID)
}

func TestSyncWorkspacesKeepsUnreported(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	svc := usecase.NewWorkspaceService(h.deps)

	// 接続断で報告が空になっても、過去のイベントを失わないよう削除しない。
	_, err := svc.Sync(t.Context(), domain.ProviderKindSlack, nil)
	require.NoError(t, err)

	all, err := svc.List(t.Context())
	require.NoError(t, err)
	assert.Len(t, all, 1)
}

func TestSyncFromProviderUsesProviderKind(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.fake.SetWorkspaces(&asobellv1.WorkspaceInfo{ExternalId: "T0999", Name: "別チーム"})

	synced, err := usecase.NewWorkspaceService(h.deps).SyncFromProvider(t.Context())
	require.NoError(t, err)
	require.Len(t, synced, 1)

	assert.Equal(t, domain.ProviderKindSlack, synced[0].Provider)
	assert.Equal(t, "T0999", synced[0].ExternalID)
}

func TestUpdateSettingsNormalizesAndValidates(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	svc := usecase.NewWorkspaceService(h.deps)

	ws, err := svc.UpdateSettings(t.Context(), h.workspac.ID, domain.WorkspaceSettings{})
	require.NoError(t, err)
	assert.Equal(t, domain.DefaultTimezone, ws.Settings.Timezone)
	assert.Equal(t, domain.DefaultPeekDuration, ws.Settings.PeekDuration)

	_, err = svc.UpdateSettings(t.Context(), h.workspac.ID, domain.WorkspaceSettings{Timezone: "Mars/Olympus"})

	var invalid *domain.ValidationError

	require.ErrorAs(t, err, &invalid)
}

func TestRecruitChannelSetAndClear(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	svc := usecase.NewWorkspaceService(h.deps)

	ws, err := svc.SetRecruitChannel(t.Context(), h.workspac.ID, "C0RECRUIT")
	require.NoError(t, err)
	assert.Equal(t, "C0RECRUIT", ws.Settings.RecruitChannelID)
	// 他の設定は変えない。
	assert.Equal(t, h.workspac.Settings.PeekDuration, ws.Settings.PeekDuration)

	ws, err = svc.ClearRecruitChannel(t.Context(), h.workspac.ID)
	require.NoError(t, err)
	assert.Empty(t, ws.Settings.RecruitChannelID)
}

func TestProviderStatusReportsObservedState(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	svc := usecase.NewProviderStatusService(h.deps)

	assert.Equal(t, domain.ProviderStatusOnline, svc.Current(t.Context()).Status)

	h.status.state.Status = domain.ProviderStatusOffline
	assert.Equal(t, domain.ProviderStatusOffline, svc.Current(t.Context()).Status)
}
