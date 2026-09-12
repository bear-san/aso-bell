package storetest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
)

// NewRepository は空の状態のリポジトリを 1 つ返すファクトリ。テストごとに独立していること。
type NewRepository func(t *testing.T) usecase.Repository

// RunContract はリポジトリ実装が満たすべき契約をすべて検証する。
func RunContract(t *testing.T, newRepo NewRepository) {
	t.Helper()

	suites := map[string]func(*testing.T, usecase.Repository){
		"Meta":           testMeta,
		"Workspaces":     testWorkspaces,
		"Events":         testEvents,
		"Participations": testParticipations,
		"Jobs":           testJobs,
		"ReminderLogs":   testReminderLogs,
		"Accounts":       testAccounts,
		"LinkTokens":     testLinkTokens,
		"Sessions":       testSessions,
		"OAuthStates":    testOAuthStates,
	}

	for name, run := range suites {
		t.Run(name, func(t *testing.T) {
			run(t, newRepo(t))
		})
	}
}

func testMeta(t *testing.T, repo usecase.Repository) {
	ctx := t.Context()
	meta := repo.Meta()

	_, err := meta.GetProvider(ctx)
	require.ErrorIs(t, err, domain.ErrNotFound)

	version, err := meta.SchemaVersion(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, version)

	require.NoError(t, meta.SetSchemaVersion(ctx, 1))

	version, err = meta.SchemaVersion(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, version)

	state := &domain.ProviderState{
		Kind:         domain.ProviderKindSlack,
		Address:      "dns:///provider:9091",
		Capabilities: domain.Capabilities{Forms: true, Ephemeral: true, DirectMessage: true},
		Version:      "v0.1.0",
		BotUserID:    "U0BOT",
		Connected:    true,
		Status:       domain.ProviderStatusOnline,
		FirstSeenAt:  Now(),
		LastSeenAt:   Now(),
		UpdatedAt:    Now(),
	}
	require.NoError(t, meta.SaveProvider(ctx, state))

	got, err := meta.GetProvider(ctx)
	require.NoError(t, err)
	assert.Equal(t, state.Kind, got.Kind)
	assert.Equal(t, state.Capabilities, got.Capabilities)
	assert.Equal(t, Now(), got.FirstSeenAt)

	later := Now().Add(time.Hour)
	state.Status = domain.ProviderStatusOffline
	state.Connected = false
	state.Failures = 3
	state.FirstSeenAt = later
	state.LastSeenAt = later
	state.UpdatedAt = later
	require.NoError(t, meta.SaveProvider(ctx, state))

	got, err = meta.GetProvider(ctx)
	require.NoError(t, err)
	assert.Equal(t, domain.ProviderStatusOffline, got.Status)
	assert.Equal(t, 3, got.Failures)
	// firstSeenAt は初回接続時刻のまま据え置く。
	assert.Equal(t, Now(), got.FirstSeenAt)
}

func testWorkspaces(t *testing.T, repo usecase.Repository) {
	ctx := t.Context()
	workspaces := repo.Workspaces()

	ws := SeedWorkspace(t, repo, "T0123")
	assert.NotEmpty(t, ws.ID)
	assert.Equal(t, domain.DefaultTimezone, ws.Settings.Timezone)

	byID, err := workspaces.Get(ctx, ws.ID)
	require.NoError(t, err)
	assert.Equal(t, ws.ExternalID, byID.ExternalID)

	byExternal, err := workspaces.GetByExternalID(ctx, domain.ProviderKindSlack, "T0123")
	require.NoError(t, err)
	assert.Equal(t, ws.ID, byExternal.ID)

	_, err = workspaces.GetByExternalID(ctx, domain.ProviderKindDiscord, "T0123")
	require.ErrorIs(t, err, domain.ErrNotFound)

	// 2 回目の Upsert は名前だけを更新し、設定は保持する。
	custom := domain.DefaultWorkspaceSettings()
	custom.Timezone = "America/New_York"

	again, err := workspaces.Upsert(ctx, &domain.Workspace{
		Provider:   domain.ProviderKindSlack,
		ExternalID: "T0123",
		Name:       "あそび部(改名)",
		Settings:   custom,
	}, Now().Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, ws.ID, again.ID)
	assert.Equal(t, "あそび部(改名)", again.Name)
	assert.Equal(t, domain.DefaultTimezone, again.Settings.Timezone)

	updated, err := workspaces.UpdateSettings(ctx, ws.ID, custom, Now().Add(2*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, "America/New_York", updated.Settings.Timezone)

	_, err = workspaces.UpdateSettings(ctx, "66e0a1b2c3d4e5f607182930", custom, Now())
	require.ErrorIs(t, err, domain.ErrNotFound)

	SeedWorkspace(t, repo, "T0456")

	all, err := workspaces.List(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 2)
}

func testEvents(t *testing.T, repo usecase.Repository) {
	ctx := t.Context()
	events := repo.Events()
	ws := SeedWorkspace(t, repo, "T0123")

	ev := SeedEvent(t, repo, ws.ID, Now().Add(7*24*time.Hour))
	assert.NotEmpty(t, ev.ID)

	got, err := events.Get(ctx, ev.ID)
	require.NoError(t, err)
	assert.Equal(t, "ボドゲ会", got.Title)
	assert.Equal(t, domain.EventStatusOpen, got.Status)
	assert.Equal(t, domain.DefaultReminderPolicy().Offsets, got.ReminderPolicy.Offsets)

	_, err = events.Get(ctx, "66e0a1b2c3d4e5f607182930")
	require.ErrorIs(t, err, domain.ErrNotFound)

	channel := domain.ChannelRef{ChannelID: "G0456", Name: "ev-0920-ボドゲ会"}
	require.NoError(t, events.SetChannel(ctx, ev.ID, channel, Now()))

	messages := domain.MessageRefs{
		Announcement: &domain.MessageRef{ChannelID: "C0123", MessageID: "1726000000.000100"},
		Summary:      &domain.MessageRef{ChannelID: "G0456", MessageID: "1726000000.000300"},
	}
	require.NoError(t, events.SetMessages(ctx, ev.ID, messages, Now()))

	got, err = events.Get(ctx, ev.ID)
	require.NoError(t, err)
	assert.Equal(t, channel, got.Channel)
	require.NotNil(t, got.Messages.Announcement)
	assert.Equal(t, "1726000000.000100", got.Messages.Announcement.MessageID)
	assert.Nil(t, got.Messages.RecruitAnnouncement)

	byChannel, err := events.GetByChannelID(ctx, "G0456")
	require.NoError(t, err)
	assert.Equal(t, ev.ID, byChannel.ID)

	require.NoError(t, events.AddParticipantCount(ctx, ev.ID, 1, Now()))
	require.NoError(t, events.AddParticipantCount(ctx, ev.ID, 1, Now()))
	require.NoError(t, events.AddParticipantCount(ctx, ev.ID, -1, Now()))

	got, err = events.Get(ctx, ev.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, got.ParticipantCount)

	require.True(t, got.End(domain.EndReasonManual, Now().Add(time.Hour)))
	require.NoError(t, events.Update(ctx, got))

	got, err = events.Get(ctx, ev.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.EventStatusEnded, got.Status)
	assert.Equal(t, domain.EndReasonManual, got.EndReason)
	require.NotNil(t, got.EndedAt)
	// チャンネル参照は Update の対象外で、消えない。
	assert.Equal(t, "G0456", got.Channel.ChannelID)

	testEventListing(t, repo, ws.ID)
}

func testEventListing(t *testing.T, repo usecase.Repository, workspaceID string) {
	t.Helper()

	ctx := t.Context()
	events := repo.Events()

	second := SeedEvent(t, repo, workspaceID, Now().Add(14*24*time.Hour))
	third := SeedEvent(t, repo, workspaceID, Now().Add(21*24*time.Hour))

	page, err := events.List(ctx, usecase.EventFilter{WorkspaceID: workspaceID, Limit: 2})
	require.NoError(t, err)
	require.Len(t, page.Events, 2)
	assert.Equal(t, third.ID, page.Events[0].ID)
	assert.Equal(t, second.ID, page.Events[1].ID)
	require.NotEmpty(t, page.NextCursor)

	next, err := events.List(ctx, usecase.EventFilter{WorkspaceID: workspaceID, Limit: 2, Cursor: page.NextCursor})
	require.NoError(t, err)
	require.Len(t, next.Events, 1)
	assert.Empty(t, next.NextCursor)

	open, err := events.List(ctx, usecase.EventFilter{
		WorkspaceID: workspaceID,
		Statuses:    []domain.EventStatus{domain.EventStatusOpen},
	})
	require.NoError(t, err)
	assert.Len(t, open.Events, 2)

	byIDs, err := events.List(ctx, usecase.EventFilter{IDs: []string{second.ID}})
	require.NoError(t, err)
	require.Len(t, byIDs.Events, 1)
	assert.Equal(t, second.ID, byIDs.Events[0].ID)

	after := Now().Add(20 * 24 * time.Hour)

	upcoming, err := events.List(ctx, usecase.EventFilter{StartsAfter: &after})
	require.NoError(t, err)
	require.Len(t, upcoming.Events, 1)
	assert.Equal(t, third.ID, upcoming.Events[0].ID)
}
