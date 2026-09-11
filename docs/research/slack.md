<!-- 調査エージェントによる裏取りレポート(自動抽出)。設計本文は docs/03-tech-stack.md を参照 -->

# Go 製 Slack Bot 向けライブラリ比較・API 検証レポート

調査日: 2026-09-11。検証手段: GitHub API、Context7 (`/slack-go/slack`)、`slack-go/slack@v0.29.0` のソースを `go get` してローカル grep、Slack 公式ドキュメント (docs.slack.dev)。

## 1. ライブラリ比較

| 項目 | github.com/slack-go/slack | github.com/slack-io/slacker | (参考) github.com/shomali11/slacker | 公式 Go SDK |
|---|---|---|---|---|
| Stars | **4,959** | **62** | 802 | 存在しない |
| SPDX License | **BSD-2-Clause** | MIT | MIT | — |
| 最新リリース | **v0.29.0** (2026-08-15) | v0.1.1 (2024-07-08) | — | — |
| 最終 push | 2026-08-24 | **2024-11-16** | 2023-11-23 | — |
| 直近コミット | Go 1.26.7/1.27 対応、socketmode の ping watchdog リファクタ、testify bump | 2024-11 の「custom command matching」が最後 | — | — |
| go.mod | `go 1.25` | — | — | — |

所見:
- **slack-go/slack が事実上の標準**。ただし README は「community-maintained、Slack 公式ではない」「major version 未リリースのため minor で breaking change あり得る」と明記 (https://github.com/slack-go/slack)。
- **公式 Go SDK は存在しない**。docs.slack.dev/tools/ に列挙される公式 SDK は Bolt (JS/Python/Java)、Node/Python/Java/Deno SDK のみ。`slackapi` org の repos にも Go 製ライブラリは無い (https://api.github.com/orgs/slackapi/repos)。
- **slack-io/slacker** は shomali11/slacker の fork で、slack-go/slack の上に載る薄いフレームワーク。約 22 ヶ月コミット無しで、star も 62。本件 (モーダル・チャンネル管理・ピン等) には slack-go/slack を直接使うのが妥当。

## 2. slack-go/slack の具体的 API (v0.29.0 ソースで確認)

### 2.1 Socket Mode / HTTP エンドポイント

```go
api := slack.New(os.Getenv("SLACK_BOT_TOKEN"),            // xoxb-
    slack.OptionAppLevelToken(os.Getenv("SLACK_APP_TOKEN")), // xapp-
    slack.OptionRetry(3))
client := socketmode.New(api, socketmode.OptionDebug(false))

go func() {
    for evt := range client.Events {
        switch evt.Type {
        case socketmode.EventTypeConnecting, socketmode.EventTypeConnected, socketmode.EventTypeConnectionError:
        case socketmode.EventTypeEventsAPI:
            ev, ok := evt.Data.(slackevents.EventsAPIEvent)
            if !ok { continue }
            client.Ack(*evt.Request)
            if ev.Type == slackevents.CallbackEvent {
                switch inner := ev.InnerEvent.Data.(type) {
                case *slackevents.MemberJoinedChannelEvent: _ = inner.User
                case *slackevents.AppHomeOpenedEvent:       _ = inner.User
                }
            }
        case socketmode.EventTypeInteractive:
            cb, ok := evt.Data.(slack.InteractionCallback)
            if !ok { continue }
            var payload any // view_submission の応答など
            switch cb.Type {
            case slack.InteractionTypeBlockActions:   /* ... */
            case slack.InteractionTypeViewSubmission: /* payload = slack.NewErrorsViewSubmissionResponse(...) */
            }
            client.Ack(*evt.Request, payload)
        case socketmode.EventTypeSlashCommand:
            cmd, ok := evt.Data.(slack.SlashCommand)
            if !ok { continue }
            client.Ack(*evt.Request, map[string]any{"response_type": slack.ResponseTypeEphemeral, "text": "受付"})
        }
    }
}()
client.Run() // or client.RunContext(ctx)
```

- 定数 (`socketmode/socketmode.go`): `EventTypeEventsAPI="events_api"`, `EventTypeInteractive="interactive"`, `EventTypeSlashCommand="slash_commands"`, ほか `EventTypeHello/Disconnect/InvalidAuth`。
- `func (smc *Client) Ack(req Request, payload ...any) error` / `AckCtx(ctx, envelopeID string, payload any)`。**20KB 以上の payload は Slack に黙って捨てられるため error を返す** (ソースコメント)。
- 実験的ルーター `socketmode.NewSocketmodeHandler(client)` に `HandleSlashCommand(command, f)`, `HandleInteractionBlockAction(actionID, f)`, `HandleViewSubmission(callbackID, f)`, `HandleEvents(slackevents.EventsAPIType, f)`, `RunEventLoop()` あり。

HTTP 方式 (examples/eventsapi/events.go):
```go
sv, err := slack.NewSecretsVerifier(r.Header, signingSecret) // (SecretsVerifier, error)
sv.Write(body); if err := sv.Ensure(); err != nil { w.WriteHeader(401); return }
ev, _ := slackevents.ParseEvent(json.RawMessage(body), slackevents.OptionNoVerifyToken())
// url_verification: slackevents.URLVerification → ChallengeResponse.Challenge を返す
```
インタラクションは `r.FormValue("payload")` を `slack.InteractionCallback` に `json.Unmarshal`、slash は `slack.SlashCommandParse(r)`。

### 2.2 Slash command

`slash.go` の struct (抜粋): `Token, TeamID, TeamDomain, ChannelID, ChannelName, UserID, UserName, Command, Text, ResponseURL, TriggerID, APIAppID` (json tag は snake_case)。`func SlashCommandParse(r *http.Request) (SlashCommand, error)`。

応答: `messages.go` に `ResponseTypeInChannel = "in_channel"`, `ResponseTypeEphemeral = "ephemeral"`。Socket Mode では `Ack` の payload、HTTP では JSON 応答、遅延応答は
```go
api.PostMessage("", slack.MsgOptionResponseURL(cmd.ResponseURL, slack.ResponseTypeInChannel), slack.MsgOptionText("done", false))
```

### 2.3 モーダル

```go
view := slack.ModalViewRequest{
    Type: slack.VTModal, CallbackID: "create_req",
    PrivateMetadata: cmd.ChannelID, // 最大 3,000 文字
    Title:  slack.NewTextBlockObject(slack.PlainTextType, "申請", false, false), // 24 文字以内
    Submit: slack.NewTextBlockObject(slack.PlainTextType, "送信", false, false),
    Close:  slack.NewTextBlockObject(slack.PlainTextType, "閉じる", false, false),
    Blocks: slack.Blocks{BlockSet: []slack.Block{
        slack.NewInputBlock("title_b", slack.NewTextBlockObject("plain_text","件名",false,false), nil,
            slack.NewPlainTextInputBlockElement(nil, "title_a")),
        slack.NewInputBlock("date_b", slack.NewTextBlockObject("plain_text","日付",false,false), nil,
            slack.NewDatePickerBlockElement("date_a")),
        slack.NewInputBlock("time_b", slack.NewTextBlockObject("plain_text","時刻",false,false), nil,
            slack.NewTimePickerBlockElement("time_a")),
        slack.NewInputBlock("num_b", slack.NewTextBlockObject("plain_text","人数",false,false), nil,
            slack.NewNumberInputBlockElement(nil, "num_a", false)),
        slack.NewInputBlock("sel_b", slack.NewTextBlockObject("plain_text","種別",false,false), nil,
            slack.NewOptionsSelectBlockElement(slack.OptTypeStatic, nil, "sel_a",
                slack.NewOptionBlockObject("a", slack.NewTextBlockObject("plain_text","A",false,false), nil))),
    }},
}
_, err := api.OpenView(cmd.TriggerID, view) // (*ViewResponse, error)
```
- 署名: `NewInputBlock(blockID string, label, hint *TextBlockObject, element BlockElement)`, `NewPlainTextInputBlockElement(placeholder *TextBlockObject, actionID string)`, `NewDatePickerBlockElement(actionID)`, `NewTimePickerBlockElement(actionID)`, `NewNumberInputBlockElement(placeholder, actionID, isDecimalAllowed bool)`, `NewOptionsSelectBlockElement(optType string, placeholder, actionID, options ...*OptionBlockObject)`。
- `ModalViewRequest` フィールド: `Type, Title, Blocks, Close, Submit, PrivateMetadata, CallbackID, ClearOnClose, NotifyOnClose, ExternalID`。
- 読み取り: `cb.View.State.Values` は `map[string]map[string]BlockAction` (block_id → action_id)。`BlockAction` (block.go) に `Value, SelectedDate, SelectedTime, SelectedOption.Value, SelectedUser, SelectedDateTime` 等。`cb.View.PrivateMetadata`, `cb.View.CallbackID` で識別。
- 応答: `slack.NewErrorsViewSubmissionResponse(map[string]string{"date_b": "必須"})`, `NewClearViewSubmissionResponse()`, `NewUpdateViewSubmissionResponse(*ModalViewRequest)`, `NewPushViewSubmissionResponse(...)` → `ViewSubmissionResponse{ResponseAction, View, Errors}`。

### 2.4 ボタン / block_actions / 更新

```go
btn := slack.NewButtonBlockElement("approve", "req-123", slack.NewTextBlockObject("plain_text","承認",false,false)).WithStyle(slack.StylePrimary)
blocks := []slack.Block{ slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType,"<@U123> から申請",false,false), nil, nil),
                         slack.NewActionBlock("approve_blk", btn) }
ch, ts, err := api.PostMessage(channelID, slack.MsgOptionBlocks(blocks...), slack.MsgOptionText("申請", false))
```
- `NewButtonBlockElement(actionID, value string, text *TextBlockObject)`, `NewActionBlock(blockID string, elements ...BlockElement)`, `NewSectionBlock(text, fields []*TextBlockObject, accessory *Accessory, opts...)`, スタイルは `StylePrimary/StyleDanger`。
- ハンドリング: `cb.Type == slack.InteractionTypeBlockActions` → `for _, a := range cb.ActionCallback.BlockActions { a.ActionID, a.BlockID, a.Value }`。元メッセージは `cb.Channel.ID`, `cb.Message.Timestamp`。
- 更新: `api.UpdateMessage(channelID, ts, slack.MsgOptionBlocks(...), slack.MsgOptionText(...))` (戻り値 4 つ: channel, ts, text, err) または `slack.MsgOptionReplaceOriginal(cb.ResponseURL)`。

### 2.5〜2.7 チャンネル作成・招待・除外・アーカイブ

```go
ch, err := api.CreateConversation(slack.CreateConversationParams{ChannelName: "req-2026-0911", IsPrivate: true})
_, err  = api.InviteUsersToConversation(ch.ID, "U1", "U2")   // (*Channel, error) 最大 1,000 人/回
err     = api.KickUserFromConversation(ch.ID, "U2")
err     = api.ArchiveConversation(ch.ID)
err     = api.UnArchiveConversation(ch.ID)
```
(Context7 の `_autodocs` は旧 `CreateConversation(name, isPrivate)` / `RemoveUserFromConversation` を示すが、v0.29.0 では上記が正。)

Slack 側の事実:
- 命名: 小文字・数字・`-`・`_` のみ、**80 文字以内**。違反は `invalid_name` / `invalid_name_specials`, 重複は `name_taken`。作成者 (bot) は自動でメンバーになる。Tier 2。
- `conversations.invite`: 呼び出し側が**メンバーであることが必須** (`not_in_channel`)。Tier 3。
- `conversations.kick`: bot scope `channels:manage` / `groups:write` (private)。**bot 自身は kick 不可 (`cant_kick_self`)**、`#general` 不可、ワークスペース設定で制限されると `restricted_action`。bot が作成した private channel からの kick は、bot がメンバーで `groups:write` があれば API 上可能 (制限は `restricted_action` で返る)。Tier 3。
- `conversations.archive`: Tier 2。**`conversations.unarchive` は bot token 不可 (`not_allowed_token_type`)、user token (xoxp) が必要**。

### 2.8 ピン

`api.AddPin(channelID, slack.NewRefToMessage(channelID, ts))`, `RemovePin(...)`, `ListPins(channel) ([]Item, *Paging, error)`。`ItemRef{Channel, Timestamp, File, Comment}`。scope `pins:write`、Tier 2。上限超過は `too_many_pins`。「1 チャンネル 100 件」という数字は第三者ブログのみで確認 (公式ヘルプは 429 で取得不可) — 要注意。

### 2.9 投稿

`PostMessage(channelID string, opts ...MsgOption) (channel, ts string, err error)`。`MsgOptionText(text string, escape bool)`, `MsgOptionBlocks(...Block)`, `MsgOptionPostEphemeral(userID)`, `MsgOptionTS(ts)` (スレッド)。メンションは `<@U0123>` を mrkdwn テキストに埋め込む。予約投稿: `ScheduleMessage(channelID, postAt string /*unix秒*/, opts...) (string, string, error)`。scope `chat:write` (bot 未参加の public に投稿するなら `chat:write.public`)。text は 4,000 文字推奨・**40,000 超で切り詰め**、section block の `text` は **3,000 文字**、fields は 10 個×2,000 文字。1 channel あたり 1 msg/秒。

### 2.10 ユーザー情報

`GetUserInfo(userID) (*User, error)`, `GetUsersInfo(ids...) (*[]User, error)`。`user.Profile.DisplayName` は空文字のことがある (公式 docs 「may contain the empty string」) ので `DisplayName → RealName → Name` のフォールバック推奨。scope `users:read` (`users:read.email` は email 用)。Tier 4。

### 2.11 イベント購読と scope 一覧

- `app_home_opened`: **scope 不要** (Home タブを使う場合のみ)。
- `member_joined_channel`: `channels:read` (public) / `groups:read` (private)。
- 上記機能を満たす bot scopes (manifest `oauth_config.scopes.bot`): `commands`, `chat:write`, `channels:manage`, `channels:read`, `groups:write`, `groups:read`, `pins:write`, `users:read` (+任意 `chat:write.public`, `users:read.email`)。Socket Mode 用の app-level token には `connections:write`。
- **bot token (xoxb) vs user token (xoxp)**: bot token はアプリ自身として動作しインストール者に依存しない。unarchive のように bot token 不可の API がある。本件は基本 bot token、unarchive が必要なら user token を別途保持。

### 2.12 レートリミット

`misc.go`: 429 かつ `Retry-After` ヘッダ有りの場合 `*slack.RateLimitedError{RetryAfter time.Duration}` (`Retryable() bool` = true) を返す。`errors.As(err, &rl)` で `time.Sleep(rl.RetryAfter)`。それ以外の非 200 は `slack.StatusCodeError{Code, Status}`、API エラー (`ok:false`) は `slack.SlackErrorResponse{Err, Errors, ResponseMetadata}`。自動リトライは `slack.OptionRetry(n)` (429 のみ) か `OptionRetryConfig(slack.RetryConfig{MaxRetries, Handlers, RetryAfterDuration, RetryAfterJitter, BackoffInitial, BackoffMax})`。Slack 側 Tier: 1=1+/min, 2=20+/min, 3=50+/min, 4=100+/min、`Retry-After` は per method・per workspace・per app。

## 3. Slack プラットフォーム事実

- **3 秒ルール**: slash command・interactive payload・view_submission は 3,000ms 以内に ack (`operation_timeout`)。Socket Mode でも `envelope_id` を 3 秒以内に返す。
- `trigger_id` は 3 秒・1 回限り (`trigger_expired` / `trigger_exchanged`)。`response_url` は 30 分以内に 5 回まで。`replace_original`/`delete_original` は slash command には使えない。
- Socket Mode: `apps.connections.open` で WS URL 取得、同時 10 接続まで、数時間ごとに `disconnect(refresh_requested)` が来るので再接続必須 (socketmode.Client が自動処理)。**Socket Mode アプリは Marketplace 配布不可** (社内利用なら問題なし)。
- モーダル: 100 blocks、`private_metadata` 3,000 文字、`callback_id` 255、title 24 文字、view stack 最大 3。
- App manifest は **YAML/JSON** (`_metadata.major_version: 2`)。例:

```yaml
_metadata: { major_version: 2 }
display_information: { name: aso-bell }
features:
  bot_user: { display_name: aso-bell, always_online: true }
  slash_commands:
    - command: /bell
      description: 申請を作成
      usage_hint: "[create|list]"
      should_escape: false
oauth_config:
  scopes:
    bot: [commands, chat:write, channels:manage, channels:read, groups:write, groups:read, pins:write, users:read]
settings:
  socket_mode_enabled: true
  interactivity: { is_enabled: true }
  event_subscriptions:
    bot_events: [member_joined_channel, app_home_opened]
```
Socket Mode 有効時は slash command の `url` と `request_url` は不要 (「Event Subscription requires Request URL or Socket Mode」)。

## 4. ソース

- GitHub API: https://api.github.com/repos/slack-go/slack, https://api.github.com/repos/slack-io/slacker, https://api.github.com/repos/shomali11/slacker, .../releases/latest, .../commits, https://api.github.com/orgs/slackapi/repos
- Context7: `/slack-go/slack` (socket mode / views / blocks / conversations / pins / errors)。`_autodocs` 由来の旧シグネチャは v0.29.0 ソースで訂正済み。
- ローカル: `/Users/kentaro/go/pkg/mod/github.com/slack-go/slack@v0.29.0/` (`conversation.go`, `views.go`, `block.go`, `block_element.go`, `chat.go`, `messages.go`, `pins.go`, `item.go`, `users.go`, `misc.go`, `retry.go`, `security.go`, `slash.go`, `interactions.go`, `socketmode/*.go`, `examples/socketmode/socketmode.go`)
- Slack docs: https://docs.slack.dev/tools/, /reference/methods/conversations.create, conversations.invite, conversations.kick, conversations.archive, conversations.unarchive, pins.add, chat.postMessage, users.info, /interactivity/handling-user-interaction, /interactivity/implementing-slash-commands, /apis/events-api/using-socket-mode, /apis/web-api/rate-limits, /surfaces/modals, /reference/block-kit/blocks/section-block, /reference/block-kit/block-elements/plain-text-input-element, /reference/events/member_joined_channel, /reference/events/app_home_opened, /reference/app-manifest, /app-manifests/configuring-apps-with-app-manifests
- ピン上限 100 件 (非公式): https://github.com/coreos/triagebot/issues/15 ほかブログ
