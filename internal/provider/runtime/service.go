package runtime

import (
	"context"
	"errors"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/provider/adapter"
)

// GetInfo は種別・能力・接続状態を返す。Manager の状態監視がこれを見る(docs/16-grpc.md §6.2)。
func (r *Runtime) GetInfo(_ context.Context, _ *asobellv1.GetInfoRequest) (*asobellv1.GetInfoResponse, error) {
	return &asobellv1.GetInfoResponse{
		Kind:         r.cfg.Adapter.Kind(),
		Capabilities: r.cfg.Adapter.Capabilities(),
		Version:      r.cfg.Version,
		BotUserId:    r.cfg.Adapter.BotUserID(),
		Connected:    r.cfg.Adapter.Connected(),
	}, nil
}

// ListConnectedWorkspaces は Adapter が把握しているワークスペースを返す。
func (r *Runtime) ListConnectedWorkspaces(
	_ context.Context,
	_ *asobellv1.ListConnectedWorkspacesRequest,
) (*asobellv1.ListConnectedWorkspacesResponse, error) {
	workspaces := r.cfg.Adapter.Workspaces()
	out := make([]*asobellv1.WorkspaceInfo, 0, len(workspaces))

	for _, ws := range workspaces {
		out = append(out, &asobellv1.WorkspaceInfo{ExternalId: ws.ExternalID, Name: ws.Name})
	}

	return &asobellv1.ListConnectedWorkspacesResponse{Workspaces: out}, nil
}

// CreatePrivateChannel はイベント用のプライベートチャンネルを作る。
func (r *Runtime) CreatePrivateChannel(
	ctx context.Context,
	req *asobellv1.CreatePrivateChannelRequest,
) (*asobellv1.CreatePrivateChannelResponse, error) {
	ch, err := r.cfg.Adapter.CreatePrivateChannel(ctx, workspaceRef(req.GetWorkspace()), adapter.CreateChannelInput{
		Name:       req.GetName(),
		Topic:      req.GetTopic(),
		MemberIDs:  req.GetMemberIds(),
		CategoryID: req.GetCategoryId(),
	})
	if err != nil {
		return nil, toStatus(err, notFoundChannel)
	}

	return &asobellv1.CreatePrivateChannelResponse{Channel: protoChannel(ch)}, nil
}

// AddMember はチャンネルへユーザーを追加する。既にメンバーなら成功として already_member を返す。
func (r *Runtime) AddMember(
	ctx context.Context,
	req *asobellv1.AddMemberRequest,
) (*asobellv1.AddMemberResponse, error) {
	err := r.cfg.Adapter.AddMember(ctx, workspaceRef(req.GetWorkspace()), req.GetChannelId(), req.GetUserId())

	switch {
	case err == nil:
		return &asobellv1.AddMemberResponse{}, nil
	case errors.Is(err, adapter.ErrAlreadyMember):
		return &asobellv1.AddMemberResponse{AlreadyMember: true}, nil
	default:
		return nil, toStatus(err, notFoundChannel)
	}
}

// RemoveMember はチャンネルからユーザーを外す。メンバーでなければ成功として was_member=false を返す。
func (r *Runtime) RemoveMember(
	ctx context.Context,
	req *asobellv1.RemoveMemberRequest,
) (*asobellv1.RemoveMemberResponse, error) {
	err := r.cfg.Adapter.RemoveMember(ctx, workspaceRef(req.GetWorkspace()), req.GetChannelId(), req.GetUserId())

	switch {
	case err == nil:
		return &asobellv1.RemoveMemberResponse{WasMember: true}, nil
	case errors.Is(err, adapter.ErrNotMember):
		return &asobellv1.RemoveMemberResponse{}, nil
	default:
		return nil, toStatus(err, notFoundChannel)
	}
}

// ArchiveChannel はチャンネルをアーカイブする。
func (r *Runtime) ArchiveChannel(
	ctx context.Context,
	req *asobellv1.ArchiveChannelRequest,
) (*asobellv1.ArchiveChannelResponse, error) {
	ch, err := r.cfg.Adapter.ArchiveChannel(
		ctx, workspaceRef(req.GetWorkspace()), req.GetChannelId(), req.GetArchiveCategoryId(),
	)
	if err != nil {
		return nil, toStatus(err, notFoundChannel)
	}

	return &asobellv1.ArchiveChannelResponse{Channel: protoChannel(ch)}, nil
}

// ListChannels はワークスペースのチャンネルをページングして返す。
func (r *Runtime) ListChannels(
	ctx context.Context,
	req *asobellv1.ListChannelsRequest,
) (*asobellv1.ListChannelsResponse, error) {
	channels, next, err := r.cfg.Adapter.ListChannels(
		ctx, workspaceRef(req.GetWorkspace()), req.GetPageToken(), int(req.GetPageSize()),
	)
	if err != nil {
		return nil, toStatus(err, notFoundChannel)
	}

	out := make([]*asobellv1.ChannelInfo, 0, len(channels))
	for _, ch := range channels {
		out = append(out, protoChannel(ch))
	}

	return &asobellv1.ListChannelsResponse{Channels: out, NextPageToken: next}, nil
}

// PostMessage はチャンネルへ投稿する。
func (r *Runtime) PostMessage(
	ctx context.Context,
	req *asobellv1.PostMessageRequest,
) (*asobellv1.PostMessageResponse, error) {
	ref, err := r.cfg.Adapter.PostMessage(ctx, workspaceRef(req.GetWorkspace()), req.GetChannelId(), req.GetMessage())
	if err != nil {
		return nil, toStatus(err, notFoundChannel)
	}

	return &asobellv1.PostMessageResponse{Ref: protoMessageRef(ref)}, nil
}

// UpdateMessage は投稿済みメッセージを差し替える。
func (r *Runtime) UpdateMessage(
	ctx context.Context,
	req *asobellv1.UpdateMessageRequest,
) (*asobellv1.UpdateMessageResponse, error) {
	err := r.cfg.Adapter.UpdateMessage(
		ctx, workspaceRef(req.GetWorkspace()), messageRef(req.GetRef()), req.GetMessage(),
	)
	if err != nil {
		return nil, toStatus(err, notFoundMessage)
	}

	return &asobellv1.UpdateMessageResponse{}, nil
}

// PostEphemeral は本人にだけ見える投稿を行う。非対応・チャンネル不明なら DM へ切り替える(docs/06 §3.2)。
func (r *Runtime) PostEphemeral(
	ctx context.Context,
	req *asobellv1.PostEphemeralRequest,
) (*asobellv1.PostEphemeralResponse, error) {
	ws := workspaceRef(req.GetWorkspace())

	err := r.cfg.Adapter.PostEphemeral(ctx, ws, req.GetChannelId(), req.GetUserId(), req.GetMessage())
	if err == nil {
		return &asobellv1.PostEphemeralResponse{}, nil
	}

	if !isMissing(err) {
		return nil, toStatus(err, notFoundChannel)
	}

	r.cfg.Logger.InfoContext(ctx, "falling back to direct message",
		"channel_id", req.GetChannelId(), "user_id", req.GetUserId(), "cause", err)

	if _, dmErr := r.cfg.Adapter.SendDirectMessage(ctx, ws, req.GetUserId(), req.GetMessage()); dmErr != nil {
		return nil, toStatus(dmErr, notFoundUser)
	}

	return &asobellv1.PostEphemeralResponse{DeliveredViaDm: true}, nil
}

// SendDirectMessage は DM を送る。
func (r *Runtime) SendDirectMessage(
	ctx context.Context,
	req *asobellv1.SendDirectMessageRequest,
) (*asobellv1.SendDirectMessageResponse, error) {
	ref, err := r.cfg.Adapter.SendDirectMessage(
		ctx, workspaceRef(req.GetWorkspace()), req.GetUserId(), req.GetMessage(),
	)
	if err != nil {
		return nil, toStatus(err, notFoundUser)
	}

	return &asobellv1.SendDirectMessageResponse{Ref: protoMessageRef(ref)}, nil
}

// PinMessage はメッセージをピン留めする。
func (r *Runtime) PinMessage(
	ctx context.Context,
	req *asobellv1.PinMessageRequest,
) (*asobellv1.PinMessageResponse, error) {
	if err := r.cfg.Adapter.PinMessage(ctx, workspaceRef(req.GetWorkspace()), messageRef(req.GetRef())); err != nil {
		return nil, toStatus(err, notFoundMessage)
	}

	return &asobellv1.PinMessageResponse{}, nil
}

// UnpinMessage はピン留めを外す。
func (r *Runtime) UnpinMessage(
	ctx context.Context,
	req *asobellv1.UnpinMessageRequest,
) (*asobellv1.UnpinMessageResponse, error) {
	if err := r.cfg.Adapter.UnpinMessage(ctx, workspaceRef(req.GetWorkspace()), messageRef(req.GetRef())); err != nil {
		return nil, toStatus(err, notFoundMessage)
	}

	return &asobellv1.UnpinMessageResponse{}, nil
}

// ResolveUser はユーザーの表示名を解決する。
func (r *Runtime) ResolveUser(
	ctx context.Context,
	req *asobellv1.ResolveUserRequest,
) (*asobellv1.ResolveUserResponse, error) {
	user, err := r.cfg.Adapter.ResolveUser(ctx, workspaceRef(req.GetWorkspace()), req.GetUserId())
	if err != nil {
		return nil, toStatus(err, notFoundUser)
	}

	return &asobellv1.ResolveUserResponse{User: &asobellv1.UserInfo{
		Id:          user.ID,
		DisplayName: user.DisplayName,
		IsBot:       user.IsBot,
	}}, nil
}
