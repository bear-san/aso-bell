package usecase

import (
	"context"
	"fmt"

	"github.com/bear-san/aso-bell/internal/manager/domain"
)

// LinkTokenView は連携ページに表示する内容。未ログインでも見えるため、個人を特定できる情報は含めない。
type LinkTokenView struct {
	WorkspaceName string
	DisplayName   string
	ChatUserID    string
}

// IdentityService はチャットアカウントと ConsoleUser の連携を扱う(ADR 0009)。
type IdentityService struct {
	deps Deps
}

// NewIdentityService は IdentityService を作る。
func NewIdentityService(deps Deps) *IdentityService {
	return &IdentityService{deps: deps.withDefaults()}
}

// IssueLinkToken は `/asobell login` に対する一度きりの連携トークンを発行し、平文トークンを返す。
// 平文はここでしか手に入らない。保存するのは SHA-256 のみ(docs/10-auth-security.md §1.3)。
func (s *IdentityService) IssueLinkToken(
	ctx context.Context,
	workspaceID, chatUserID, displayName string,
) (string, error) {
	ws, err := s.deps.workspace(ctx, workspaceID)
	if err != nil {
		return "", err
	}

	plain, token, err := domain.NewLinkToken(ws.ID, chatUserID, displayName, s.deps.now(), domain.DefaultLinkTokenTTL)
	if err != nil {
		return "", err
	}

	if err = s.deps.Repo.LinkTokens().Create(ctx, token); err != nil {
		return "", fmt.Errorf("create link token: %w", err)
	}

	return plain, nil
}

// GetLinkToken は連携ページ向けにトークンの内容を返す。消費はしない(リンクプレビュー対策)。
func (s *IdentityService) GetLinkToken(ctx context.Context, plain string) (LinkTokenView, error) {
	token, err := s.deps.Repo.LinkTokens().Get(ctx, domain.HashToken(plain))
	if err != nil {
		return LinkTokenView{}, err
	}

	if err = token.Usable(s.deps.now()); err != nil {
		return LinkTokenView{}, err
	}

	ws, err := s.deps.workspace(ctx, token.WorkspaceID)
	if err != nil {
		return LinkTokenView{}, err
	}

	return LinkTokenView{
		WorkspaceName: ws.Name,
		DisplayName:   token.DisplayName,
		ChatUserID:    token.ChatUserID,
	}, nil
}

// Link はトークンを消費してチャットアカウントを ConsoleUser に紐づける。
// 消費は原子的なため、二重クリックでも連携は 1 回しか成立しない。
func (s *IdentityService) Link(ctx context.Context, consoleUserID, plain string) (*domain.ChatIdentity, error) {
	now := s.deps.now()

	token, err := s.deps.Repo.LinkTokens().Consume(ctx, domain.HashToken(plain), now)
	if err != nil {
		return nil, err
	}

	ws, err := s.deps.workspace(ctx, token.WorkspaceID)
	if err != nil {
		return nil, err
	}

	identity, err := s.deps.Repo.ChatIdentities().Link(ctx, &domain.ChatIdentity{
		ConsoleUserID:       consoleUserID,
		WorkspaceID:         ws.ID,
		Provider:            ws.Provider,
		WorkspaceExternalID: ws.ExternalID,
		ChatUserID:          token.ChatUserID,
		DisplayName:         token.DisplayName,
		LinkedAt:            now,
	})
	if err != nil {
		return nil, fmt.Errorf("link chat identity: %w", err)
	}

	return identity, nil
}

// List は ConsoleUser に紐づくチャットアカウントを返す。
func (s *IdentityService) List(ctx context.Context, consoleUserID string) ([]*domain.ChatIdentity, error) {
	return s.deps.Repo.ChatIdentities().ListByUser(ctx, consoleUserID)
}

// Unlink は連携を解除する。他人の連携は解除できない。
func (s *IdentityService) Unlink(ctx context.Context, consoleUserID, identityID string) error {
	identities, err := s.deps.Repo.ChatIdentities().ListByUser(ctx, consoleUserID)
	if err != nil {
		return err
	}

	for _, identity := range identities {
		if identity.ID == identityID {
			return s.deps.Repo.ChatIdentities().Unlink(ctx, identityID)
		}
	}

	return domain.ErrNotFound
}

// UnlinkChatUser は `/asobell unlink` のように、チャット側から本人が解除する場合に使う。
func (s *IdentityService) UnlinkChatUser(ctx context.Context, workspaceID, chatUserID string) error {
	identity, err := s.deps.Repo.ChatIdentities().Get(ctx, workspaceID, chatUserID)
	if err != nil {
		return err
	}

	return s.deps.Repo.ChatIdentities().Unlink(ctx, identity.ID)
}

// ResolveConsoleUser はチャットユーザーに対応する ConsoleUser を返す。未連携なら domain.ErrNotFound。
func (s *IdentityService) ResolveConsoleUser(
	ctx context.Context,
	workspaceID, chatUserID string,
) (*domain.ConsoleUser, error) {
	identity, err := s.deps.Repo.ChatIdentities().Get(ctx, workspaceID, chatUserID)
	if err != nil {
		return nil, err
	}

	return s.deps.Repo.ConsoleUsers().Get(ctx, identity.ConsoleUserID)
}
