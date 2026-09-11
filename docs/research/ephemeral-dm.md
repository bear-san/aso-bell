<!-- 調査エージェントによる裏取りレポート(自動抽出)。設計本文は docs/06-provider.md / docs/07-bot-ux.md を参照 -->

# Slack / Discord「本人だけに見える返信」＋DMフォールバック 調査レポート

対象: `github.com/slack-go/slack v0.29.0`（Context7: `/slack-go/slack`）、`github.com/bwmarrin/discordgo` master（Context7: `/bwmarrin/discordgo`、2026-09-11 時点の raw ソース）。公式ドキュメントは docs.slack.dev / docs.discord.com。

---

## Slack

### 1. スラッシュコマンド / ボタンへの ephemeral 返信（`response_url`）

- 即時 ack の `response_type` は「by default it is set to `ephemeral`, but you can specify a value of `in_channel`」。ack は「must be received by Slack within 3000 milliseconds … otherwise an `operation_timeout` error」。 [implementing-slash-commands](https://docs.slack.dev/interactivity/implementing-slash-commands)
- `response_url` は「can be sent up to 5 times within 30 minutes of receiving the payload」。デフォルト ephemeral。`replace_original: true` で元メッセージ置換、`delete_original` は「sole attribute」として送る。「A message's type cannot be changed from `ephemeral` to `in_channel`」。 [handling-user-interaction](https://docs.slack.dev/interactivity/handling-user-interaction)
- block_actions への ack も「must be sent within 3 seconds of receiving the payload」（同上）。`replace_original` を付けなければ（=false）元メッセージはそのまま、`response_type` 省略で ephemeral。

slack-go v0.29.0（`chat.go` / `messages.go`）:

```go
// chat.go L650
func MsgOptionResponseURL(url string, responseType string) MsgOption
// messages.go L158-161
const ResponseTypeInChannel = "in_channel"
const ResponseTypeEphemeral = "ephemeral"
// chat.go L661 / L671
func MsgOptionReplaceOriginal(responseURL string) MsgOption
func MsgOptionDeleteOriginal(responseURL string) MsgOption
```

```go
_, _, err := api.PostMessage("", // channel は無視される（endpoint=response_url）
    slack.MsgOptionResponseURL(cmd.ResponseURL, slack.ResponseTypeEphemeral),
    slack.MsgOptionText("ログインリンク: ...", false),
    slack.MsgOptionBlocks(blocks...),
)
```

**注意（ソース確認済）**: `responseURLSender.BuildRequestContext` が送る JSON は `Msg{Text, Timestamp, ThreadTimestamp, Attachments, Blocks, Metadata, ResponseType, ReplaceOriginal, DeleteOriginal}` のみ。`MsgOptionDisableLinkUnfurl()` は `config.values["unfurl_links"]` を設定するだけなので、**response_url 経路では unfurl_links が送られない**（chat.go L758-771, L402-413）。unfurl 制御が必要なら `chat.postEphemeral` を使う。

### 2. `chat.postEphemeral`

- スコープ: `chat:write`（bot/user token）。「Sends an ephemeral message to a user in a channel.」`channel` は「Channel, private group, or IM channel」、`user` は「The user should be in the channel specified」。エラー: `user_not_in_channel`「Intended recipient is not in the specified channel」、`channel_not_found`「Value passed for `channel` was invalid」、`not_in_channel`「Cannot post user messages to a channel they are not in」。Ephemeral は「do not persist across reloads, desktop and mobile apps, or sessions」。 [chat.postEphemeral](https://docs.slack.dev/reference/methods/chat.postEphemeral)
- `chat:write.public`「Send messages to channels your Slack app isn't a member of」は **`chat.postMessage` のみ**が明記され、postEphemeral への言及なし。 [chat.write.public](https://docs.slack.dev/reference/scopes/chat.write.public)
- **非メンバーのプライベートチャンネル**: 公式に明文はないが、bot が見えないチャンネルは `channel_not_found` になるのが実務上の報告（[Knock blog](https://knock.app/blog/troubleshooting-channel-not-found-in-slack-incoming-webhooks)）。設計上は `channel_not_found` / `not_in_channel` を「ephemeral 不可」としてフォールバック対象に扱う。
- **bot との DM でコマンド実行時**: `channel_id` は `D…`。ドキュメント上 IM channel は許可されるが、`D…` を渡して `channel_not_found` になった未解決報告あり（[python-slack-sdk#734](https://github.com/slackapi/python-slack-sdk/issues/734)）。DM 内なら ephemeral の意味は薄いので、`channel_id` が `D` 始まりなら最初から通常 `chat.postMessage` でよい。

```go
// chat.go L183
func (api *Client) PostEphemeral(channelID, userID string, options ...MsgOption) (string, error)
func (api *Client) PostEphemeralContext(ctx context.Context, channelID, userID string, options ...MsgOption) (timestamp string, err error)

ts, err := api.PostEphemeralContext(ctx, cmd.ChannelID, cmd.UserID,
    slack.MsgOptionText("…", false),
    slack.MsgOptionDisableLinkUnfurl(),   // unfurl_links=false
    slack.MsgOptionDisableMediaUnfurl(),  // unfurl_media=false
)
```

### 3. DM フォールバック（`conversations.open` → `chat.postMessage`）

- `conversations.open`: bot token スコープ `im:write`（他 `channels:manage, groups:write, mpim:write`）。`users`「If only one user is included, this creates a 1:1 DM」、「Don't include the ID of the user you're calling `conversations.open` on behalf of」。応答 `channel.id`（例 `D069C7QFK`）。エラー: `user_not_found`, `user_disabled`「A specified `user` has been disabled」, `user_not_visible`「The calling user is restricted from seeing the requested user」。 [conversations.open](https://docs.slack.dev/reference/methods/conversations.open)、[im:write](https://docs.slack.dev/reference/scopes/im.write)
- `chat.postMessage` は「provide the user's ID as the `channel` value」で bot からの 1:1 を開始可能とも記載（[chat.postMessage](https://docs.slack.dev/reference/methods/chat.postMessage)）。ただし明示的に `conversations.open` する方がエラー分類が明確。
- ゲスト/DM 無効化: ゲストは「same channel(s)」の相手のみ DM 可という Slack ヘルプ記載があるが、bot→ゲスト DM の API 上の可否は公式に見つからず **不明**。`user_not_visible` / `user_disabled` をハンドリングすること。

```go
// conversation.go
type OpenConversationParameters struct {
    ChannelID string
    ReturnIM  bool
    Users     []string
}
func (api *Client) OpenConversationContext(ctx context.Context, params *OpenConversationParameters) (*Channel, bool, bool, error)
// 戻り値: channel, no_op, already_open, err

ch, _, _, err := api.OpenConversationContext(ctx, &slack.OpenConversationParameters{Users: []string{uid}})
if err != nil { /* user_disabled / user_not_visible / user_not_found */ }
_, _, err = api.PostMessageContext(ctx, ch.ID,
    slack.MsgOptionText("…", false),
    slack.MsgOptionDisableLinkUnfurl(), slack.MsgOptionDisableMediaUnfurl())
```

### 4. ワークスペース識別

- `auth.test`: `url, team, user, team_id, user_id, bot_id`（bot token 時）、`enterprise_id`「when working against a team within an Enterprise organization」。スコープ不要。 [auth.test](https://docs.slack.dev/reference/methods/auth.test)。slack-go: `AuthTestResponse{URL, Team, User, TeamID, UserID, EnterpriseID, BotID}`（slack.go L42）。
- スラッシュコマンド payload: `team_id, enterprise_id, user_id, channel_id, response_url, trigger_id`。slack-go `SlashCommand` に `TeamID, EnterpriseID, IsEnterpriseInstall, ChannelID, UserID, ResponseURL, TriggerID, APIAppID`（slash.go）。
- block_actions の `team` は「Null if the app is org-installed」、`enterprise` は org 配下で付く（[block_actions payload](https://docs.slack.dev/reference/interaction-payloads/block_actions-payload)）。Grid では `enterprise_id + team_id` の組で識別し、org-install で `team` が null になり得る点に注意。
- `users.info`（`users:read`）: `team_id`, `enterprise_user{enterprise_id, teams}`, `is_restricted`, `is_ultra_restricted`, `is_stranger`。 [users.info](https://docs.slack.dev/reference/methods/users.info)

### 5. Ephemeral のボタン/リンク、Socket Mode の 3 秒

- Ephemeral に Block Kit（ボタン含む）は可: Socket Mode ドキュメントの slash ack 例自体が `payload.blocks` を返しており、block_actions payload の `container.is_ephemeral` フィールドがある（上記 2 ページ）。
- Socket Mode: 「Your app still needs to acknowledge receiving each event」、`envelope_id` で ack、`accepts_response_payload: true` なら `payload` を同梱可。 [using-socket-mode](https://docs.slack.dev/apis/events-api/using-socket-mode)。3 秒の明文は Socket Mode ページにはないが、slash の 3000ms/`operation_timeout` 規定は同じく適用される（HTTP と同じ ack 契約）。

```go
// socketmode/socket_mode_managed_conn.go L466
func (smc *Client) Ack(req Request, payload ...any) error
func (smc *Client) AckCtx(ctx context.Context, reqID string, payload any) error

smc.Ack(*evt.Request, map[string]any{
    "response_type": slack.ResponseTypeEphemeral,
    "blocks": blocks,
})
```

---

## Discord

### 6. Ephemeral インタラクション応答

- 「must send an initial response within 3 seconds」「tokens are valid for 15 minutes and can be used to send followup messages」。
- `flags`: 「only `SUPPRESS_EMBEDS`, `EPHEMERAL`, `IS_COMPONENTS_V2`, `IS_VOICE_MESSAGE`, and `SUPPRESS_NOTIFICATIONS` can be set」。`components` も callback data に含められる（＝ボタン・Link ボタン可）。
- DEFERRED (5) 時: 「the only valid [message flag] you may use is `EPHEMERAL`」、後の編集では「the ephemeral flag will be ignored, and the value you provided in the initial defer response will be preserved, as an existing message's ephemeral state cannot be changed」。
- Followup: 「You can use the `EPHEMERAL` message flag `1 << 6` (64) to send a message that only the user can see」。 [receiving-and-responding](https://docs.discord.com/developers/interactions/receiving-and-responding)
- 寿命: 公式 FAQ（[Ephemeral Messages FAQ](https://support-apps.discord.com/hc/en-us/articles/26501839512855-Ephemeral-Messages-FAQ)、直接取得は 403 のため検索スニペット引用）: 「disappear when you dismiss them, wait long enough, or restart Discord」。バックエンドに保存されない。

discordgo（interactions.go / restapi.go / webhook.go）:

```go
type InteractionResponseData struct {
    Content    string             `json:"content"`
    Components []MessageComponent `json:"components"`
    // NOTE: only MessageFlagsSuppressEmbeds and MessageFlagsEphemeral can be set.
    Flags MessageFlags `json:"flags,omitempty"`
    ...
}
func (s *Session) InteractionRespond(interaction *Interaction, resp *InteractionResponse, options ...RequestOption) error
func (s *Session) FollowupMessageCreate(interaction *Interaction, wait bool, data *WebhookParams, options ...RequestOption) (*Message, error)
type WebhookParams struct { ...; Flags MessageFlags `json:"flags,omitempty"` } // 「MessageFlagsEphemeral can only be set when using Followup Message Create endpoint.」

err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
    Type: discordgo.InteractionResponseChannelMessageWithSource, // or ...Deferred...
    Data: &discordgo.InteractionResponseData{
        Content: "ログインはこちら: <url>",
        Flags:   discordgo.MessageFlagsEphemeral | discordgo.MessageFlagsSuppressEmbeds,
        Components: []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
            discordgo.Button{Style: discordgo.LinkButton, Label: "ログイン", URL: url}}}},
    },
})
```

### 7. DM フォールバック

- `POST /users/@me/channels`（`recipient_id`）: 既存 DM があればそれを返す。警告: 「If you open a significant amount of DMs too quickly, your bot may be rate limited or blocked from opening new ones」。 [user resource](https://docs.discord.com/developers/resources/user)
- エラー: `50007`「Cannot send messages to this user」（DM 拒否設定/ブロック）、`40003`「You are opening direct messages too fast」、`50033`「Invalid Recipient(s)」。 [opcodes-and-status-codes](https://docs.discord.com/developers/topics/opcodes-and-status-codes)。DM 専用の数値レート制限は公式未記載（不明）。

```go
func (s *Session) UserChannelCreate(recipientID string, options ...RequestOption) (st *Channel, err error) // POST EndpointUserChannels("@me")
func (s *Session) ChannelMessageSendComplex(channelID string, data *MessageSend, options ...RequestOption) (*Message, error)

ch, err := s.UserChannelCreate(userID)
_, err = s.ChannelMessageSendComplex(ch.ID, &discordgo.MessageSend{
    Content: "…", Flags: discordgo.MessageFlagsSuppressEmbeds,
})
var rerr *discordgo.RESTError
if errors.As(err, &rerr) && rerr.Message != nil && rerr.Message.Code == 50007 { /* DM 不可 */ }
```

### 8. コマンドのコンテキスト

- `contexts`: `GUILD=0`「within servers」、`BOT_DM=1`「within DMs with the app's bot user」、`PRIVATE_CHANNEL=2`「Group DMs and DMs other than the app's bot user」（USER_INSTALL 時のみ意味あり）。`integration_types`: `GUILD_INSTALL=0 / USER_INSTALL=1`、「Defaults to your app's configured contexts」。`dm_permission` は「Deprecated (use `contexts` instead)」。 [application-commands](https://docs.discord.com/developers/interactions/application-commands)
- 推奨: `Contexts: &[]discordgo.InteractionContextType{discordgo.InteractionContextGuild}`, `IntegrationTypes: &[]discordgo.ApplicationIntegrationType{discordgo.ApplicationIntegrationGuildInstall}`（interactions.go L49-50, L212-217）。guild 限定にすれば ephemeral が常に成立し、`guild_id` でテナント識別できる。

### 9. モーダル

- Modal callback: `custom_id`(1-100), `title`(≤45), 「Between 1 and 5 (inclusive) components」。
- Components reference の Usage 列で Modal 可: String/User/Role/Mentionable/Channel Select、Text Input、Text Display、Label(18)、File Upload(19)、Radio Group(21)、Checkbox Group(22)、Checkbox(23)。「Action Row with Text Inputs in modals are now deprecated」「Label is recommended」。**日付ピッカーは存在しない**。 [components/reference](https://docs.discord.com/developers/components/reference)
- discordgo master: `ComponentType` は 1-19（`LabelComponent=18`, `FileUploadComponent=19`）まで。**Radio/Checkbox(21-23) は未実装**。`InteractionResponseModal = 9`、`ModalSubmitInteractionData{CustomID, Components, Resolved}`、`i.ModalSubmitData()`。
- 結論: 日付入力は Text Input（`YYYY-MM-DD HH:MM`）＋サーバー側バリデーション、または Web UI へ誘導。

---

## ワンタイムログインリンクと unfurl / embed

- **Slack**: 「Our servers must fetch every URL in a message」。「set both `unfurl_links` and `unfurl_media` to `false` … to stop Slack from crawling a link」。 [unfurling-links](https://docs.slack.dev/messaging/unfurling-links-in-messages)。ephemeral での挙動は公式未記載（[node-slack-sdk#614](https://github.com/slackapi/node-slack-sdk/issues/614) は ephemeral では attachment 画像のみ展開と報告、[python-slack-sdk#1338](https://github.com/slackapi/python-slack-sdk/issues/1338) は回答なし）。→ `PostEphemeral` に `MsgOptionDisableLinkUnfurl()` + `MsgOptionDisableMediaUnfurl()`。response_url 経路は前述の通り slack-go が unfurl フラグを送らないため、リンク配布は `chat.postEphemeral` 経路を優先。
- **Discord**: 非 defer の ephemeral 応答では招待リンクの embed が生成される報告（[discord-api-docs discussion #5290](https://github.com/discord/discord-api-docs/discussions/5290)）＝ URL はクロールされ得る。→ `MessageFlagsSuppressEmbeds` を常に付与。クローラの user-agent 等は公式未記載（不明）。
- 共通の防御: トークン消費を GET で行わず、ランディングページでの明示クリック（POST）で消費させる設計にする。

## 推奨フロー（両プラットフォーム共通）

1. Slack: `channel_id` が `D…` → 通常 `chat.postMessage`。それ以外 → `chat.postEphemeral`（unfurl off）。`channel_not_found` / `not_in_channel` / `user_not_in_channel` → `conversations.open` → `chat.postMessage`。3 秒以内に `response_url`/Socket Mode ack で「DM を送りました」等を ephemeral 返答。
2. Discord: guild 限定コマンド → `InteractionRespond`（`Ephemeral|SuppressEmbeds`）。失敗時のみ `UserChannelCreate` → `ChannelMessageSendComplex`（`SuppressEmbeds`）。`50007` はユーザーに DM 設定変更を案内。

ソース（ローカル取得済）: `/private/tmp/claude-501/-Users-kentaro-dev-aso-bell/a5671720-a4f8-40f0-95e3-1fe5c37e5dee/scratchpad/sg_*.go`, `sm_*.go`, `dg_*.go`。
