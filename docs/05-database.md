# 05. データベース設計 (MongoDB)

## 1. 方針

- データベース名: `asobell`(環境変数で変更可)
- 全ドキュメントに `createdAt` / `updatedAt`(UTC `Date`)を持つ
- `_id` は `ObjectID` を使用する。外部システムの ID(Slack/Discord)は文字列で保持する
- **複数ドキュメントトランザクションは使用しない**。レプリカセット無しのスタンドアロン MongoDB でも動作させるため、ユースケースは「単一ドキュメントの原子的更新 + 冪等な副作用」で構成する
- 列挙値は小文字スネークケースの文字列で保存する
- スキーマバリデーションは Go 側(ドメイン層)で行い、MongoDB の `$jsonSchema` は使用しない(スキーマ変更の柔軟性を優先)
- 時刻は `time.Time` を UTC に正規化して保存し、bson の `Date` 型で保持する

## 2. コレクション一覧

| コレクション | 用途 | 主なインデックス |
| --- | --- | --- |
| `meta` | スキーマバージョン、対になる Provider の状態 | `_id` = `"schema"` / `"provider"` |
| `workspaces` | ワークスペースと設定 | `{ provider: 1, externalId: 1 }` unique |
| `events` | イベント | `{ workspaceId: 1, status: 1, startsAt: 1 }`、`{ "channel.channelId": 1 }`、`{ "messages.announcement.messageId": 1 }` |
| `participations` | 参加者 | `{ eventId: 1, chatUserId: 1 }` unique、`{ eventId: 1, status: 1 }` |
| `jobs` | 永続ジョブキュー | `{ status: 1, runAt: 1 }`、`{ dedupeKey: 1 }` unique sparse、`{ eventId: 1, status: 1 }`、`{ finishedAt: 1 }` TTL |
| `reminder_logs` | リマインド送信履歴 | `{ eventId: 1, sequence: 1 }` |
| `console_users` | WebConsole 利用者 | `{ googleSub: 1 }` unique、`{ email: 1 }` |
| `chat_identities` | チャット ID と ConsoleUser の紐づけ | `{ workspaceId: 1, chatUserId: 1 }` unique、`{ consoleUserId: 1 }` |
| `link_tokens` | 連携トークン | `{ expiresAt: 1 }` TTL |
| `sessions` | WebConsole セッション | `{ expiresAt: 1 }` TTL、`{ userId: 1 }` |
| `oauth_states` | Google ログインの state / PKCE verifier | `{ expiresAt: 1 }` TTL |

## 3. ドキュメント定義

### 3.1 `meta`

```json
{ "_id": "schema", "version": 1 }
```

```json
{
  "_id": "provider",
  "kind": "slack",
  "address": "dns:///provider:9091",
  "capabilities": { "forms": true, "ephemeral": true, "directMessage": true },
  "version": "v0.1.0",
  "botUserId": "U0BOT",
  "connected": true,
  "status": "online",
  "firstSeenAt": "Date",
  "lastSeenAt": "Date",
  "updatedAt": "Date"
}
```

`kind` は初回接続時に記録し、以後 Manager 起動時に `GetInfo` の結果と照合する。資格情報は保存しない。

### 3.2 `workspaces`

```json
{
  "_id": "ObjectID",
  "provider": "discord",
  "externalId": "123456789012345678",
  "name": "あそび部",
  "settings": {
    "recruitChannelId": "234567890123456789",
    "timezone": "Asia/Tokyo",
    "defaultReminderPolicy": { "mode": "offsets", "offsets": ["168h0m0s", "24h0m0s", "3h0m0s"] },
    "peekDuration": "1h0m0s",
    "autoEndGrace": "24h0m0s",
    "channelNamePrefix": "ev-",
    "defaultStartTime": "19:00",
    "discord": { "eventCategoryId": null, "archiveCategoryId": null }
  },
  "createdAt": "Date",
  "updatedAt": "Date"
}
```

duration は Go の `time.Duration.String()` 形式の文字列で保存する(bson のカスタムエンコーダで変換)。

### 3.3 `events`

```json
{
  "_id": "ObjectID",
  "workspaceId": "ObjectID",
  "title": "ボドゲ会",
  "description": "18時集合、遅刻OK",
  "location": "渋谷",
  "startsAt": "Date",
  "endsAt": null,
  "status": "open",
  "organizer": { "userId": "U0123", "displayName": "kentaro" },
  "originChannelId": "C0123",
  "channel": { "channelId": "G0456", "name": "ev-0920-bodoge", "archived": false },
  "messages": {
    "announcement": { "channelId": "C0123", "messageId": "1726000000.000100" },
    "recruitAnnouncement": { "channelId": "C0999", "messageId": "1726000000.000200" },
    "summary": { "channelId": "G0456", "messageId": "1726000000.000300" }
  },
  "reminderPolicy": { "mode": "offsets", "offsets": ["168h0m0s", "24h0m0s", "3h0m0s"] },
  "participantCount": 3,
  "endedAt": null,
  "endReason": null,
  "createdVia": "chat",
  "createdBy": "U0123",
  "createdAt": "Date",
  "updatedAt": "Date"
}
```

作成は 2 段階で行う。まず `channel` / `messages` が空のドキュメントを `status: "open"` で挿入し、Provider 側の作成が終わり次第 `$set` で埋める。Provider 側で失敗した場合は `status: "canceled"`、`endReason: "provision_failed"` に更新し、作成済みのチャンネルがあればアーカイブジョブを登録する。

### 3.4 `participations`

```json
{
  "_id": "ObjectID",
  "eventId": "ObjectID",
  "workspaceId": "ObjectID",
  "chatUserId": "U0456",
  "displayName": "taro",
  "role": "peeker",
  "status": "active",
  "joinedAt": "Date",
  "expiresAt": "Date",
  "peekCount": 1,
  "updatedAt": "Date"
}
```

参加・チラ見の登録は `FindOneAndUpdate` with `upsert: true` の単一操作で行い、更新前ドキュメント(`ReturnDocument: Before`)から遷移種別を判定する。これによりボタン連打時も 1 度だけ副作用(チャンネル追加・メッセージ投稿)が実行される。

```text
filter: { eventId, chatUserId }
update:
  $setOnInsert: { joinedAt: now, peekCount: 0 }
  $set:         { role, status: "active", expiresAt, displayName, updatedAt: now }
  $inc:         { peekCount: role == "peeker" ? 1 : 0 }
```

遷移判定はアプリケーション側で行うため、`participant/active` に対する「チラ見」は事前に `FindOne` で弾く(競合しても最終状態は `participant/active` に収束するよう、`$set` は `participant` → `peeker` の降格を行わない条件付き更新にする)。

### 3.5 `jobs`

```json
{
  "_id": "ObjectID",
  "kind": "reminder",
  "runAt": "Date",
  "dedupeKey": "reminder:66e0...:3",
  "eventId": "ObjectID",
  "payload": { "eventId": "ObjectID", "sequence": 3 },
  "status": "pending",
  "attempts": 0,
  "maxAttempts": 5,
  "leaseUntil": null,
  "lastError": null,
  "createdAt": "Date",
  "updatedAt": "Date",
  "finishedAt": null
}
```

- `finishedAt` に TTL インデックス(`expireAfterSeconds: 2592000` = 30 日)を張り、完了ジョブを自動削除する
- `dedupeKey` の一意インデックスは `sparse: true`。重複登録時は `E11000` を無視する

### 3.6 `reminder_logs`

```json
{
  "_id": "ObjectID",
  "eventId": "ObjectID",
  "sequence": 3,
  "sentAt": "Date",
  "targets": [
    { "kind": "event", "channelId": "G0456", "messageId": "1726...", "ok": true },
    { "kind": "recruit", "channelId": "C0999", "messageId": null, "ok": false, "error": "channel_not_found" }
  ]
}
```

### 3.7 `console_users`

```json
{
  "_id": "ObjectID",
  "googleSub": "1122334455",
  "email": "kentaro@example.com",
  "name": "Kentaro",
  "picture": "https://...",
  "createdVia": "link",
  "createdAt": "Date",
  "lastLoginAt": "Date"
}
```

### 3.8 `chat_identities`

```json
{
  "_id": "ObjectID",
  "consoleUserId": "ObjectID",
  "workspaceId": "ObjectID",
  "provider": "slack",
  "workspaceExternalId": "T0123",
  "chatUserId": "U0123",
  "displayName": "kentaro",
  "linkedAt": "Date"
}
```

### 3.9 `link_tokens`

```json
{
  "_id": "<sha256 hex of token>",
  "workspaceId": "ObjectID",
  "chatUserId": "U0123",
  "displayName": "kentaro",
  "expiresAt": "Date",
  "consumedAt": null
}
```

消費は `FindOneAndUpdate({ _id, consumedAt: null, expiresAt: { $gt: now } }, { $set: { consumedAt: now } })` で原子的に行う。TTL は `expiresAt`。

### 3.10 `sessions`

```json
{
  "_id": "<random 32 bytes, base64url>",
  "userId": "ObjectID",
  "csrfToken": "<random 32 bytes, base64url>",
  "createdAt": "Date",
  "expiresAt": "Date",
  "lastSeenAt": "Date"
}
```

`_id` をセッション ID として Cookie に格納する。TTL インデックスは `expiresAt`。

### 3.11 `oauth_states`

```json
{
  "_id": "<state>",
  "codeVerifier": "<PKCE verifier>",
  "redirectTo": "/events",
  "expiresAt": "Date"
}
```

TTL 10 分。

## 4. インデックス作成

アプリ起動時に `EnsureIndexes(ctx)` で全インデックスを `CreateMany` する(冪等)。テストでも同じ関数を用いる。インデックス定義は `internal/store/mongo/indexes.go` に一箇所で管理する。

## 5. マイグレーション

v1 ではスキーマバージョンを `meta` コレクションの `{ _id: "schema", version: 1 }` で管理し、起動時にバージョンを確認する。将来の破壊的変更は `internal/store/mongo/migrations/` に番号付き Go 関数として追加し、起動時に順次適用する。

## 6. ローカル開発

`docker compose` で `mongo:7`(スタンドアロン)を起動する。トランザクションを使わないため replica set 設定は不要。
