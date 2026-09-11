<!-- 調査エージェントによる裏取りレポート(自動抽出)。設計本文は docs/03-tech-stack.md を参照 -->

## Go Discord ライブラリ比較レポート（discordgo vs disgo）

調査日: 2026-09-11。GitHub API / Discord 公式ドキュメント（docs.discord.com の `.md` 版を curl で取得し grep）/ Context7（`/bwmarrin/discordgo`）/ discordgo ソース（`v0.29.0` タグおよび `master` の raw ファイル）を直接確認した内容のみ記載しています。

---

### 1. ライブラリ比較

| 項目 | github.com/bwmarrin/discordgo | github.com/disgoorg/disgo |
|---|---|---|
| Stars | **5,985** | **607** |
| SPDX License | **BSD-3-Clause** | **Apache-2.0** |
| 最新リリース | **v0.29.0**（2025-05-24 公開） | **v0.19.6**（2026-06-07 公開） |
| 最終 push | 2026-02-14（`f43dd94` "fix(SelectMenu, TextInput): Add Required field…"） | 2026-09-08（`382dfa7` "deprecate(connection): drop ConnectionTypeBattleNet"） |
| 直近コミット | 2025-12-29 に 4 件（FileUploadComponent 実装、role member count endpoint 等）、2026-02-14 に 1 件 | 2026-08-30〜09-08 に 5 件（ほぼ毎週） |
| Open issues | 232 | 19 |
| go.mod の Go 版 | `go 1.13`（依存: gorilla/websocket v1.4.2） | `go 1.24.0` |
| 自己申告の安定性 | README: "This library and the Discord API are unfinished… there may be major changes"。`go get` はタグ版を取るので最新機能は `@master` 指定が慣習 | README: "public API of DisGo is mostly stable… Smaller breaking changes can happen before the v1" |
| archived | false | false |

**所見**: discordgo はスター数・利用実績で圧倒的だが、タグ付きリリースは 2025-05 で止まっており、master の更新頻度も 2〜3 ヶ月に一度程度。disgo は保守が活発（週次コミット、issue 少）だが採用実績は 1/10。要件（スラッシュコマンド、ボタン、チャンネル/権限操作、ピン留め）はどちらも網羅。**本レポートは指示どおり最多スターの discordgo を選定して API を検証**しますが、後述のとおり **v0.29.0 のピン留めエンドポイントは Discord 側で deprecated 済み（master では修正済み）** なので、`go get github.com/bwmarrin/discordgo@master` の利用を推奨します。

Sources: `https://api.github.com/repos/bwmarrin/discordgo`, `/releases/latest`, `/commits`, `/tags`; 同じく `disgoorg/disgo`。README: https://github.com/bwmarrin/discordgo, https://github.com/disgoorg/disgo

---

### 2. discordgo API 検証（Context7 ID: `/bwmarrin/discordgo` + `v0.29.0` ソース）

#### 2-1. Guild スコープのスラッシュコマンド登録

シグネチャ（restapi.go）:
```go
func (s *Session) ApplicationCommandCreate(appID string, guildID string, cmd *ApplicationCommand, options ...RequestOption) (ccmd *ApplicationCommand, err error)
func (s *Session) ApplicationCommandBulkOverwrite(appID, guildID string, commands []*ApplicationCommand, options ...RequestOption) (ccmds []*ApplicationCommand, err error)
```
`guildID` に `""` を渡すとグローバル登録。`appID` は `s.State.User.ID`。

`ApplicationCommandOption`（interactions.go）主要フィールド: `Type ApplicationCommandOptionType`, `Name`, `Description`, `Required bool`, `Choices []*ApplicationCommandOptionChoice`, `MinValue *float64`, `MaxValue float64`, `MinLength *int`, `MaxLength int`, `Autocomplete bool`, `ChannelTypes []ChannelType`。
型定数: `ApplicationCommandOptionString = 3`, `ApplicationCommandOptionInteger = 4`, `ApplicationCommandOptionBoolean = 5`, `ApplicationCommandOptionUser = 6`, `ApplicationCommandOptionChannel = 7`, `ApplicationCommandOptionRole = 8`, `ApplicationCommandOptionNumber = 10`, `ApplicationCommandOptionAttachment = 11`。

**日付型のオプションは Discord に存在しない**ため、`String`（`MinLength`/`MaxLength` で `YYYY-MM-DD` 長に制限しサーバ側でパース）か `Integer`（UNIX 秒）で受ける。

```go
minLen, maxLen := 10, 10
minV := float64(1)
cmds := []*discordgo.ApplicationCommand{{
    Name: "reserve", Description: "予約を作成",
    Type: discordgo.ChatApplicationCommand,
    Options: []*discordgo.ApplicationCommandOption{
        {Type: discordgo.ApplicationCommandOptionString,  Name: "title", Description: "件名", Required: true, MaxLength: 100},
        {Type: discordgo.ApplicationCommandOptionInteger, Name: "count", Description: "人数", Required: true, MinValue: &minV, MaxValue: 99},
        {Type: discordgo.ApplicationCommandOptionString,  Name: "date",  Description: "YYYY-MM-DD", Required: true, MinLength: &minLen, MaxLength: maxLen},
    },
}}
_, err := s.ApplicationCommandBulkOverwrite(s.State.User.ID, guildID, cmds) // 一括上書き
```

#### 2-2. InteractionCreate の処理と応答

`Interaction` 構造体（interactions.go）: `ID`, `AppID`, `Type InteractionType`, `Data InteractionData`, `GuildID`, `ChannelID`, `Message *Message`, `AppPermissions int64`, `Member *Member`（**guild 内でのみ非 nil**）, `User *User`（**DM 時のみ非 nil**）, `Token`, `Locale`。
`InteractionType`: `InteractionPing=1`, `InteractionApplicationCommand=2`, `InteractionMessageComponent=3`, `InteractionApplicationCommandAutocomplete=4`, `InteractionModalSubmit=5`。
`MessageComponentInteractionData`: `CustomID string`, `ComponentType`, `Resolved`, `Values []string`。
`InteractionResponseType`: `InteractionResponseChannelMessageWithSource=4`, `InteractionResponseDeferredChannelMessageWithSource=5`, `InteractionResponseDeferredMessageUpdate=6`, `InteractionResponseUpdateMessage=7`, `InteractionResponseModal=9`。
Ephemeral: `MessageFlagsEphemeral MessageFlags = 1 << 6`（message.go）。

```go
func (s *Session) InteractionRespond(interaction *Interaction, resp *InteractionResponse, options ...RequestOption) error
func (s *Session) InteractionResponseEdit(interaction *Interaction, newresp *WebhookEdit, options ...RequestOption) (*Message, error)
func (s *Session) FollowupMessageCreate(interaction *Interaction, wait bool, data *WebhookParams, options ...RequestOption) (*Message, error)
```
（注: Context7 の一部スニペットに `InteractionResponseEdit(appID, ...)` という旧形式が混在しているが、v0.29.0 ソースは上記のとおり `appID` 引数なし。）

```go
s.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
    switch i.Type {
    case discordgo.InteractionApplicationCommand:
        data := i.ApplicationCommandData()
        opts := map[string]*discordgo.ApplicationCommandInteractionDataOption{}
        for _, o := range data.Options { opts[o.Name] = o }
        title := opts["title"].StringValue()
        count := opts["count"].IntValue()      // int64（型不一致だと panic）
        // 3 秒以内に ACK → 後で編集
        _ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
            Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
            Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
        })
        msg := fmt.Sprintf("%s (%d)", title, count)
        _, _ = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &msg})
    case discordgo.InteractionMessageComponent:
        customID := i.MessageComponentData().CustomID   // custom_id の読み出し
        userID := i.Member.User.ID                       // guild 内なので Member
        _ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
            Type: discordgo.InteractionResponseChannelMessageWithSource,
            Data: &discordgo.InteractionResponseData{Content: "受付: " + customID + " by " + userID, Flags: discordgo.MessageFlagsEphemeral},
        })
    }
})
```
Deferred 応答時に指定できる flag は `EPHEMERAL` のみ（Discord docs）。

#### 2-3. プライベートテキストチャンネル作成

```go
func (s *Session) GuildChannelCreateComplex(guildID string, data GuildChannelCreateData, options ...RequestOption) (st *Channel, err error)

type GuildChannelCreateData struct {
    Name                 string                 `json:"name"`
    Type                 ChannelType            `json:"type"`
    Topic                string                 `json:"topic,omitempty"`
    Bitrate, UserLimit, RateLimitPerUser, Position int
    PermissionOverwrites []*PermissionOverwrite `json:"permission_overwrites,omitempty"`
    ParentID             string                 `json:"parent_id,omitempty"`
    NSFW                 bool                   `json:"nsfw,omitempty"`
}
type PermissionOverwrite struct { ID string; Type PermissionOverwriteType; Deny int64; Allow int64 }
const ( PermissionOverwriteTypeRole PermissionOverwriteType = 0; PermissionOverwriteTypeMember = 1 )
```
権限定数（structs.go v0.29.0）: `PermissionViewChannel = 1 << 10`, `PermissionSendMessages = 1 << 11`, `PermissionReadMessageHistory = 1 << 16`, `PermissionManageMessages = 1 << 13`, `PermissionManageChannels = 1 << 4`, `PermissionManageRoles = 1 << 28`, `PermissionAdministrator = 1 << 3`。`PermissionReadMessages` は deprecated。**`PermissionPinMessages`（1<<51）は v0.29.0 にも master にも未定義**（後述）。

```go
everyoneID := guildID // @everyone ロール ID = guild ID
ch, err := s.GuildChannelCreateComplex(guildID, discordgo.GuildChannelCreateData{
    Name: "reserve-0911-tanaka", Type: discordgo.ChannelTypeGuildText, ParentID: categoryID,
    PermissionOverwrites: []*discordgo.PermissionOverwrite{
        {ID: everyoneID, Type: discordgo.PermissionOverwriteTypeRole,   Deny: discordgo.PermissionViewChannel},
        {ID: s.State.User.ID, Type: discordgo.PermissionOverwriteTypeMember,
         Allow: discordgo.PermissionViewChannel | discordgo.PermissionSendMessages | discordgo.PermissionReadMessageHistory | discordgo.PermissionManageMessages},
        {ID: userID, Type: discordgo.PermissionOverwriteTypeMember,
         Allow: discordgo.PermissionViewChannel | discordgo.PermissionSendMessages | discordgo.PermissionReadMessageHistory},
    },
})
```
Discord 側要件（guild.md "Create Guild Channel"）: `MANAGE_CHANNELS` 必須。overwrite を設定する場合「only permissions your bot has in the guild can be allowed/denied」、`MANAGE_ROLES` を overwrite に含めるのは管理者のみ。

#### 2-4. 後からのアクセス追加/削除

```go
func (s *Session) ChannelPermissionSet(channelID, targetID string, targetType PermissionOverwriteType, allow, deny int64, options ...RequestOption) (err error)
func (s *Session) ChannelPermissionDelete(channelID, targetID string, options ...RequestOption) (err error)
```
```go
err = s.ChannelPermissionSet(ch.ID, userID, discordgo.PermissionOverwriteTypeMember,
        discordgo.PermissionViewChannel|discordgo.PermissionSendMessages|discordgo.PermissionReadMessageHistory, 0)
err = s.ChannelPermissionDelete(ch.ID, userID)
```
Discord 側: `PUT/DELETE /channels/{id}/permissions/{overwrite.id}` は **`MANAGE_ROLES` 必須**（channel.md L544, L601）。

#### 2-5. チャンネル名変更

```go
func (s *Session) ChannelEdit(channelID string, data *ChannelEdit, options ...RequestOption) (st *Channel, err error)
func (s *Session) ChannelEditComplex(...)  // NOTE: deprecated, use ChannelEdit instead
```
`ChannelEdit` 構造体: `Name string`, `Topic string`, `NSFW *bool`, `Position *int`, `PermissionOverwrites []*PermissionOverwrite`, `ParentID string`, `RateLimitPerUser *int`, `Archived *bool`, … 。
```go
_, err = s.ChannelEdit(ch.ID, &discordgo.ChannelEdit{Name: "reserve-0912-tanaka"})
```
制約: 名前は **1〜100 文字**（channel.md "1-100 character channel name"）。Modify Channel は `MANAGE_CHANNELS` 必須、overwrite 同時変更時は `MANAGE_ROLES`。**小文字/ダッシュ化は公式ドキュメントに明記がない**（discord-api-docs Discussion #5338 でも「未文書化」と指摘）が、実運用ではテキストチャンネル名は小文字化・空白→`-` 変換される（サポート記事・同 Discussion で確認）。アプリ側で事前に `strings.ToLower` + 空白置換しておくと安全。

#### 2-6. ボタン付きメッセージ送信と編集

components.go: `type ButtonStyle uint`; `PrimaryButton=1`, `SecondaryButton=2`, `SuccessButton=3`, `DangerButton=4`, `LinkButton=5`, `PremiumButton=6`。
```go
type Button struct { Label string; Style ButtonStyle; Disabled bool; Emoji *ComponentEmoji; URL string; CustomID string `json:"custom_id,omitempty"`; SKUID string; ID int }
type ActionsRow struct { Components []MessageComponent; ID int }
func (s *Session) ChannelMessageSendComplex(channelID string, data *MessageSend, options ...RequestOption) (st *Message, err error)
func (s *Session) ChannelMessageEditComplex(m *MessageEdit, options ...RequestOption) (st *Message, err error)
```
`MessageEdit`: `Content *string`, `Components *[]MessageComponent`, `Embeds *[]*MessageEmbed`, `Flags MessageFlags`, `ID`, `Channel`。`NewMessageEdit(channelID, messageID)` + `.SetContent()/.SetEmbeds()` チェーン可。
```go
msg, err := s.ChannelMessageSendComplex(ch.ID, &discordgo.MessageSend{
    Content: "承認しますか？",
    Components: []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
        discordgo.Button{Label: "承認", Style: discordgo.SuccessButton, CustomID: "approve:" + reqID},
        discordgo.Button{Label: "却下", Style: discordgo.DangerButton,  CustomID: "reject:" + reqID},
    }}},
})
done := "承認済み"
_, err = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
    ID: msg.ID, Channel: ch.ID, Content: &done,
    Components: &[]discordgo.MessageComponent{}, // ボタン除去
})
```
Discord 制約（components.md）: `custom_id` **1〜100 文字**・同一メッセージ内で一意、`label` 最大 80 文字、Action Row あたりボタン最大 5、レガシーメッセージで Action Row 最大 5。discordgo は長さ検証しないので自前で確認。

#### 2-7. ピン留め

```go
func (s *Session) ChannelMessagePin(channelID, messageID string, options ...RequestOption) (err error)
func (s *Session) ChannelMessageUnpin(channelID, messageID string, options ...RequestOption) (err error)
```
**重要な差異**:
- v0.29.0 の `EndpointChannelMessagePin` は `/channels/{id}/pins/{mid}`。Discord 公式（message.md）では **"Pin Message (deprecated)"** 扱いで、現行は `PUT /channels/{id}/messages/pins/{mid}`。**master では新エンドポイントに更新済み**（endpoints.go L147-148）。
- 必要権限は現在 **`PIN_MESSAGES`（`1 << 51`）**（permissions.md L99、message.md L1353）。以前は `MANAGE_MESSAGES` だったが 2025-08 に分離。discordgo に定数がないため `const PermissionPinMessages int64 = 1 << 51` を自前定義して overwrite/招待 permissions に含める。
- 上限: 公式 docs の message.md には数値記載がなく、Discord 公式ブログ（2025-09-25 changelog）で "pin limit from 50 to 250" と告知。**旧来の 50 ではなく現在は 250/チャンネル**。ただし deprecated の `GET /channels/{id}/pins` は最初の 50 件のみ返す。

#### 2-8. Gateway Intents と招待時の scope/permissions

- `INTERACTION_CREATE` は gateway.md の intent 一覧に載っておらず「Any events not listed… will always be sent to your app」→ **interaction 受信に intent 不要**。
- チャンネル作成/更新イベント（`CHANNEL_CREATE/UPDATE/DELETE/PINS_UPDATE`）を受けるなら `GUILDS (1 << 0)`。
- `GUILD_MEMBERS (1 << 1)`, `GUILD_PRESENCES (1 << 8)`, `MESSAGE_CONTENT (1 << 15)` は特権 intent。`List Guild Members` エンドポイントは `GUILD_MEMBERS` 必須（guild.md L1055）だが `Get Guild Member`（単体）は不要。
- discordgo 定数: `IntentsGuilds = 1 << 0`, `IntentsGuildMembers = 1 << 1`, `IntentsGuildMessages = 1 << 9`, `IntentsMessageContent = 1 << 15`, `IntentsNone = 0`, `IntentsAllWithoutPrivileged`（`New()` の既定値）。
```go
dg.Identify.Intents = discordgo.IntentsGuilds // interaction のみなら IntentsNone でも可
```
- OAuth2（oauth2 docs）: `bot` scope は "puts the bot in the user's selected guild"、`applications.commands` は "allows your app to add commands to a guild - included by default with the bot scope"。招待 URL: `https://discord.com/oauth2/authorize?client_id=<ID>&scope=bot+applications.commands&permissions=<int>`。
- 必要 permissions（Discord 公式ビット）: `VIEW_CHANNEL 1<<10 (1024)`, `SEND_MESSAGES 1<<11 (2048)`, `READ_MESSAGE_HISTORY 1<<16 (65536)`, `MANAGE_CHANNELS 1<<4 (16)`（チャンネル作成/改名）, `MANAGE_ROLES 1<<28 (268435456)`（overwrite 編集）, `PIN_MESSAGES 1<<51`（ピン留め）。`MANAGE_MESSAGES 1<<13 (8192)` は他人のメッセージ削除用で、ピン留めには不要になった。合計例: `1024+2048+65536+16+268435456+2251799813685248`。

#### 2-9. メンバー/表示名

```go
func (s *Session) GuildMember(guildID, userID string, options ...RequestOption) (st *Member, err error)
func (s *Session) GuildMembers(guildID string, after string, limit int, options ...RequestOption) (st []*Member, err error)  // 要 GUILD_MEMBERS intent
func (s *Session) GuildMembersSearch(guildID, query string, limit int, options ...RequestOption) (st []*Member, err error)
```
`Member`: `GuildID`, `Nick string`, `User *User`, `Roles []string`, `Permissions int64`, `JoinedAt`。`Member.DisplayName()` は Nick → `User.DisplayName()` の順。
`User`: `Username`（一意ハンドル）, `GlobalName`（"The user's display name, if it is set. For bots, this is the application name"）, `Discriminator`（移行済みユーザーは `"0"`）, `Bot`。`User.DisplayName()` は `GlobalName` 非空ならそれ、なければ `Username`。
```go
m, _ := s.GuildMember(guildID, userID)
name := m.DisplayName() // Nick > GlobalName > Username
```

#### 2-10. レート制限の挙動

`discord.New()` の既定: `ShouldRetryOnRateLimit: true`, `MaxRestRetries: 3`, `ShouldReconnectOnError: true`, HTTP timeout 20s。
`RequestWithLockedBucket` の 429 処理（restapi.go）: レスポンスを `TooManyRequests{Bucket, Message, RetryAfter time.Duration}` に unmarshal → `cfg.ShouldRetryOnRateLimit` が true なら `time.Sleep(rl.RetryAfter)` 後に **同じ `sequence` で再帰リトライ（回数無制限）**。false なら `&RateLimitError{...}` を返す。`MaxRestRetries` が効くのは **502 Bad Gateway** の再試行のみ。リクエスト単位で `discordgo.WithRetryOnRatelimit(false)` / `WithContext(ctx)` オプション指定可。加えてバケット別ローカル rate limiter（`s.Ratelimiter`）で事前待機する。
Discord 側: グローバル 50 req/s、429 時は `Retry-After` ヘッダ/`retry_after` に従う、10 分間に 10,000 件の無効リクエスト（401/403/429）で一時 IP 制限。

---

### 3. Discord プラットフォーム制約まとめ（公式 docs から確認）

| 項目 | 値 | 出典 |
|---|---|---|
| Interaction 初回応答期限 | **3 秒**（超過でトークン無効）。トークン自体は **15 分**有効 | interactions.md L377 |
| Ephemeral flag | `1 << 6` (64) | interactions.md L511 |
| メッセージ本文 | **最大 2000 文字** | message.md L1140 |
| チャンネル名 | **1〜100 文字**。小文字/ダッシュ化は未文書化（実装上は自動変換） | channel.md L452 / Discussion #5338 |
| custom_id | **1〜100 文字**、メッセージ内で一意 | components.md L88 |
| ボタン label | 最大 80 文字、Row あたり 5 個、Row 最大 5 | components.md L188, L102, L2613 |
| ピン留め上限 | **250/チャンネル**（2025-08 に 50 から引き上げ） | Discord blog 2025-09-25 changelog |
| ピン留め権限 | `PIN_MESSAGES (1<<51)` | permissions.md L99 |
| Permission overwrite 上限 | 公式 docs に記載なし。サポート情報では「サーバー全体で 1000 overwrites」のエラー上限が存在 | Discord support (WebSearch) |
| overwrite 編集権限 | `MANAGE_ROLES`。bot が自身の持たない権限を allow/deny 不可 | channel.md L544 |

---

### 4. 使用ソース

- GitHub API: `https://api.github.com/repos/bwmarrin/discordgo`（+ `/releases/latest`, `/tags`, `/commits`）, `https://api.github.com/repos/disgoorg/disgo`（同）
- discordgo ソース: `https://raw.githubusercontent.com/bwmarrin/discordgo/v0.29.0/{restapi,structs,interactions,components,message,user,discord,endpoints}.go`、`master/{structs,endpoints,restapi}.go`
- Context7: `/bwmarrin/discordgo`（289 snippets, High reputation）
- Discord 公式: `https://docs.discord.com/developers/{interactions/receiving-and-responding,resources/channel,resources/message,resources/guild,components/reference,topics/permissions,topics/oauth2,topics/rate-limits,events/gateway}.md`
- 補足: https://github.com/discord/discord-api-docs/discussions/5338（チャンネル名制約）、https://discord.com/blog/discord-update-september-25-2025-changelog（ピン上限 250）、https://support.discord.com/hc/en-us/articles/360041033511-Server-Templates 系検索結果（overwrite 1000 上限）

**実装上の注意点（要約）**: (1) `@master` を使うか、v0.29.0 ならピン留めが deprecated エンドポイントを叩く点を許容する。(2) `PIN_MESSAGES` 定数は自前定義。(3) 日付オプションは String/Integer で代替。(4) 3 秒制限があるので重い処理は Deferred → `InteractionResponseEdit`。(5) 429 は自動リトライ（無制限）なので、ループ内での大量 API 呼び出しには `WithContext` でタイムアウトを設ける。
