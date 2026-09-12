// Package fake は Provider のテスト用実装を提供する。
// ProviderServer は Manager 側のテストで bufconn 上に起動する ProviderService のインメモリ実装で、
// Adapter のフェイクと同じ振る舞い(冪等性、エラー契約)を再現する(docs/06-provider.md §8)。
package fake

import (
	"context"
	"fmt"
	"sync"

	"google.golang.org/grpc/codes"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/shared/rpcerr"
)

// Channel はフェイクが保持するチャンネル。
type Channel struct {
	ID          string
	Name        string
	WorkspaceID string
	Topic       string
	IsPrivate   bool
	Archived    bool
	Members     []string
}

// Post はフェイクへ投稿されたメッセージ。
type Post struct {
	Ref       *asobellv1.MessageRef
	Message   *asobellv1.Message
	Pinned    bool
	Ephemeral bool
	// DirectTo は DM の宛先ユーザー。通常投稿では空。
	DirectTo string
	// UserID は ephemeral の宛先ユーザー。
	UserID string
}

// Call は呼び出し履歴の 1 件。
type Call struct {
	Method      string
	WorkspaceID string
	ChannelID   string
	UserID      string
	MessageID   string
}

// ProviderServer は ProviderService のインメモリ実装。ゼロ値ではなく New で作る。
type ProviderServer struct {
	asobellv1.UnimplementedProviderServiceServer

	mu           sync.Mutex
	kind         asobellv1.ProviderKind
	capabilities *asobellv1.Capabilities
	version      string
	botUserID    string
	connected    bool
	workspaces   []*asobellv1.WorkspaceInfo
	channels     map[string]*Channel
	posts        []*Post
	users        map[string]*asobellv1.UserInfo
	calls        []Call
	failures     map[string][]error
	seq          int
}

// Option は ProviderServer の初期状態を変える。
type Option func(*ProviderServer)

// WithKind は Provider 種別を設定する。
func WithKind(kind asobellv1.ProviderKind) Option {
	return func(s *ProviderServer) { s.kind = kind }
}

// WithCapabilities は能力申告を設定する。
func WithCapabilities(caps *asobellv1.Capabilities) Option {
	return func(s *ProviderServer) { s.capabilities = caps }
}

// WithConnected はチャットツールへの接続状態を設定する。
func WithConnected(connected bool) Option {
	return func(s *ProviderServer) { s.connected = connected }
}

// WithWorkspaces は ListConnectedWorkspaces が返すワークスペースを設定する。
func WithWorkspaces(workspaces ...*asobellv1.WorkspaceInfo) Option {
	return func(s *ProviderServer) { s.workspaces = workspaces }
}

// New は既定で Slack 相当(forms / ephemeral / DM すべて対応、接続済み)のフェイクを作る。
func New(opts ...Option) *ProviderServer {
	s := &ProviderServer{
		kind: asobellv1.ProviderKind_PROVIDER_KIND_SLACK,
		capabilities: &asobellv1.Capabilities{
			Forms:         true,
			Ephemeral:     true,
			DirectMessage: true,
		},
		version:   "fake",
		botUserID: "UBOT",
		connected: true,
		channels:  map[string]*Channel{},
		users:     map[string]*asobellv1.UserInfo{},
		failures:  map[string][]error{},
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// FailNext は method の次の呼び出しで err を返させる。複数回呼べばその順に消費される。
func (s *ProviderServer) FailNext(method string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.failures[method] = append(s.failures[method], err)
}

// SetConnected はチャットツールへの接続状態を変える。
func (s *ProviderServer) SetConnected(connected bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.connected = connected
}

// SetUser は ResolveUser が返すユーザーを登録する。
func (s *ProviderServer) SetUser(user *asobellv1.UserInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.users[user.GetId()] = user
}

// SetWorkspaces は ListConnectedWorkspaces が返すワークスペースを差し替える。
func (s *ProviderServer) SetWorkspaces(workspaces ...*asobellv1.WorkspaceInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.workspaces = workspaces
}

// Calls は呼び出し履歴を返す。
func (s *ProviderServer) Calls() []Call {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]Call(nil), s.calls...)
}

// CallsOf は method の呼び出し履歴だけを返す。
func (s *ProviderServer) CallsOf(method string) []Call {
	var out []Call

	for _, call := range s.Calls() {
		if call.Method == method {
			out = append(out, call)
		}
	}

	return out
}

// Channels は現在のチャンネルを返す。
func (s *ProviderServer) Channels() []Channel {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Channel, 0, len(s.channels))
	for _, ch := range s.channels {
		out = append(out, *ch)
	}

	return out
}

// Channel は ID でチャンネルを返す。存在しなければ false。
func (s *ProviderServer) Channel(id string) (Channel, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch, ok := s.channels[id]
	if !ok {
		return Channel{}, false
	}

	return *ch, true
}

// Posts は投稿されたメッセージを投稿順で返す。
func (s *ProviderServer) Posts() []Post {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Post, 0, len(s.posts))
	for _, p := range s.posts {
		out = append(out, *p)
	}

	return out
}

// PostsIn は特定チャンネルへの投稿だけを返す。
func (s *ProviderServer) PostsIn(channelID string) []Post {
	var out []Post

	for _, p := range s.Posts() {
		if p.Ref.GetChannelId() == channelID {
			out = append(out, p)
		}
	}

	return out
}

// GetInfo は種別・能力・接続状態を返す。
func (s *ProviderServer) GetInfo(_ context.Context, _ *asobellv1.GetInfoRequest) (*asobellv1.GetInfoResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.failLocked("GetInfo", Call{Method: "GetInfo"}); err != nil {
		return nil, err
	}

	return &asobellv1.GetInfoResponse{
		Kind:         s.kind,
		Capabilities: s.capabilities,
		Version:      s.version,
		BotUserId:    s.botUserID,
		Connected:    s.connected,
	}, nil
}

// ListConnectedWorkspaces は設定済みのワークスペースを返す。
func (s *ProviderServer) ListConnectedWorkspaces(
	_ context.Context,
	_ *asobellv1.ListConnectedWorkspacesRequest,
) (*asobellv1.ListConnectedWorkspacesResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.failLocked("ListConnectedWorkspaces", Call{Method: "ListConnectedWorkspaces"}); err != nil {
		return nil, err
	}

	return &asobellv1.ListConnectedWorkspacesResponse{Workspaces: s.workspaces}, nil
}

// CreatePrivateChannel は同名チャンネルがあれば NAME_TAKEN を返す。
func (s *ProviderServer) CreatePrivateChannel(
	_ context.Context,
	req *asobellv1.CreatePrivateChannelRequest,
) (*asobellv1.CreatePrivateChannelResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	call := Call{Method: "CreatePrivateChannel", WorkspaceID: req.GetWorkspace().GetExternalId()}
	if err := s.failLocked("CreatePrivateChannel", call); err != nil {
		return nil, err
	}

	for _, ch := range s.channels {
		if ch.Name == req.GetName() && !ch.Archived {
			return nil, rpcerr.New(
				codes.AlreadyExists,
				rpcerr.ReasonNameTaken,
				fmt.Sprintf("channel %q already exists", req.GetName()),
				nil,
			)
		}
	}

	s.seq++

	ch := &Channel{
		ID:          fmt.Sprintf("C%03d", s.seq),
		Name:        req.GetName(),
		WorkspaceID: req.GetWorkspace().GetExternalId(),
		Topic:       req.GetTopic(),
		IsPrivate:   true,
		Members:     append([]string{s.botUserID}, req.GetMemberIds()...),
	}
	s.channels[ch.ID] = ch

	return &asobellv1.CreatePrivateChannelResponse{Channel: channelInfo(ch)}, nil
}

// AddMember はチャンネルへユーザーを追加する。既にメンバーなら already_member を返す。
func (s *ProviderServer) AddMember(
	_ context.Context,
	req *asobellv1.AddMemberRequest,
) (*asobellv1.AddMemberResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	call := Call{
		Method:      "AddMember",
		WorkspaceID: req.GetWorkspace().GetExternalId(),
		ChannelID:   req.GetChannelId(),
		UserID:      req.GetUserId(),
	}
	if err := s.failLocked("AddMember", call); err != nil {
		return nil, err
	}

	ch, err := s.channelLocked(req.GetChannelId())
	if err != nil {
		return nil, err
	}

	for _, m := range ch.Members {
		if m == req.GetUserId() {
			return &asobellv1.AddMemberResponse{AlreadyMember: true}, nil
		}
	}

	ch.Members = append(ch.Members, req.GetUserId())

	return &asobellv1.AddMemberResponse{}, nil
}

// RemoveMember はチャンネルからユーザーを外す。メンバーでなければ was_member=false を返す。
func (s *ProviderServer) RemoveMember(
	_ context.Context,
	req *asobellv1.RemoveMemberRequest,
) (*asobellv1.RemoveMemberResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	call := Call{
		Method:      "RemoveMember",
		WorkspaceID: req.GetWorkspace().GetExternalId(),
		ChannelID:   req.GetChannelId(),
		UserID:      req.GetUserId(),
	}
	if err := s.failLocked("RemoveMember", call); err != nil {
		return nil, err
	}

	ch, err := s.channelLocked(req.GetChannelId())
	if err != nil {
		return nil, err
	}

	for i, m := range ch.Members {
		if m == req.GetUserId() {
			ch.Members = append(ch.Members[:i], ch.Members[i+1:]...)

			return &asobellv1.RemoveMemberResponse{WasMember: true}, nil
		}
	}

	return &asobellv1.RemoveMemberResponse{}, nil
}

// ArchiveChannel はチャンネルをアーカイブする。二度目以降も成功扱い。
func (s *ProviderServer) ArchiveChannel(
	_ context.Context,
	req *asobellv1.ArchiveChannelRequest,
) (*asobellv1.ArchiveChannelResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	call := Call{
		Method:      "ArchiveChannel",
		WorkspaceID: req.GetWorkspace().GetExternalId(),
		ChannelID:   req.GetChannelId(),
	}
	if err := s.failLocked("ArchiveChannel", call); err != nil {
		return nil, err
	}

	ch, err := s.channelLocked(req.GetChannelId())
	if err != nil {
		return nil, err
	}

	ch.Archived = true

	return &asobellv1.ArchiveChannelResponse{Channel: channelInfo(ch)}, nil
}

// ListChannels はワークスペースのチャンネルを返す。ページングは行わない。
func (s *ProviderServer) ListChannels(
	_ context.Context,
	req *asobellv1.ListChannelsRequest,
) (*asobellv1.ListChannelsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	call := Call{Method: "ListChannels", WorkspaceID: req.GetWorkspace().GetExternalId()}
	if err := s.failLocked("ListChannels", call); err != nil {
		return nil, err
	}

	out := make([]*asobellv1.ChannelInfo, 0, len(s.channels))
	for _, ch := range s.channels {
		if ch.WorkspaceID == req.GetWorkspace().GetExternalId() {
			out = append(out, channelInfo(ch))
		}
	}

	return &asobellv1.ListChannelsResponse{Channels: out}, nil
}

// PostMessage はチャンネルへ投稿し、参照を返す。
func (s *ProviderServer) PostMessage(
	_ context.Context,
	req *asobellv1.PostMessageRequest,
) (*asobellv1.PostMessageResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	call := Call{
		Method:      "PostMessage",
		WorkspaceID: req.GetWorkspace().GetExternalId(),
		ChannelID:   req.GetChannelId(),
	}
	if err := s.failLocked("PostMessage", call); err != nil {
		return nil, err
	}

	ref := s.newRefLocked(req.GetChannelId())
	s.posts = append(s.posts, &Post{Ref: ref, Message: req.GetMessage()})

	return &asobellv1.PostMessageResponse{Ref: ref}, nil
}

// UpdateMessage は投稿済みメッセージを差し替える。未知の参照は MESSAGE_NOT_FOUND。
func (s *ProviderServer) UpdateMessage(
	_ context.Context,
	req *asobellv1.UpdateMessageRequest,
) (*asobellv1.UpdateMessageResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	call := Call{
		Method:      "UpdateMessage",
		WorkspaceID: req.GetWorkspace().GetExternalId(),
		ChannelID:   req.GetRef().GetChannelId(),
		MessageID:   req.GetRef().GetMessageId(),
	}
	if err := s.failLocked("UpdateMessage", call); err != nil {
		return nil, err
	}

	post := s.postLocked(req.GetRef())
	if post == nil {
		return nil, rpcerr.New(codes.NotFound, rpcerr.ReasonMessageNotFound, "message not found", nil)
	}

	post.Message = req.GetMessage()

	return &asobellv1.UpdateMessageResponse{}, nil
}

// PostEphemeral は本人向け投稿を記録する。ephemeral 非対応なら UNSUPPORTED を返す。
func (s *ProviderServer) PostEphemeral(
	_ context.Context,
	req *asobellv1.PostEphemeralRequest,
) (*asobellv1.PostEphemeralResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	call := Call{
		Method:      "PostEphemeral",
		WorkspaceID: req.GetWorkspace().GetExternalId(),
		ChannelID:   req.GetChannelId(),
		UserID:      req.GetUserId(),
	}
	if err := s.failLocked("PostEphemeral", call); err != nil {
		return nil, err
	}

	if !s.capabilities.GetEphemeral() {
		return nil, rpcerr.New(codes.Unimplemented, rpcerr.ReasonUnsupported, "ephemeral not supported", nil)
	}

	ref := s.newRefLocked(req.GetChannelId())
	s.posts = append(s.posts, &Post{
		Ref:       ref,
		Message:   req.GetMessage(),
		Ephemeral: true,
		UserID:    req.GetUserId(),
	})

	return &asobellv1.PostEphemeralResponse{}, nil
}

// SendDirectMessage は DM を記録する。DM 非対応なら DM_BLOCKED を返す。
func (s *ProviderServer) SendDirectMessage(
	_ context.Context,
	req *asobellv1.SendDirectMessageRequest,
) (*asobellv1.SendDirectMessageResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	call := Call{
		Method:      "SendDirectMessage",
		WorkspaceID: req.GetWorkspace().GetExternalId(),
		UserID:      req.GetUserId(),
	}
	if err := s.failLocked("SendDirectMessage", call); err != nil {
		return nil, err
	}

	if !s.capabilities.GetDirectMessage() {
		return nil, rpcerr.New(codes.FailedPrecondition, rpcerr.ReasonDMBlocked, "dm not supported", nil)
	}

	ref := s.newRefLocked("D" + req.GetUserId())
	s.posts = append(s.posts, &Post{Ref: ref, Message: req.GetMessage(), DirectTo: req.GetUserId()})

	return &asobellv1.SendDirectMessageResponse{Ref: ref}, nil
}

// PinMessage はピン留めする。既にピン留め済みでも成功扱い。
func (s *ProviderServer) PinMessage(
	_ context.Context,
	req *asobellv1.PinMessageRequest,
) (*asobellv1.PinMessageResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	call := Call{
		Method:      "PinMessage",
		WorkspaceID: req.GetWorkspace().GetExternalId(),
		ChannelID:   req.GetRef().GetChannelId(),
		MessageID:   req.GetRef().GetMessageId(),
	}
	if err := s.failLocked("PinMessage", call); err != nil {
		return nil, err
	}

	post := s.postLocked(req.GetRef())
	if post == nil {
		return nil, rpcerr.New(codes.NotFound, rpcerr.ReasonMessageNotFound, "message not found", nil)
	}

	post.Pinned = true

	return &asobellv1.PinMessageResponse{}, nil
}

// UnpinMessage はピン留めを解除する。未ピンでも成功扱い。
func (s *ProviderServer) UnpinMessage(
	_ context.Context,
	req *asobellv1.UnpinMessageRequest,
) (*asobellv1.UnpinMessageResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	call := Call{
		Method:      "UnpinMessage",
		WorkspaceID: req.GetWorkspace().GetExternalId(),
		ChannelID:   req.GetRef().GetChannelId(),
		MessageID:   req.GetRef().GetMessageId(),
	}
	if err := s.failLocked("UnpinMessage", call); err != nil {
		return nil, err
	}

	post := s.postLocked(req.GetRef())
	if post == nil {
		return &asobellv1.UnpinMessageResponse{}, nil
	}

	post.Pinned = false

	return &asobellv1.UnpinMessageResponse{}, nil
}

// ResolveUser は登録済みユーザーを返す。未登録の ID は ID をそのまま表示名として返す。
func (s *ProviderServer) ResolveUser(
	_ context.Context,
	req *asobellv1.ResolveUserRequest,
) (*asobellv1.ResolveUserResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	call := Call{
		Method:      "ResolveUser",
		WorkspaceID: req.GetWorkspace().GetExternalId(),
		UserID:      req.GetUserId(),
	}
	if err := s.failLocked("ResolveUser", call); err != nil {
		return nil, err
	}

	if user, ok := s.users[req.GetUserId()]; ok {
		return &asobellv1.ResolveUserResponse{User: user}, nil
	}

	return &asobellv1.ResolveUserResponse{
		User: &asobellv1.UserInfo{Id: req.GetUserId(), DisplayName: req.GetUserId()},
	}, nil
}

// failLocked は呼び出しを記録し、FailNext で仕込まれたエラーがあれば消費して返す。
func (s *ProviderServer) failLocked(method string, call Call) error {
	s.calls = append(s.calls, call)

	queued := s.failures[method]
	if len(queued) == 0 {
		return nil
	}

	s.failures[method] = queued[1:]

	return queued[0]
}

func (s *ProviderServer) channelLocked(id string) (*Channel, error) {
	ch, ok := s.channels[id]
	if !ok {
		return nil, rpcerr.New(codes.NotFound, rpcerr.ReasonChannelNotFound, "channel not found", nil)
	}

	return ch, nil
}

func (s *ProviderServer) postLocked(ref *asobellv1.MessageRef) *Post {
	for _, p := range s.posts {
		if p.Ref.GetChannelId() == ref.GetChannelId() && p.Ref.GetMessageId() == ref.GetMessageId() {
			return p
		}
	}

	return nil
}

func (s *ProviderServer) newRefLocked(channelID string) *asobellv1.MessageRef {
	s.seq++

	return &asobellv1.MessageRef{ChannelId: channelID, MessageId: fmt.Sprintf("M%03d", s.seq)}
}

func channelInfo(ch *Channel) *asobellv1.ChannelInfo {
	return &asobellv1.ChannelInfo{
		Id:        ch.ID,
		Name:      ch.Name,
		IsPrivate: ch.IsPrivate,
		Archived:  ch.Archived,
	}
}
