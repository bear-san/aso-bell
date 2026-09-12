package fake

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/provider/adapter"
)

// archivePrefix は Discord と同じく、アーカイブしたチャンネル名に付ける接頭辞。
const archivePrefix = "archived_"

// Adapter は adapter.Adapter のインメモリ実装。Runtime のテストと provider-fake バイナリで使う。
// 実装は Slack / Discord の契約(冪等性、センチネルエラー)を再現する(docs/06-provider.md §8)。
type Adapter struct {
	mu           sync.Mutex
	kind         asobellv1.ProviderKind
	capabilities *asobellv1.Capabilities
	botUserID    string
	connected    bool
	sink         adapter.InboundSink
	workspaces   []adapter.WorkspaceInfo
	channels     map[string]*Channel
	posts        []*Post
	users        map[string]adapter.UserInfo
	calls        []Call
	failures     map[string][]error
	seq          int
}

// AdapterOption は Adapter の初期状態を変える。
type AdapterOption func(*Adapter)

// WithAdapterKind は Provider 種別を設定する。
func WithAdapterKind(kind asobellv1.ProviderKind) AdapterOption {
	return func(a *Adapter) { a.kind = kind }
}

// WithAdapterCapabilities は能力申告を設定する。
func WithAdapterCapabilities(caps *asobellv1.Capabilities) AdapterOption {
	return func(a *Adapter) { a.capabilities = caps }
}

// WithAdapterWorkspaces は接続時点で判明しているワークスペースを設定する。
func WithAdapterWorkspaces(workspaces ...adapter.WorkspaceInfo) AdapterOption {
	return func(a *Adapter) { a.workspaces = workspaces }
}

// NewAdapter は Adapter を作る。
func NewAdapter(opts ...AdapterOption) *Adapter {
	a := &Adapter{
		kind:         asobellv1.ProviderKind_PROVIDER_KIND_SLACK,
		capabilities: &asobellv1.Capabilities{Forms: true, Ephemeral: true, DirectMessage: true},
		botUserID:    "UBOT",
		channels:     map[string]*Channel{},
		users:        map[string]adapter.UserInfo{},
		failures:     map[string][]error{},
		workspaces:   []adapter.WorkspaceInfo{{ExternalID: "T0123", Name: "あそび部"}},
	}

	for _, opt := range opts {
		opt(a)
	}

	return a
}

// Kind は Provider 種別を返す。
func (a *Adapter) Kind() asobellv1.ProviderKind { return a.kind }

// Capabilities は能力申告を返す。
func (a *Adapter) Capabilities() *asobellv1.Capabilities { return a.capabilities }

// BotUserID は Bot 自身のユーザー ID を返す。
func (a *Adapter) BotUserID() string { return a.botUserID }

// Connect は接続済みとして扱い、sink を保持する。以後 Sink() から受信イベントを流せる。
func (a *Adapter) Connect(_ context.Context, sink adapter.InboundSink) error {
	a.mu.Lock()

	if err := a.failLocked("Connect", Call{Method: "Connect"}); err != nil {
		a.mu.Unlock()

		return err
	}

	a.sink = sink
	a.connected = true
	a.mu.Unlock()

	sink.OnConnectionStateChanged(true, nil)

	return nil
}

// Close は切断する。
func (a *Adapter) Close(_ context.Context) error {
	a.mu.Lock()
	a.connected = false
	sink := a.sink
	a.mu.Unlock()

	if sink != nil {
		sink.OnConnectionStateChanged(false, nil)
	}

	return nil
}

// Connected は接続状態を返す。
func (a *Adapter) Connected() bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.connected
}

// Sink は Connect で受け取った受信イベントの流し先を返す。テストが受信を模擬するために使う。
func (a *Adapter) Sink() adapter.InboundSink {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.sink
}

// Workspaces は把握しているワークスペースを返す。
func (a *Adapter) Workspaces() []adapter.WorkspaceInfo {
	a.mu.Lock()
	defer a.mu.Unlock()

	return slices.Clone(a.workspaces)
}

// AddWorkspace はワークスペースを増やし、接続済みなら sink へ通知する(Discord の GuildCreate 相当)。
func (a *Adapter) AddWorkspace(ctx context.Context, ws adapter.WorkspaceInfo) {
	a.mu.Lock()
	a.workspaces = append(a.workspaces, ws)
	sink := a.sink
	a.mu.Unlock()

	if sink != nil {
		sink.OnWorkspaceDiscovered(ctx, ws)
	}
}

// SetUser は ResolveUser が返すユーザーを登録する。
func (a *Adapter) SetUser(user adapter.UserInfo) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.users[user.ID] = user
}

// FailNext は method の次の呼び出しで err を返させる。複数回呼べばその順に消費される。
func (a *Adapter) FailNext(method string, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.failures[method] = append(a.failures[method], err)
}

// Calls は呼び出し履歴を返す。
func (a *Adapter) Calls() []Call {
	a.mu.Lock()
	defer a.mu.Unlock()

	return slices.Clone(a.calls)
}

// CallsOf は method の呼び出し履歴を返す。
func (a *Adapter) CallsOf(method string) []Call {
	a.mu.Lock()
	defer a.mu.Unlock()

	var out []Call

	for _, c := range a.calls {
		if c.Method == method {
			out = append(out, c)
		}
	}

	return out
}

// Channel は ID でチャンネルを返す。
func (a *Adapter) Channel(id string) (Channel, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	ch, ok := a.channels[id]
	if !ok {
		return Channel{}, false
	}

	return *ch, true
}

// Posts は投稿履歴を返す。
func (a *Adapter) Posts() []Post {
	a.mu.Lock()
	defer a.mu.Unlock()

	out := make([]Post, 0, len(a.posts))
	for _, p := range a.posts {
		out = append(out, *p)
	}

	return out
}

// CreatePrivateChannel はプライベートチャンネルを作る。同名の生存チャンネルがあれば ErrNameTaken。
func (a *Adapter) CreatePrivateChannel(
	_ context.Context,
	ws adapter.WorkspaceRef,
	in adapter.CreateChannelInput,
) (adapter.ChannelInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.failLocked("CreatePrivateChannel", Call{
		Method:      "CreatePrivateChannel",
		WorkspaceID: ws.ExternalID,
	}); err != nil {
		return adapter.ChannelInfo{}, err
	}

	for _, ch := range a.channels {
		if ch.Name == in.Name && !ch.Archived {
			return adapter.ChannelInfo{}, adapter.ErrNameTaken
		}
	}

	a.seq++

	ch := &Channel{
		ID:          fmt.Sprintf("C%03d", a.seq),
		Name:        in.Name,
		WorkspaceID: ws.ExternalID,
		Topic:       in.Topic,
		IsPrivate:   true,
		Members:     append([]string{a.botUserID}, in.MemberIDs...),
	}
	a.channels[ch.ID] = ch

	return channelInfoOf(ch), nil
}

// AddMember はチャンネルへユーザーを追加する。既にメンバーなら ErrAlreadyMember。
func (a *Adapter) AddMember(_ context.Context, ws adapter.WorkspaceRef, channelID, userID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.failLocked("AddMember", Call{
		Method:      "AddMember",
		WorkspaceID: ws.ExternalID,
		ChannelID:   channelID,
		UserID:      userID,
	}); err != nil {
		return err
	}

	ch, err := a.channelLocked(channelID)
	if err != nil {
		return err
	}

	if slices.Contains(ch.Members, userID) {
		return adapter.ErrAlreadyMember
	}

	ch.Members = append(ch.Members, userID)

	return nil
}

// RemoveMember はチャンネルからユーザーを外す。メンバーでなければ ErrNotMember。
func (a *Adapter) RemoveMember(_ context.Context, ws adapter.WorkspaceRef, channelID, userID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.failLocked("RemoveMember", Call{
		Method:      "RemoveMember",
		WorkspaceID: ws.ExternalID,
		ChannelID:   channelID,
		UserID:      userID,
	}); err != nil {
		return err
	}

	ch, err := a.channelLocked(channelID)
	if err != nil {
		return err
	}

	idx := slices.Index(ch.Members, userID)
	if idx < 0 {
		return adapter.ErrNotMember
	}

	ch.Members = slices.Delete(ch.Members, idx, idx+1)

	return nil
}

// ArchiveChannel はチャンネルをアーカイブする。既にアーカイブ済みでも成功扱い(冪等)。
func (a *Adapter) ArchiveChannel(
	_ context.Context,
	ws adapter.WorkspaceRef,
	channelID, _ string,
) (adapter.ChannelInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.failLocked("ArchiveChannel", Call{
		Method:      "ArchiveChannel",
		WorkspaceID: ws.ExternalID,
		ChannelID:   channelID,
	}); err != nil {
		return adapter.ChannelInfo{}, err
	}

	ch, err := a.channelLocked(channelID)
	if err != nil {
		return adapter.ChannelInfo{}, err
	}

	if !ch.Archived {
		ch.Archived = true
		ch.Name = archivePrefix + strings.TrimPrefix(ch.Name, archivePrefix)
	}

	return channelInfoOf(ch), nil
}

// ListChannels はワークスペースのチャンネルを ID 順に返す。pageSize ごとにページングする。
func (a *Adapter) ListChannels(
	_ context.Context,
	ws adapter.WorkspaceRef,
	pageToken string,
	pageSize int,
) ([]adapter.ChannelInfo, string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.failLocked("ListChannels", Call{
		Method:      "ListChannels",
		WorkspaceID: ws.ExternalID,
	}); err != nil {
		return nil, "", err
	}

	ids := make([]string, 0, len(a.channels))

	for id, ch := range a.channels {
		if ch.WorkspaceID == ws.ExternalID {
			ids = append(ids, id)
		}
	}

	slices.Sort(ids)

	if pageToken != "" {
		ids = ids[min(slices.Index(ids, pageToken)+1, len(ids)):]
	}

	if pageSize <= 0 || pageSize > len(ids) {
		pageSize = len(ids)
	}

	out := make([]adapter.ChannelInfo, 0, pageSize)
	for _, id := range ids[:pageSize] {
		out = append(out, channelInfoOf(a.channels[id]))
	}

	var next string
	if pageSize < len(ids) {
		next = out[len(out)-1].ID
	}

	return out, next, nil
}

// PostMessage はチャンネルへ投稿する。
func (a *Adapter) PostMessage(
	_ context.Context,
	ws adapter.WorkspaceRef,
	channelID string,
	msg *asobellv1.Message,
) (adapter.MessageRef, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.failLocked("PostMessage", Call{
		Method:      "PostMessage",
		WorkspaceID: ws.ExternalID,
		ChannelID:   channelID,
	}); err != nil {
		return adapter.MessageRef{}, err
	}

	if _, err := a.channelLocked(channelID); err != nil {
		return adapter.MessageRef{}, err
	}

	return a.appendPostLocked(channelID, msg, Post{}), nil
}

// UpdateMessage は投稿済みメッセージを差し替える。
func (a *Adapter) UpdateMessage(
	_ context.Context,
	ws adapter.WorkspaceRef,
	ref adapter.MessageRef,
	msg *asobellv1.Message,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.failLocked("UpdateMessage", Call{
		Method:      "UpdateMessage",
		WorkspaceID: ws.ExternalID,
		ChannelID:   ref.ChannelID,
		MessageID:   ref.MessageID,
	}); err != nil {
		return err
	}

	post := a.postLocked(ref)
	if post == nil {
		return adapter.ErrNotFound
	}

	post.Message = msg

	return nil
}

// PostEphemeral は本人にだけ見える投稿を行う。ephemeral 非対応の設定なら ErrUnsupported。
func (a *Adapter) PostEphemeral(
	_ context.Context,
	ws adapter.WorkspaceRef,
	channelID, userID string,
	msg *asobellv1.Message,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.failLocked("PostEphemeral", Call{
		Method:      "PostEphemeral",
		WorkspaceID: ws.ExternalID,
		ChannelID:   channelID,
		UserID:      userID,
	}); err != nil {
		return err
	}

	if !a.capabilities.GetEphemeral() {
		return adapter.ErrUnsupported
	}

	if _, err := a.channelLocked(channelID); err != nil {
		return err
	}

	a.appendPostLocked(channelID, msg, Post{Ephemeral: true, UserID: userID})

	return nil
}

// SendDirectMessage は DM を送る。
func (a *Adapter) SendDirectMessage(
	_ context.Context,
	ws adapter.WorkspaceRef,
	userID string,
	msg *asobellv1.Message,
) (adapter.MessageRef, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.failLocked("SendDirectMessage", Call{
		Method:      "SendDirectMessage",
		WorkspaceID: ws.ExternalID,
		UserID:      userID,
	}); err != nil {
		return adapter.MessageRef{}, err
	}

	if !a.capabilities.GetDirectMessage() {
		return adapter.MessageRef{}, adapter.ErrUnsupported
	}

	return a.appendPostLocked("D-"+userID, msg, Post{DirectTo: userID}), nil
}

// PinMessage はメッセージをピン留めする。
func (a *Adapter) PinMessage(_ context.Context, ws adapter.WorkspaceRef, ref adapter.MessageRef) error {
	return a.setPinned(ws, ref, true, "PinMessage")
}

// UnpinMessage はピン留めを外す。
func (a *Adapter) UnpinMessage(_ context.Context, ws adapter.WorkspaceRef, ref adapter.MessageRef) error {
	return a.setPinned(ws, ref, false, "UnpinMessage")
}

func (a *Adapter) setPinned(ws adapter.WorkspaceRef, ref adapter.MessageRef, pinned bool, method string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.failLocked(method, Call{
		Method:      method,
		WorkspaceID: ws.ExternalID,
		ChannelID:   ref.ChannelID,
		MessageID:   ref.MessageID,
	}); err != nil {
		return err
	}

	post := a.postLocked(ref)
	if post == nil {
		return adapter.ErrNotFound
	}

	post.Pinned = pinned

	return nil
}

// ResolveUser はユーザーを解決する。未登録の ID は ID をそのまま表示名にした結果を返す。
func (a *Adapter) ResolveUser(
	_ context.Context,
	ws adapter.WorkspaceRef,
	userID string,
) (adapter.UserInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.failLocked("ResolveUser", Call{
		Method:      "ResolveUser",
		WorkspaceID: ws.ExternalID,
		UserID:      userID,
	}); err != nil {
		return adapter.UserInfo{}, err
	}

	if user, ok := a.users[userID]; ok {
		return user, nil
	}

	return adapter.UserInfo{ID: userID, DisplayName: userID}, nil
}

func (a *Adapter) appendPostLocked(channelID string, msg *asobellv1.Message, base Post) adapter.MessageRef {
	a.seq++

	ref := &asobellv1.MessageRef{ChannelId: channelID, MessageId: fmt.Sprintf("M%03d", a.seq)}
	post := base
	post.Ref = ref
	post.Message = msg
	a.posts = append(a.posts, &post)

	return adapter.MessageRef{ChannelID: ref.GetChannelId(), MessageID: ref.GetMessageId()}
}

func (a *Adapter) failLocked(method string, call Call) error {
	a.calls = append(a.calls, call)

	queued := a.failures[method]
	if len(queued) == 0 {
		return nil
	}

	a.failures[method] = queued[1:]

	return queued[0]
}

func (a *Adapter) channelLocked(id string) (*Channel, error) {
	ch, ok := a.channels[id]
	if !ok {
		return nil, adapter.ErrNotFound
	}

	return ch, nil
}

func (a *Adapter) postLocked(ref adapter.MessageRef) *Post {
	for _, p := range a.posts {
		if p.Ref.GetChannelId() == ref.ChannelID && p.Ref.GetMessageId() == ref.MessageID {
			return p
		}
	}

	return nil
}

func channelInfoOf(ch *Channel) adapter.ChannelInfo {
	return adapter.ChannelInfo{ID: ch.ID, Name: ch.Name, IsPrivate: ch.IsPrivate, Archived: ch.Archived}
}
