package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bear-san/aso-bell/internal/manager/domain"
)

func TestGenerateTokenIsUnique(t *testing.T) {
	t.Parallel()

	first, err := domain.GenerateToken()
	require.NoError(t, err)

	second, err := domain.GenerateToken()
	require.NoError(t, err)

	assert.NotEqual(t, first, second)
	assert.NotEmpty(t, first)
	assert.NotContains(t, first, "=")
}

func TestHashTokenIsStable(t *testing.T) {
	t.Parallel()

	hashed := domain.HashToken("abc")

	assert.Equal(t, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", hashed)
	assert.NotEqual(t, hashed, domain.HashToken("abd"))
}

func TestNewLinkToken(t *testing.T) {
	t.Parallel()

	now := testNow()

	plain, rec, err := domain.NewLinkToken(testWorkspaceID, "U0123", "kentaro", now, domain.DefaultLinkTokenTTL)
	require.NoError(t, err)

	assert.Equal(t, domain.HashToken(plain), rec.ID)
	assert.NotContains(t, rec.ID, plain)
	assert.Equal(t, now.Add(domain.DefaultLinkTokenTTL), rec.ExpiresAt)
	require.NoError(t, rec.Usable(now))
}

func TestLinkTokenUsable(t *testing.T) {
	t.Parallel()

	now := testNow()

	_, rec, err := domain.NewLinkToken(testWorkspaceID, "U0123", "kentaro", now, domain.DefaultLinkTokenTTL)
	require.NoError(t, err)

	require.ErrorIs(t, rec.Usable(now.Add(domain.DefaultLinkTokenTTL)), domain.ErrLinkTokenExpired)

	consumedAt := now.Add(time.Minute)
	rec.ConsumedAt = &consumedAt
	require.ErrorIs(t, rec.Usable(now), domain.ErrLinkTokenConsumed)
}

func TestSessionExpired(t *testing.T) {
	t.Parallel()

	now := testNow()
	s := domain.Session{ExpiresAt: now.Add(time.Hour)}

	assert.False(t, s.Expired(now))
	assert.True(t, s.Expired(now.Add(time.Hour)))
}
