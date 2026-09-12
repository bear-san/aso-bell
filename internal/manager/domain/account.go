package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"
)

// TokenBytes は連携トークン・セッション ID・CSRF トークンの乱数長(256 bit)。
const TokenBytes = 32

// DefaultLinkTokenTTL は `/asobell login` で発行するトークンの有効期間(ADR 0009)。
const DefaultLinkTokenTTL = 10 * time.Minute

// AccountOrigin は ConsoleUser の初回ログイン経路。
type AccountOrigin string

// 初回ログインの経路。
const (
	AccountOriginLink      AccountOrigin = "link"
	AccountOriginAllowlist AccountOrigin = "allowlist"
)

// ConsoleUser は WebConsole の利用者。Google アカウントと 1:1。
type ConsoleUser struct {
	ID         string
	GoogleSub  string
	Email      string
	Name       string
	Picture    string
	CreatedVia AccountOrigin

	CreatedAt   time.Time
	LastLoginAt time.Time
}

// ChatIdentity はチャットユーザーと ConsoleUser の紐づけ。`(workspaceId, chatUserId)` で一意。
type ChatIdentity struct {
	ID                  string
	ConsoleUserID       string
	WorkspaceID         string
	Provider            ProviderKind
	WorkspaceExternalID string
	ChatUserID          string
	DisplayName         string
	LinkedAt            time.Time
}

// LinkToken は `/asobell login` が発行する一度きりの連携トークン。ID は平文トークンの SHA-256。
type LinkToken struct {
	ID          string
	WorkspaceID string
	ChatUserID  string
	DisplayName string
	ExpiresAt   time.Time
	ConsumedAt  *time.Time
}

// Session は WebConsole のセッション。ID を Cookie に格納する。
type Session struct {
	ID        string
	UserID    string
	CSRFToken string

	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastSeenAt time.Time
}

// OAuthState は Google ログインの state と PKCE verifier。
type OAuthState struct {
	State        string
	CodeVerifier string
	RedirectTo   string
	ExpiresAt    time.Time
}

// GenerateToken は URL に載せる乱数トークンを base64url で返す。
func GenerateToken() (string, error) {
	buf := make([]byte, TokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// HashToken は平文トークンを永続化用の SHA-256 hex に変換する。平文は保存しない(ADR 0009)。
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))

	return hex.EncodeToString(sum[:])
}

// NewLinkToken は平文トークンと、その保存用レコードを返す。
func NewLinkToken(
	workspaceID, chatUserID, displayName string,
	now time.Time,
	ttl time.Duration,
) (string, LinkToken, error) {
	token, err := GenerateToken()
	if err != nil {
		return "", LinkToken{}, err
	}

	return token, LinkToken{
		ID:          HashToken(token),
		WorkspaceID: workspaceID,
		ChatUserID:  chatUserID,
		DisplayName: displayName,
		ExpiresAt:   now.UTC().Add(ttl),
	}, nil
}

// Usable は消費されておらず、有効期限内かを検査する。
func (t *LinkToken) Usable(now time.Time) error {
	if t.ConsumedAt != nil {
		return ErrLinkTokenConsumed
	}

	if !t.ExpiresAt.After(now) {
		return ErrLinkTokenExpired
	}

	return nil
}

// Expired はセッションの有効期限が切れているかを返す。
func (s *Session) Expired(now time.Time) bool {
	return !s.ExpiresAt.After(now)
}
