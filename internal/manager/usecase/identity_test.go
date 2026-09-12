package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
)

func seedConsoleUser(t *testing.T, h *harness, sub string) *domain.ConsoleUser {
	t.Helper()

	user, err := h.repo.ConsoleUsers().Create(t.Context(), &domain.ConsoleUser{
		GoogleSub:  sub,
		Email:      sub + "@example.com",
		Name:       "テスト",
		CreatedVia: domain.AccountOriginLink,
		CreatedAt:  testNow(),
	})
	require.NoError(t, err)

	return user
}

func issueToken(t *testing.T, h *harness) string {
	t.Helper()

	plain, err := usecase.NewIdentityService(h.deps).IssueLinkToken(
		t.Context(), h.workspac.ID, guest, "ゲスト",
	)
	require.NoError(t, err)

	return plain
}

func TestIssueLinkTokenStoresHashOnly(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	plain := issueToken(t, h)

	assert.NotEmpty(t, plain)

	_, err := h.repo.LinkTokens().Get(t.Context(), plain)
	require.ErrorIs(t, err, domain.ErrNotFound, "平文のままでは引けない")

	stored, err := h.repo.LinkTokens().Get(t.Context(), domain.HashToken(plain))
	require.NoError(t, err)
	assert.Equal(t, guest, stored.ChatUserID)
	assert.Nil(t, stored.ConsumedAt)
	assert.True(t, testNow().Add(domain.DefaultLinkTokenTTL).Equal(stored.ExpiresAt))
}

func TestGetLinkTokenDoesNotConsume(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	svc := usecase.NewIdentityService(h.deps)
	plain := issueToken(t, h)

	view, err := svc.GetLinkToken(t.Context(), plain)
	require.NoError(t, err)
	assert.Equal(t, "あそび部", view.WorkspaceName)
	assert.Equal(t, "ゲスト", view.DisplayName)

	stored, err := h.repo.LinkTokens().Get(t.Context(), domain.HashToken(plain))
	require.NoError(t, err)
	assert.Nil(t, stored.ConsumedAt)
}

func TestGetLinkTokenRejectsExpired(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	svc := usecase.NewIdentityService(h.deps)
	plain := issueToken(t, h)

	h.clock.Advance(domain.DefaultLinkTokenTTL)

	_, err := svc.GetLinkToken(t.Context(), plain)

	require.ErrorIs(t, err, domain.ErrLinkTokenExpired)
}

func TestLinkConsumesTokenOnce(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	svc := usecase.NewIdentityService(h.deps)
	user := seedConsoleUser(t, h, "sub-1")
	plain := issueToken(t, h)

	identity, err := svc.Link(t.Context(), user.ID, plain)
	require.NoError(t, err)
	assert.Equal(t, guest, identity.ChatUserID)
	assert.Equal(t, domain.ProviderKindSlack, identity.Provider)
	assert.Equal(t, "T0123", identity.WorkspaceExternalID)

	_, err = svc.Link(t.Context(), user.ID, plain)
	require.ErrorIs(t, err, domain.ErrLinkTokenConsumed, "一度きりのトークンは二重連携できない")
}

func TestLinkRejectsAlreadyLinkedChatUser(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	svc := usecase.NewIdentityService(h.deps)
	first := seedConsoleUser(t, h, "sub-1")
	second := seedConsoleUser(t, h, "sub-2")

	_, err := svc.Link(t.Context(), first.ID, issueToken(t, h))
	require.NoError(t, err)

	_, err = svc.Link(t.Context(), second.ID, issueToken(t, h))
	require.ErrorIs(t, err, domain.ErrIdentityLinkedElsewhere)
}

func TestResolveConsoleUser(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	svc := usecase.NewIdentityService(h.deps)
	user := seedConsoleUser(t, h, "sub-1")

	_, err := svc.ResolveConsoleUser(t.Context(), h.workspac.ID, guest)
	require.ErrorIs(t, err, domain.ErrNotFound)

	_, err = svc.Link(t.Context(), user.ID, issueToken(t, h))
	require.NoError(t, err)

	resolved, err := svc.ResolveConsoleUser(t.Context(), h.workspac.ID, guest)
	require.NoError(t, err)
	assert.Equal(t, user.ID, resolved.ID)
}

func TestUnlinkOnlyOwnIdentity(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	svc := usecase.NewIdentityService(h.deps)
	owner := seedConsoleUser(t, h, "sub-1")
	other := seedConsoleUser(t, h, "sub-2")

	identity, err := svc.Link(t.Context(), owner.ID, issueToken(t, h))
	require.NoError(t, err)

	require.ErrorIs(t, svc.Unlink(t.Context(), other.ID, identity.ID), domain.ErrNotFound)
	require.NoError(t, svc.Unlink(t.Context(), owner.ID, identity.ID))

	list, err := svc.List(t.Context(), owner.ID)
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestUnlinkChatUser(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	svc := usecase.NewIdentityService(h.deps)
	user := seedConsoleUser(t, h, "sub-1")

	_, err := svc.Link(t.Context(), user.ID, issueToken(t, h))
	require.NoError(t, err)

	require.NoError(t, svc.UnlinkChatUser(t.Context(), h.workspac.ID, guest))
	require.ErrorIs(t, svc.UnlinkChatUser(t.Context(), h.workspac.ID, guest), domain.ErrNotFound)
}
