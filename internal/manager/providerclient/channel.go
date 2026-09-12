package providerclient

import (
	"context"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
)

// CreatePrivateChannel はイベント用のプライベートチャンネルを作る。
// 名前が重複した場合は usecase.ErrNameTaken を返す。
func (c *Client) CreatePrivateChannel(
	ctx context.Context,
	ws usecase.WorkspaceRef,
	in usecase.CreateChannelInput,
) (usecase.ChannelInfo, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	res, err := c.rpc.CreatePrivateChannel(ctx, &asobellv1.CreatePrivateChannelRequest{
		Workspace:  workspaceRef(ws),
		Name:       in.Name,
		Topic:      in.Topic,
		MemberIds:  in.MemberIDs,
		CategoryId: in.CategoryID,
	})
	if err != nil {
		return usecase.ChannelInfo{}, mapError(err, "create private channel")
	}

	return channelInfo(res.GetChannel()), nil
}

// AddMember はユーザーをチャンネルへ追加する。既にメンバーなら alreadyMember=true。
func (c *Client) AddMember(ctx context.Context, ws usecase.WorkspaceRef, channelID, userID string) (bool, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	res, err := c.rpc.AddMember(ctx, &asobellv1.AddMemberRequest{
		Workspace: workspaceRef(ws),
		ChannelId: channelID,
		UserId:    userID,
	})
	if err != nil {
		return false, mapError(err, "add member")
	}

	return res.GetAlreadyMember(), nil
}

// RemoveMember はユーザーをチャンネルから外す。メンバーでなければ wasMember=false。
func (c *Client) RemoveMember(ctx context.Context, ws usecase.WorkspaceRef, channelID, userID string) (bool, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	res, err := c.rpc.RemoveMember(ctx, &asobellv1.RemoveMemberRequest{
		Workspace: workspaceRef(ws),
		ChannelId: channelID,
		UserId:    userID,
	})
	if err != nil {
		return false, mapError(err, "remove member")
	}

	return res.GetWasMember(), nil
}

// ArchiveChannel はチャンネルをアーカイブする。冪等で、既にアーカイブ済みでも成功する。
func (c *Client) ArchiveChannel(
	ctx context.Context,
	ws usecase.WorkspaceRef,
	channelID, archiveCategoryID string,
) (usecase.ChannelInfo, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	res, err := c.rpc.ArchiveChannel(ctx, &asobellv1.ArchiveChannelRequest{
		Workspace:         workspaceRef(ws),
		ChannelId:         channelID,
		ArchiveCategoryId: archiveCategoryID,
	})
	if err != nil {
		return usecase.ChannelInfo{}, mapError(err, "archive channel")
	}

	return channelInfo(res.GetChannel()), nil
}

// ListChannels はワークスペースのチャンネルを 1 ページ分返す。第 2 戻り値は次ページのトークン。
func (c *Client) ListChannels(
	ctx context.Context,
	ws usecase.WorkspaceRef,
	pageToken string,
	pageSize int,
) ([]usecase.ChannelInfo, string, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	res, err := c.rpc.ListChannels(ctx, &asobellv1.ListChannelsRequest{
		Workspace: workspaceRef(ws),
		PageSize:  int32(pageSize), //nolint:gosec // pageSize は API のページ上限で、int32 を超える値は渡らない
		PageToken: pageToken,
	})
	if err != nil {
		return nil, "", mapError(err, "list channels")
	}

	out := make([]usecase.ChannelInfo, 0, len(res.GetChannels()))
	for _, ch := range res.GetChannels() {
		out = append(out, channelInfo(ch))
	}

	return out, res.GetNextPageToken(), nil
}
