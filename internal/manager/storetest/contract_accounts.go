package storetest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
)

func testReminderLogs(t *testing.T, repo usecase.Repository) {
	ctx := t.Context()
	logs := repo.ReminderLogs()
	ws := SeedWorkspace(t, repo, "T0123")
	ev := SeedEvent(t, repo, ws.ID, Now().Add(7*24*time.Hour))

	scheduledFor := Now().Add(time.Hour)

	sent, err := logs.SentSequence(ctx, ev.ID, 1)
	require.NoError(t, err)
	assert.False(t, sent)

	appended, err := logs.Append(ctx, &domain.ReminderLog{
		EventID:      ev.ID,
		Sequence:     1,
		ScheduledFor: scheduledFor,
		SentAt:       Now().Add(time.Hour),
		Targets: []domain.ReminderTarget{
			{Kind: domain.ReminderTargetEvent, ChannelID: "G0456", MessageID: "1726", OK: true},
			{Kind: domain.ReminderTargetRecruit, ChannelID: "C0999", OK: false, Error: "channel_not_found"},
		},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, appended.ID)
	require.Len(t, appended.Targets, 2)
	assert.Equal(t, "channel_not_found", appended.Targets[1].Error)

	sent, err = logs.SentSequence(ctx, ev.ID, 1)
	require.NoError(t, err)
	assert.True(t, sent)

	sent, err = logs.SentAt(ctx, ev.ID, scheduledFor)
	require.NoError(t, err)
	assert.True(t, sent)

	sent, err = logs.SentAt(ctx, ev.ID, scheduledFor.Add(time.Minute))
	require.NoError(t, err)
	assert.False(t, sent)

	listed, err := logs.ListByEvent(ctx, ev.ID)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, 1, listed[0].Sequence)
}

func testAccounts(t *testing.T, repo usecase.Repository) {
	ctx := t.Context()
	users := repo.ConsoleUsers()
	identities := repo.ChatIdentities()
	ws := SeedWorkspace(t, repo, "T0123")

	user := SeedConsoleUser(t, repo, "1122334455")
	assert.NotEmpty(t, user.ID)

	byID, err := users.Get(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, "kentaro@example.com", byID.Email)

	bySub, err := users.GetByGoogleSub(ctx, "1122334455")
	require.NoError(t, err)
	assert.Equal(t, user.ID, bySub.ID)

	_, err = users.GetByGoogleSub(ctx, "unknown")
	require.ErrorIs(t, err, domain.ErrNotFound)

	_, err = users.Create(ctx, &domain.ConsoleUser{GoogleSub: "1122334455", Email: "dup@example.com"})
	require.ErrorIs(t, err, domain.ErrAlreadyExists)

	later := Now().Add(time.Hour)
	require.NoError(t, users.TouchLogin(ctx, user.ID, later))

	touched, err := users.Get(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, later, touched.LastLoginAt)

	identity := &domain.ChatIdentity{
		ConsoleUserID:       user.ID,
		WorkspaceID:         ws.ID,
		Provider:            domain.ProviderKindSlack,
		WorkspaceExternalID: ws.ExternalID,
		ChatUserID:          "U0123",
		DisplayName:         "kentaro",
		LinkedAt:            Now(),
	}

	linked, err := identities.Link(ctx, identity)
	require.NoError(t, err)
	assert.NotEmpty(t, linked.ID)

	other := SeedConsoleUser(t, repo, "9988776655")
	duplicate := *identity
	duplicate.ConsoleUserID = other.ID

	_, err = identities.Link(ctx, &duplicate)
	require.ErrorIs(t, err, domain.ErrIdentityLinkedElsewhere)

	got, err := identities.Get(ctx, ws.ID, "U0123")
	require.NoError(t, err)
	assert.Equal(t, user.ID, got.ConsoleUserID)

	mine, err := identities.ListByUser(ctx, user.ID)
	require.NoError(t, err)
	assert.Len(t, mine, 1)

	require.NoError(t, identities.Unlink(ctx, linked.ID))

	_, err = identities.Get(ctx, ws.ID, "U0123")
	require.ErrorIs(t, err, domain.ErrNotFound)
	require.ErrorIs(t, identities.Unlink(ctx, linked.ID), domain.ErrNotFound)
}

func testLinkTokens(t *testing.T, repo usecase.Repository) {
	ctx := t.Context()
	tokens := repo.LinkTokens()
	ws := SeedWorkspace(t, repo, "T0123")

	plain, rec, err := domain.NewLinkToken(ws.ID, "U0123", "kentaro", Now(), domain.DefaultLinkTokenTTL)
	require.NoError(t, err)
	require.NoError(t, tokens.Create(ctx, rec))

	// GET 相当の参照では消費しない(ADR 0009)。
	got, err := tokens.Get(ctx, domain.HashToken(plain))
	require.NoError(t, err)
	assert.Equal(t, "kentaro", got.DisplayName)
	assert.Nil(t, got.ConsumedAt)

	consumed, err := tokens.Consume(ctx, rec.ID, Now().Add(time.Minute))
	require.NoError(t, err)
	require.NotNil(t, consumed.ConsumedAt)

	_, err = tokens.Consume(ctx, rec.ID, Now().Add(2*time.Minute))
	require.ErrorIs(t, err, domain.ErrLinkTokenConsumed)

	_, expiring, err := domain.NewLinkToken(ws.ID, "U0456", "taro", Now(), domain.DefaultLinkTokenTTL)
	require.NoError(t, err)
	require.NoError(t, tokens.Create(ctx, expiring))

	_, err = tokens.Consume(ctx, expiring.ID, Now().Add(domain.DefaultLinkTokenTTL+time.Minute))
	require.ErrorIs(t, err, domain.ErrLinkTokenExpired)

	_, err = tokens.Consume(ctx, "unknown", Now())
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func testSessions(t *testing.T, repo usecase.Repository) {
	ctx := t.Context()
	sessions := repo.Sessions()
	user := SeedConsoleUser(t, repo, "1122334455")

	session := domain.Session{
		ID:         "session-1",
		UserID:     user.ID,
		CSRFToken:  "csrf-1",
		CreatedAt:  Now(),
		ExpiresAt:  Now().Add(24 * time.Hour),
		LastSeenAt: Now(),
	}
	require.NoError(t, sessions.Create(ctx, session))

	got, err := sessions.Get(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, user.ID, got.UserID)
	assert.Equal(t, "csrf-1", got.CSRFToken)
	assert.False(t, got.Expired(Now()))

	later := Now().Add(time.Hour)
	require.NoError(t, sessions.Touch(ctx, session.ID, later))

	touched, err := sessions.Get(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, later, touched.LastSeenAt)

	require.NoError(t, sessions.Delete(ctx, session.ID))

	_, err = sessions.Get(ctx, session.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)

	second := session
	second.ID = "session-2"
	require.NoError(t, sessions.Create(ctx, second))
	require.NoError(t, sessions.DeleteByUser(ctx, user.ID))

	_, err = sessions.Get(ctx, second.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func testOAuthStates(t *testing.T, repo usecase.Repository) {
	ctx := t.Context()
	states := repo.OAuthStates()

	state := domain.OAuthState{
		State:        "state-1",
		CodeVerifier: "verifier",
		RedirectTo:   "/events",
		ExpiresAt:    Now().Add(10 * time.Minute),
	}
	require.NoError(t, states.Create(ctx, state))

	got, err := states.Consume(ctx, state.State, Now())
	require.NoError(t, err)
	assert.Equal(t, "verifier", got.CodeVerifier)
	assert.Equal(t, "/events", got.RedirectTo)

	// 一度きり。2 回目は取得できない。
	_, err = states.Consume(ctx, state.State, Now())
	require.ErrorIs(t, err, domain.ErrNotFound)

	expired := state
	expired.State = "state-2"
	require.NoError(t, states.Create(ctx, expired))

	_, err = states.Consume(ctx, expired.State, Now().Add(time.Hour))
	require.ErrorIs(t, err, domain.ErrNotFound)
}
