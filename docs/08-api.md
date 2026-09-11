# 08. WebConsole 向け API 設計(gRPC + grpc-gateway)

## 1. 方針

- **すべての API は Protocol Buffers で定義する**。WebConsole 向けサービスには `google.api.http` アノテーションを付け、grpc-gateway が REST(JSON)へ変換する。手書きの OpenAPI は持たない
- 契約は [proto/asobell/v1/](../proto/asobell/v1/) の `event.proto` / `workspace.proto` / `account.proto` / `status.proto`。Provider 向けの `manager.proto` / `provider.proto` には REST バインディングを付けない(ゲートウェイ経由では呼べない)
- REST のベースパスは `/api/v1`。リソース指向(AIP-131〜136)に従い、カスタム動詞は `:end` のようにコロン区切り
- 認証はセッション Cookie(`asobell_session`)。ゲートウェイが Cookie を gRPC メタデータへ転写し、gRPC の認証インターセプタが検証する(→ [10-auth-security.md](10-auth-security.md) §2.1)
- CSRF: 状態変更メソッドは `X-Requested-With: asobell` ヘッダ必須(HTTP ミドルウェア)
- 入力制約は `buf.validate` アノテーションで proto に記述し、protovalidate インターセプタで検証する
- JSON 表現は protojson の既定に従う(§5)
- OpenAPI 文書と TypeScript 型は proto から生成する(§6)

## 2. サービスと REST マッピング

### 2.1 AccountService(`account.proto`)

| RPC | REST | 認証 | 説明 |
| --- | --- | --- | --- |
| `GetMe` | `GET /api/v1/me` | セッション | ログインユーザー |
| `ListIdentities` | `GET /api/v1/me/identities` | セッション | 自分の ChatIdentity 一覧 |
| `LinkIdentity` | `POST /api/v1/me/identities:link` `{ token }` | セッション | 連携トークンを消費して連携 |
| `UnlinkIdentity` | `DELETE /api/v1/me/identities/{identityId}` | セッション | 連携解除 |
| `GetLinkToken` | `GET /api/v1/link-tokens/{token}` | 不要 | 連携ページ表示用。消費しない |
| `Logout` | `POST /api/v1/me:logout` | セッション | セッション破棄。`Set-Cookie` で失効 |

Google ログインのリダイレクトフロー(`GET /auth/google/login`, `GET /auth/google/callback`)は gRPC ではなく Manager の HTTP ハンドラ(→ [10-auth-security.md](10-auth-security.md) §1)。

### 2.2 ProviderStatusService(`status.proto`)

| RPC | REST | 説明 |
| --- | --- | --- |
| `GetProviderStatus` | `GET /api/v1/provider` | 対になる Provider の状態。未接続でも 200 で `PROVIDER_STATUS_OFFLINE` |

### 2.3 WorkspaceService(`workspace.proto`)

| RPC | REST | 説明 |
| --- | --- | --- |
| `ListWorkspaces` | `GET /api/v1/workspaces` | |
| `GetWorkspace` | `GET /api/v1/workspaces/{workspaceId}` | 設定含む |
| `UpdateWorkspaceSettings` | `PATCH /api/v1/workspaces/{workspaceId}/settings` | 設定された(`optional`)フィールドのみ更新。`clearRecruitChannel: true` で募集チャンネル解除 |
| `ListWorkspaceChannels` | `GET /api/v1/workspaces/{workspaceId}/channels` | Provider に問い合わせ。offline なら 503 |

### 2.4 EventService(`event.proto`)

| RPC | REST | 説明 |
| --- | --- | --- |
| `ListEvents` | `GET /api/v1/events?workspaceId=&status=&mine=&pageSize=&pageToken=` | `mine=true` で自分の連携 ID が主催・参加しているものに絞る |
| `GetEvent` | `GET /api/v1/events/{eventId}` | |
| `CreateEvent` | `POST /api/v1/events` | 主催者はログインユーザーの当該ワークスペースの ChatIdentity。未連携は 400(`IDENTITY_REQUIRED`) |
| `UpdateEvent` | `PATCH /api/v1/events/{eventId}` | `optional` フィールドのみ更新。`clearLocation` / `clearEndsAt` で null 化 |
| `EndEvent` | `POST /api/v1/events/{eventId}:end` | 終了済みでも 200 で現状を返す |
| `CancelEvent` | `POST /api/v1/events/{eventId}:cancel` | |
| `SetReminderPolicy` | `PUT /api/v1/events/{eventId}/reminder-policy`(body は `ReminderPolicy`) | |
| `ListParticipants` | `GET /api/v1/events/{eventId}/participants` | |
| `RemoveParticipant` | `DELETE /api/v1/events/{eventId}/participants/{chatUserId}` | |
| `ListReminders` | `GET /api/v1/events/{eventId}/reminders` | 予定(pending ジョブ)と履歴 |
| `SendReminderNow` | `POST /api/v1/events/{eventId}/reminders:send` | 即時送信ジョブを登録 |

### 2.5 gRPC のみ(REST なし)

| サービス | 用途 | 参照 |
| --- | --- | --- |
| `ManagerService` | Provider → Manager | [16-grpc.md](16-grpc.md) |
| `grpc.health.v1.Health` | ヘルスチェック | |

### 2.6 HTTP ハンドラ(gRPC 外)

| パス | 説明 |
| --- | --- |
| `GET /auth/google/login?redirect=` | Google へリダイレクト |
| `GET /auth/google/callback` | セッション発行後 `redirect` へ 302 |
| `GET /auth/dev/login?email=` | `ASOBELL_DEV_LOGIN=true` のときのみ |
| `GET /healthz`, `GET /readyz` | 生存 / MongoDB ping |
| `GET /*` | WebConsole(SPA フォールバック) |

## 3. ページング

AIP-158 に従い `pageSize`(1〜100、既定 50)と `pageToken`(不透明)を使う。レスポンスの `nextPageToken` が空なら終端。`ListEvents` のみページングし、他の一覧は件数が小さいためページングしない。

## 4. エラー

gRPC ステータスをゲートウェイが HTTP へ写す。ボディは `google.rpc.Status` の JSON。

```json
{
  "code": 9,
  "message": "identity required",
  "details": [
    { "@type": "type.googleapis.com/google.rpc.ErrorInfo", "reason": "IDENTITY_REQUIRED", "domain": "asobell.dev", "metadata": { "workspaceId": "..." } }
  ]
}
```

| gRPC code | HTTP | `ErrorInfo.reason` の例 | 状況 |
| --- | --- | --- | --- |
| `INVALID_ARGUMENT` | 400 | `VALIDATION`(+ `google.rpc.BadRequest.fieldViolations`) | `buf.validate` 違反、日時の前後関係など。protovalidate の `Violations` は自作インターセプタで `BadRequest` に詰め替える |
| `FAILED_PRECONDITION` | 400 | `IDENTITY_REQUIRED`, `EVENT_NOT_OPEN`, `LINK_TOKEN_CONSUMED`, `DM_BLOCKED` | 状態が前提を満たさない |
| `UNAUTHENTICATED` | 401 | `SESSION_REQUIRED` | セッションなし・期限切れ |
| `PERMISSION_DENIED` | 403 | `CSRF`, `BOT_PERMISSION` | CSRF(HTTP ミドルウェア)、Bot の権限不足 |
| `NOT_FOUND` | 404 | `EVENT_NOT_FOUND`, `LINK_TOKEN_NOT_FOUND` | |
| `ALREADY_EXISTS` | 409 | `IDENTITY_TAKEN` | 別アカウントと連携済み |
| `RESOURCE_EXHAUSTED` | 429 | `RATE_LIMITED` | チャットツール側のレート制限 |
| `UNAVAILABLE` | 503 | `PROVIDER_UNAVAILABLE` | Provider offline |
| `INTERNAL` | 500 | – | |

フロントエンドは `details[]` の `ErrorInfo.reason` で文言を、`BadRequest.fieldViolations[].field` でフォームのフィールドエラーを表示する。

## 5. JSON 表現(protojson)

| proto | JSON |
| --- | --- |
| フィールド名 | lowerCamelCase(`starts_at` → `startsAt`) |
| `google.protobuf.Timestamp` | RFC 3339 文字列(UTC、`2026-09-20T10:00:00Z`) |
| `google.protobuf.Duration` | 秒表記文字列(`"604800s"`、`"3600s"`) |
| enum | 名前文字列(`"EVENT_STATUS_OPEN"`) |
| `int64` | 文字列 |
| `optional` 未設定 | フィールド省略(`EmitUnpopulated` は `optional` に影響しない) |
| `oneof`(`ReminderPolicy`) | 設定された 1 つのみ(`{"offsets": {"offsets": ["604800s", "86400s"]}}`) |
| 未設定の非 `optional` スカラ | `EmitUnpopulated: true` で既定値を出力(`0`, `""`, `false`) |

## 6. 生成物

| 生成物 | 生成元 | 用途 |
| --- | --- | --- |
| `gen/asobell/v1/*.pb.go` | protoc-gen-go | メッセージ型 |
| `gen/asobell/v1/*_grpc.pb.go` | protoc-gen-go-grpc | サービスのサーバー / クライアント |
| `gen/asobell/v1/*.pb.gw.go` | protoc-gen-grpc-gateway | REST → gRPC 変換ハンドラ |
| `gen/openapi/openapi.yaml` | gnostic `protoc-gen-openapi`(OpenAPI 3.0.3。→ [03-tech-stack.md](03-tech-stack.md) §2.3) | フロントエンドの型生成、API リファレンス |
| `web/src/lib/api/schema.d.ts` | openapi-typescript | `openapi-fetch` の型 |

```bash
buf generate                                   # Go + gateway + OpenAPI
pnpm --dir web gen:api                         # openapi-typescript ../gen/openapi/openapi.yaml -o src/lib/api/schema.d.ts
```

すべてコミットし、CI で再生成して差分がないことを検証する。設定は [buf.gen.yaml](../buf.gen.yaml)。

生成された OpenAPI の特徴(2026-09-11 に試行して確認):

- パスは `google.api.http` どおり(`/api/v1/events/{eventId}:end` などカスタム動詞も含む)。パラメータ名は lowerCamelCase
- スキーマ名は `asobell.v1.Event` のような完全修飾名(`fq_schema_naming=true`)
- enum は `type: string` + `enum: [...]`、Timestamp は `format: date-time`、Duration は秒表記の `pattern`
- `oneof`(`ReminderPolicy`)は 3 つの省略可能プロパティを持つオブジェクトとして出力され、排他性は表現されない。`optional` の nullable も表現されない。TS 側では該当プロパティが `?` 付きになる
- 全操作に `google.rpc.Status` の `default` レスポンスが付く

## 7. ゲートウェイの構成(`internal/manager/gateway`)

```go
mux := runtime.NewServeMux(
    runtime.WithMetadata(sessionCookieToMetadata),          // Cookie asobell_session → metadata "asobell-session"
    runtime.WithForwardResponseOption(forwardSetCookie),    // gRPC ヘッダ metadata "set-cookie" → HTTP Set-Cookie
    runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
        MarshalOptions:   protojson.MarshalOptions{EmitUnpopulated: true},
        UnmarshalOptions: protojson.UnmarshalOptions{DiscardUnknown: true},
    }),
)
conn, _ := grpc.NewClient("dns:///localhost:9090", grpc.WithTransportCredentials(insecure.NewCredentials()))
asobellv1.RegisterEventServiceHandler(ctx, mux, conn)
asobellv1.RegisterWorkspaceServiceHandler(ctx, mux, conn)
asobellv1.RegisterAccountServiceHandler(ctx, mux, conn)
asobellv1.RegisterProviderStatusServiceHandler(ctx, mux, conn)

root := http.NewServeMux()
root.Handle("/api/", csrf(securityHeaders(mux)))
root.Handle("GET /auth/google/login", authHandlers.Login)
root.Handle("GET /auth/google/callback", authHandlers.Callback)
root.Handle("GET /healthz", healthz)
root.Handle("GET /readyz", readyz)
root.Handle("/", spa)
```

- ゲートウェイは loopback の gRPC サーバーへ接続する。`RegisterXxxHandlerServer`(直接呼び出し)は認証・バリデーションのインターセプタを迂回するため使わない
- Cookie 以外の認証ヘッダ(`Authorization`)はゲートウェイで転写しない
- `X-Requested-With` と `Origin` の検査は HTTP ミドルウェアで行い、gRPC 側には持ち込まない

## 8. 認可

v1 は「ログイン済みなら全操作可」。ハンドラは `auth.PrincipalFromContext(ctx)` で主体を取り出し、`usecase` へ `Actor{Kind: console, ID}` を渡す。将来ロールを導入する際の変更点を usecase 層に閉じ込める。
