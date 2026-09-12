// Package discord は Discord 向けの adapter.Adapter 実装(docs/06-provider.md §7)。
// Gateway 接続とスラッシュコマンド登録、REST 操作、エラー変換をここに閉じ込める。
package discord

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/bwmarrin/discordgo"

	asobellv1 "github.com/bear-san/aso-bell/gen/asobell/v1"
	"github.com/bear-san/aso-bell/internal/provider/adapter"
)

// archivePrefix はアーカイブしたチャンネル名の接頭辞。Discord にはアーカイブ機能が無いため名前で表す。
const archivePrefix = "archived_"

// channelNameLimit は Discord のチャンネル名の上限。
const channelNameLimit = 100

// Config は Discord Adapter の設定。
type Config struct {
	Token         string
	ApplicationID string
	// CommandName は登録するスラッシュコマンド名(既定 "asobell")。
	CommandName string
	Logger      *slog.Logger
	// HTTPClient は REST 呼び出しに使う。テストがスタブサーバーへ向けるために差し替える。
	HTTPClient *http.Client
}

// Adapter は Discord 上の操作を担う。
type Adapter struct {
	session *discordgo.Session
	cfg     Config

	mu        sync.RWMutex
	ctx       context.Context //nolint:containedctx // Gateway のハンドラは引数を持たないため接続時の ctx を保持する
	sink      adapter.InboundSink
	guilds    map[string]string
	connected bool
}

// New は Discord Adapter を作る。Gateway への接続は Connect で行う。
func New(cfg Config) (*Adapter, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	if cfg.CommandName == "" {
		cfg.CommandName = "asobell"
	}

	session, err := discordgo.New("Bot " + cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("new discord session: %w", err)
	}

	// interaction は intent に依存しないが、GuildCreate でワークスペースを把握するため Guilds は必要。
	session.Identify.Intents = discordgo.IntentsGuilds

	if cfg.HTTPClient != nil {
		session.Client = cfg.HTTPClient
	}

	return &Adapter{session: session, cfg: cfg, guilds: map[string]string{}}, nil
}

// Kind は Provider 種別を返す。
func (a *Adapter) Kind() asobellv1.ProviderKind { return asobellv1.ProviderKind_PROVIDER_KIND_DISCORD }

// Capabilities は能力を申告する。Discord のモーダルには日付入力が無いため forms は false(docs/06 §5.1)。
func (a *Adapter) Capabilities() *asobellv1.Capabilities {
	return &asobellv1.Capabilities{Forms: false, Ephemeral: true, DirectMessage: true}
}

// BotUserID は Bot 自身のユーザー ID を返す。
func (a *Adapter) BotUserID() string {
	if user := a.session.State.User; user != nil {
		return user.ID
	}

	return ""
}

// Connected は Gateway への接続状態を返す。
func (a *Adapter) Connected() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.connected
}

// Workspaces は参加している Guild を返す。
func (a *Adapter) Workspaces() []adapter.WorkspaceInfo {
	a.mu.RLock()
	defer a.mu.RUnlock()

	out := make([]adapter.WorkspaceInfo, 0, len(a.guilds))
	for id, name := range a.guilds {
		out = append(out, adapter.WorkspaceInfo{ExternalID: id, Name: name})
	}

	slices.SortFunc(out, func(x, y adapter.WorkspaceInfo) int { return strings.Compare(x.ExternalID, y.ExternalID) })

	return out
}

// Connect は Gateway へ接続し、受信イベントを sink へ流す。
func (a *Adapter) Connect(ctx context.Context, sink adapter.InboundSink) error {
	a.mu.Lock()
	a.ctx = context.WithoutCancel(ctx)
	a.sink = sink
	a.mu.Unlock()

	a.session.AddHandler(a.onReady)
	a.session.AddHandler(a.onGuildCreate)
	a.session.AddHandler(a.onDisconnect)
	a.session.AddHandler(a.onResumed)
	a.session.AddHandler(a.onInteraction)

	if err := a.session.Open(); err != nil {
		return fmt.Errorf("open discord gateway: %w", err)
	}

	return nil
}

// Close は Gateway との接続を閉じる。
func (a *Adapter) Close(context.Context) error {
	a.setConnected(false, nil)

	if err := a.session.Close(); err != nil {
		return fmt.Errorf("close discord gateway: %w", err)
	}

	return nil
}

// inbound はハンドラが使う ctx と sink を返す。接続前なら sink は nil。
func (a *Adapter) inbound() (context.Context, adapter.InboundSink) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.sink == nil {
		return context.Background(), nil
	}

	return a.ctx, a.sink
}

func (a *Adapter) setConnected(connected bool, cause error) {
	a.mu.Lock()
	changed := a.connected != connected
	a.connected = connected
	sink := a.sink
	a.mu.Unlock()

	if changed && sink != nil {
		sink.OnConnectionStateChanged(connected, cause)
	}
}

func (a *Adapter) onReady(_ *discordgo.Session, ready *discordgo.Ready) {
	a.mu.Lock()
	for _, guild := range ready.Guilds {
		if _, ok := a.guilds[guild.ID]; !ok {
			a.guilds[guild.ID] = guild.Name
		}
	}
	a.mu.Unlock()

	a.setConnected(true, nil)
}

func (a *Adapter) onResumed(*discordgo.Session, *discordgo.Resumed) {
	a.setConnected(true, nil)
}

func (a *Adapter) onDisconnect(*discordgo.Session, *discordgo.Disconnect) {
	a.setConnected(false, adapter.ErrUnavailable)
}

// onGuildCreate は Guild が判明したときにコマンドを登録し、Manager へ通知する。
// 参加直後と再接続のたびに届くため、登録は一括上書きで冪等にする。
func (a *Adapter) onGuildCreate(_ *discordgo.Session, guild *discordgo.GuildCreate) {
	a.mu.Lock()
	_, known := a.guilds[guild.ID]
	a.guilds[guild.ID] = guild.Name
	a.mu.Unlock()

	ctx, sink := a.inbound()

	if err := a.registerCommands(ctx, guild.ID); err != nil {
		a.cfg.Logger.ErrorContext(ctx, "register commands failed", "guild_id", guild.ID, "error", err)
	}

	if known || sink == nil {
		return
	}

	sink.OnWorkspaceDiscovered(ctx, adapter.WorkspaceInfo{ExternalID: guild.ID, Name: guild.Name})
}

// registerCommands は Guild にスラッシュコマンドを登録する。
func (a *Adapter) registerCommands(ctx context.Context, guildID string) error {
	_, err := a.session.ApplicationCommandBulkOverwrite(
		a.applicationID(), guildID,
		[]*discordgo.ApplicationCommand{CommandDefinition(a.cfg.CommandName)},
		discordgo.WithContext(ctx),
	)

	return convert(err)
}

func (a *Adapter) applicationID() string {
	if a.cfg.ApplicationID != "" {
		return a.cfg.ApplicationID
	}

	return a.BotUserID()
}

// CreatePrivateChannel は @everyone から見えないテキストチャンネルを作る。
func (a *Adapter) CreatePrivateChannel(
	ctx context.Context,
	ws adapter.WorkspaceRef,
	in adapter.CreateChannelInput,
) (adapter.ChannelInfo, error) {
	overwrites := []*discordgo.PermissionOverwrite{
		// @everyone ロール ID は Guild ID と同じ。
		{ID: ws.ExternalID, Type: discordgo.PermissionOverwriteTypeRole, Deny: discordgo.PermissionViewChannel},
		{ID: a.BotUserID(), Type: discordgo.PermissionOverwriteTypeMember, Allow: botPermissions},
	}

	for _, member := range in.MemberIDs {
		overwrites = append(overwrites, &discordgo.PermissionOverwrite{
			ID:    member,
			Type:  discordgo.PermissionOverwriteTypeMember,
			Allow: memberPermissions,
		})
	}

	channel, err := a.session.GuildChannelCreateComplex(ws.ExternalID, discordgo.GuildChannelCreateData{
		Name:                 channelName(in.Name),
		Type:                 discordgo.ChannelTypeGuildText,
		Topic:                in.Topic,
		ParentID:             in.CategoryID,
		PermissionOverwrites: overwrites,
	}, discordgo.WithContext(ctx))
	if err != nil {
		return adapter.ChannelInfo{}, convert(err)
	}

	return channelInfo(channel), nil
}

// AddMember はチャンネルの overwrite を追加して閲覧できるようにする。
func (a *Adapter) AddMember(ctx context.Context, _ adapter.WorkspaceRef, channelID, userID string) error {
	member, err := a.hasOverwrite(ctx, channelID, userID)
	if err != nil {
		return err
	}

	if member {
		return adapter.ErrAlreadyMember
	}

	err = a.session.ChannelPermissionSet(
		channelID, userID, discordgo.PermissionOverwriteTypeMember, memberPermissions, 0,
		discordgo.WithContext(ctx),
	)

	return convert(err)
}

// RemoveMember は overwrite を消して閲覧できないようにする。
func (a *Adapter) RemoveMember(ctx context.Context, _ adapter.WorkspaceRef, channelID, userID string) error {
	member, err := a.hasOverwrite(ctx, channelID, userID)
	if err != nil {
		return err
	}

	if !member {
		return adapter.ErrNotMember
	}

	return convert(a.session.ChannelPermissionDelete(channelID, userID, discordgo.WithContext(ctx)))
}

// hasOverwrite はユーザー個別の overwrite があるかを返す。
// Discord の overwrite 設定は冪等で「既にメンバー」を区別できないため、事前に現在の状態を読む。
func (a *Adapter) hasOverwrite(ctx context.Context, channelID, userID string) (bool, error) {
	channel, err := a.session.Channel(channelID, discordgo.WithContext(ctx))
	if err != nil {
		return false, convert(err)
	}

	for _, overwrite := range channel.PermissionOverwrites {
		if overwrite.Type == discordgo.PermissionOverwriteTypeMember && overwrite.ID == userID {
			return true, nil
		}
	}

	return false, nil
}

// ArchiveChannel は名前に接頭辞を付け、指定があればアーカイブ用カテゴリへ移す。
func (a *Adapter) ArchiveChannel(
	ctx context.Context,
	_ adapter.WorkspaceRef,
	channelID, archiveCategoryID string,
) (adapter.ChannelInfo, error) {
	channel, err := a.session.Channel(channelID, discordgo.WithContext(ctx))
	if err != nil {
		return adapter.ChannelInfo{}, convert(err)
	}

	if strings.HasPrefix(channel.Name, archivePrefix) {
		return channelInfo(channel), nil
	}

	edit := &discordgo.ChannelEdit{Name: channelName(archivePrefix + channel.Name)}
	if archiveCategoryID != "" {
		edit.ParentID = archiveCategoryID
	}

	updated, err := a.session.ChannelEdit(channelID, edit, discordgo.WithContext(ctx))
	if err != nil {
		return adapter.ChannelInfo{}, convert(err)
	}

	return channelInfo(updated), nil
}

// ListChannels は Guild のテキストチャンネルを ID 順に返す。
// Discord の一覧 API はページングを持たないため、取得後に切り出す。
func (a *Adapter) ListChannels(
	ctx context.Context,
	ws adapter.WorkspaceRef,
	pageToken string,
	pageSize int,
) ([]adapter.ChannelInfo, string, error) {
	channels, err := a.session.GuildChannels(ws.ExternalID, discordgo.WithContext(ctx))
	if err != nil {
		return nil, "", convert(err)
	}

	infos := make([]adapter.ChannelInfo, 0, len(channels))

	for _, channel := range channels {
		if channel.Type == discordgo.ChannelTypeGuildText {
			infos = append(infos, channelInfo(channel))
		}
	}

	slices.SortFunc(infos, func(x, y adapter.ChannelInfo) int { return strings.Compare(x.ID, y.ID) })

	return page(infos, pageToken, pageSize)
}

// PostMessage はチャンネルへ投稿する。
func (a *Adapter) PostMessage(
	ctx context.Context,
	_ adapter.WorkspaceRef,
	channelID string,
	msg *asobellv1.Message,
) (adapter.MessageRef, error) {
	sent, err := a.session.ChannelMessageSendComplex(channelID, messageSend(msg), discordgo.WithContext(ctx))
	if err != nil {
		return adapter.MessageRef{}, convert(err)
	}

	return adapter.MessageRef{ChannelID: sent.ChannelID, MessageID: sent.ID}, nil
}

// UpdateMessage は投稿済みメッセージを差し替える。
func (a *Adapter) UpdateMessage(
	ctx context.Context,
	_ adapter.WorkspaceRef,
	ref adapter.MessageRef,
	msg *asobellv1.Message,
) error {
	_, err := a.session.ChannelMessageEditComplex(
		messageEdit(ref.ChannelID, ref.MessageID, msg), discordgo.WithContext(ctx),
	)

	return convert(err)
}

// PostEphemeral は常に失敗する。Discord の ephemeral はインタラクション応答としてしか存在せず、
// Manager からの後追い通知はインタラクション文脈の外にあるため(docs/06 §7)。Runtime が DM へ切り替える。
func (a *Adapter) PostEphemeral(
	context.Context,
	adapter.WorkspaceRef,
	string, string,
	*asobellv1.Message,
) error {
	return adapter.ErrUnsupported
}

// SendDirectMessage は DM チャンネルを開いて投稿する。
func (a *Adapter) SendDirectMessage(
	ctx context.Context,
	_ adapter.WorkspaceRef,
	userID string,
	msg *asobellv1.Message,
) (adapter.MessageRef, error) {
	channel, err := a.session.UserChannelCreate(userID, discordgo.WithContext(ctx))
	if err != nil {
		return adapter.MessageRef{}, convert(err)
	}

	send := messageSend(msg)
	// DM のリンクプレビューは連携 URL の先読みにつながるため常に抑止する(ADR 0009)。
	send.Flags |= discordgo.MessageFlagsSuppressEmbeds

	sent, err := a.session.ChannelMessageSendComplex(channel.ID, send, discordgo.WithContext(ctx))
	if err != nil {
		return adapter.MessageRef{}, convert(err)
	}

	return adapter.MessageRef{ChannelID: sent.ChannelID, MessageID: sent.ID}, nil
}

// PinMessage はメッセージをピン留めする。
func (a *Adapter) PinMessage(ctx context.Context, _ adapter.WorkspaceRef, ref adapter.MessageRef) error {
	return convert(a.session.ChannelMessagePin(ref.ChannelID, ref.MessageID, discordgo.WithContext(ctx)))
}

// UnpinMessage はピン留めを外す。
func (a *Adapter) UnpinMessage(ctx context.Context, _ adapter.WorkspaceRef, ref adapter.MessageRef) error {
	return convert(a.session.ChannelMessageUnpin(ref.ChannelID, ref.MessageID, discordgo.WithContext(ctx)))
}

// ResolveUser は Guild 内の表示名(ニックネーム優先)を返す。
func (a *Adapter) ResolveUser(
	ctx context.Context,
	ws adapter.WorkspaceRef,
	userID string,
) (adapter.UserInfo, error) {
	member, err := a.session.GuildMember(ws.ExternalID, userID, discordgo.WithContext(ctx))
	if err != nil {
		return adapter.UserInfo{}, convert(err)
	}

	info := adapter.UserInfo{ID: userID, DisplayName: member.DisplayName()}
	if member.User != nil {
		info.IsBot = member.User.Bot
	}

	return info, nil
}

// channelName は Discord の制約(1〜100 文字、小文字とダッシュ)に合わせる。
// 小文字化は Discord 側でも行われるが、返ってくる名前と記録がずれないよう先に揃えておく。
func channelName(name string) string {
	name = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), " ", "-"))

	runes := []rune(name)
	if len(runes) > channelNameLimit {
		name = string(runes[:channelNameLimit])
	}

	if name == "" {
		return "event"
	}

	return name
}

func channelInfo(channel *discordgo.Channel) adapter.ChannelInfo {
	return adapter.ChannelInfo{
		ID:        channel.ID,
		Name:      channel.Name,
		IsPrivate: true,
		Archived:  strings.HasPrefix(channel.Name, archivePrefix),
	}
}

// page は取得済みの一覧を pageToken 以降から pageSize 件だけ切り出す。
func page(infos []adapter.ChannelInfo, pageToken string, pageSize int) ([]adapter.ChannelInfo, string, error) {
	if pageToken != "" {
		idx := slices.IndexFunc(infos, func(c adapter.ChannelInfo) bool { return c.ID == pageToken })
		infos = infos[min(idx+1, len(infos)):]
	}

	if pageSize <= 0 || pageSize > len(infos) {
		return infos, "", nil
	}

	out := infos[:pageSize]

	return out, out[len(out)-1].ID, nil
}
