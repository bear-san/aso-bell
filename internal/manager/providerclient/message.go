package providerclient

import (
	"context"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
)

// PostMessage はチャンネルへ投稿し、投稿の参照を返す。
func (c *Client) PostMessage(
	ctx context.Context,
	ws usecase.WorkspaceRef,
	channelID string,
	msg *asobellv1.Message,
) (domain.MessageRef, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	res, err := c.rpc.PostMessage(ctx, &asobellv1.PostMessageRequest{
		Workspace: workspaceRef(ws),
		ChannelId: channelID,
		Message:   msg,
	})
	if err != nil {
		return domain.MessageRef{}, mapError(err, "post message")
	}

	return messageRef(res.GetRef()), nil
}

// UpdateMessage は投稿済みメッセージを置き換える。
func (c *Client) UpdateMessage(
	ctx context.Context,
	ws usecase.WorkspaceRef,
	ref domain.MessageRef,
	msg *asobellv1.Message,
) error {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	if _, err := c.rpc.UpdateMessage(ctx, &asobellv1.UpdateMessageRequest{
		Workspace: workspaceRef(ws),
		Ref:       protoMessageRef(ref),
		Message:   msg,
	}); err != nil {
		return mapError(err, "update message")
	}

	return nil
}

// PostEphemeral は本人にだけ見えるメッセージを送る。Provider が非対応なら DM で代替される。
func (c *Client) PostEphemeral(
	ctx context.Context,
	ws usecase.WorkspaceRef,
	channelID, userID string,
	msg *asobellv1.Message,
) error {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	if _, err := c.rpc.PostEphemeral(ctx, &asobellv1.PostEphemeralRequest{
		Workspace: workspaceRef(ws),
		ChannelId: channelID,
		UserId:    userID,
		Message:   msg,
	}); err != nil {
		return mapError(err, "post ephemeral")
	}

	return nil
}

// SendDirectMessage はユーザーへ DM を送る。
func (c *Client) SendDirectMessage(
	ctx context.Context,
	ws usecase.WorkspaceRef,
	userID string,
	msg *asobellv1.Message,
) (domain.MessageRef, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	res, err := c.rpc.SendDirectMessage(ctx, &asobellv1.SendDirectMessageRequest{
		Workspace: workspaceRef(ws),
		UserId:    userID,
		Message:   msg,
	})
	if err != nil {
		return domain.MessageRef{}, mapError(err, "send direct message")
	}

	return messageRef(res.GetRef()), nil
}

// PinMessage は投稿をピン留めする。
func (c *Client) PinMessage(ctx context.Context, ws usecase.WorkspaceRef, ref domain.MessageRef) error {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	if _, err := c.rpc.PinMessage(ctx, &asobellv1.PinMessageRequest{
		Workspace: workspaceRef(ws),
		Ref:       protoMessageRef(ref),
	}); err != nil {
		return mapError(err, "pin message")
	}

	return nil
}

// UnpinMessage はピン留めを解除する。
func (c *Client) UnpinMessage(ctx context.Context, ws usecase.WorkspaceRef, ref domain.MessageRef) error {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	if _, err := c.rpc.UnpinMessage(ctx, &asobellv1.UnpinMessageRequest{
		Workspace: workspaceRef(ws),
		Ref:       protoMessageRef(ref),
	}); err != nil {
		return mapError(err, "unpin message")
	}

	return nil
}

// ResolveUser はチャットユーザーの表示名を取得する。
func (c *Client) ResolveUser(
	ctx context.Context,
	ws usecase.WorkspaceRef,
	userID string,
) (usecase.UserInfo, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	res, err := c.rpc.ResolveUser(ctx, &asobellv1.ResolveUserRequest{
		Workspace: workspaceRef(ws),
		UserId:    userID,
	})
	if err != nil {
		return usecase.UserInfo{}, mapError(err, "resolve user")
	}

	user := res.GetUser()

	return usecase.UserInfo{ID: user.GetId(), DisplayName: user.GetDisplayName(), IsBot: user.GetIsBot()}, nil
}
