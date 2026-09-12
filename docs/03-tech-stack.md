# 03. 技術選定

選定基準(要件より): GitHub Star 数が多いものを優先し、ライセンスは MIT / Apache-2.0 を原則とする。コピーレフト(GPL 系)は採用しない。Star 数・ライセンス・最新バージョンは 2026-09-11 に GitHub API / npm registry で確認し、API の使い方は Context7 と各リポジトリのタグ固定ソースで裏取りした。裏取りの詳細は [research/](research/) を参照。

## 1. サマリー

### 1.1 バックエンド (Go)

| 分類 | 採用 | Stars | ライセンス | バージョン | 備考 |
| --- | --- | ---: | --- | --- | --- |
| 言語 | Go | – | BSD-3 | 1.25 | 標準ライブラリを最大限使う |
| Discord | `github.com/bwmarrin/discordgo` | 5,985 | **BSD-3-Clause** | master 固定 | 例外採用(→ §2.1) |
| Slack | `github.com/slack-go/slack` | 4,959 | **BSD-2-Clause** | v0.29.0 | 例外採用(→ §2.1) |
| MongoDB | `go.mongodb.org/mongo-driver/v2` | 8,539 | Apache-2.0 | v2.9.1 | 公式。Manager のみ |
| gRPC | `google.golang.org/grpc` | 23,052 | Apache-2.0 | v1.83.2 | Manager ⇄ Provider(→ [16-grpc.md](16-grpc.md)) |
| Protobuf | `google.golang.org/protobuf` | 3,349 | **BSD-3-Clause** | v1.36.12 | grpc-go の必須依存。例外採用(→ §2.1) |
| エラー詳細 | `google.golang.org/genproto/googleapis/rpc` | – | Apache-2.0 | v0.0.0-20260904194346 | `errdetails.ErrorInfo`(→ [16-grpc.md](16-grpc.md) §3)。grpc-go の必須依存として既に推移的に入っている |
| protoc plugin | `protoc-gen-go` / `protoc-gen-go-grpc` | – | BSD-3 / Apache-2.0 | v1.36.12 / v1.6.2 | buf のリモートプラグインとして利用 |
| Proto ツール | `bufbuild/buf` | 11,429 | Apache-2.0 | v1.73.0 | lint / generate / breaking |
| REST ゲートウェイ | `github.com/grpc-ecosystem/grpc-gateway/v2` | 20,002 | **BSD-3-Clause** | v2.30.0 | `google.api.http` → REST(→ §2.3)。BSD 許容済み |
| OpenAPI 生成 | `google/gnostic` `protoc-gen-openapi`(BSR `buf.build/community/google-gnostic-openapi`) | 2,300 | Apache-2.0 | v0.7.1 | proto → OpenAPI 3.0(→ §2.3) |
| 入力検証 | `buf.build/go/protovalidate`(`bufbuild/protovalidate-go`) | 1,559 / 488 | Apache-2.0 | v1.2.2 / v1.4.0 | `buf.validate` アノテーション |
| OIDC | `github.com/coreos/go-oidc/v3` | 2,476 | Apache-2.0 | v3.21.0 | |
| OAuth2 | `golang.org/x/oauth2` | 5,898 | **BSD-3-Clause** | v0.37.0 | Go 公式、go-oidc の必須依存。例外採用 |
| 設定 | `github.com/caarlos0/env/v11` | 6,308 | MIT | v11.4.1 | (→ §2.4) |
| テスト | `github.com/stretchr/testify` | 26,203 | MIT | v1.12.1 | |
| テスト(DB) | `github.com/testcontainers/testcontainers-go` + `modules/mongodb` | 4,974 | MIT | v0.44.0 | |
| モック | `go.uber.org/mock` | 3,404 | Apache-2.0 | v0.6.0 | 手書き Fake を優先し、必要時のみ |
| ログ | `log/slog` | – | (stdlib) | – | |
| 乱数・ハッシュ | `crypto/rand`, `crypto/sha256` | – | (stdlib) | – | 連携トークン・セッション ID |
| Unicode 正規化 | `golang.org/x/text` | 807 | **BSD-3-Clause** | v0.41.0 | チャンネル名の NFKC 正規化。Go 公式。例外採用(→ §2.1) |
| Lint | `golangci-lint` | 19,367 | **GPL-3.0** | v2.13.2 | 開発時ツール(→ §2.2) |
| フォーマッタ | `gofmt` + `goimports` | – | BSD-3 | – | gofumpt は使わない(BSD-3 の追加依存を避ける) |
| タスクランナー | `github.com/go-task/task` | 16,123 | MIT | v3.53.1 | |

不採用(理由付き):

| 候補 | 理由 |
| --- | --- |
| `disgoorg/disgo`(Apache-2.0, 607★) | ライセンス上は理想的で保守も活発だが、Star 数と実績で discordgo に劣る。discordgo の保守停滞が問題になった場合の移行先として Provider 抽象で備える |
| `robfig/cron` / `go-co-op/gocron` | スケジュールは MongoDB 永続ジョブキュー + `time.Ticker` で実現するため不要(→ [adr/0003](adr/0003-mongo-job-queue.md)) |
| `golang-jwt/jwt` | セッションはサーバー側(MongoDB)に保持し不透明 ID を Cookie に載せるため JWT 不要 |
| `gorilla/sessions` | BSD-3 かつ更新停滞。自前の薄いセッション層で十分 |
| `spf13/viper`(MIT, 30,458★) | Star 数は最多だが、環境変数のみで十分な本件には依存が重く、`AutomaticEnv` + `Unmarshal` の既知の落とし穴がある。`caarlos0/env` を採用 |
| `gin-gonic/gin`(MIT, 89,200★)/ `oapi-codegen` | API をすべて gRPC で定義し REST は grpc-gateway で導出するため、HTTP フレームワークと手書き OpenAPI は不要(→ [adr/0010](adr/0010-grpc-gateway.md)) |
| `go-playground/validator` | 入力制約は proto の `buf.validate` に一本化。Go 構造体タグでの重複定義を避ける |
| grpc-gateway `protoc-gen-openapiv2` + `swagger2openapi` | Swagger 2.0 出力で `openapi-typescript` v7 が受け付けず、変換ツールは 2021 年から更新停止 |
| grpc-gateway `protoc-gen-openapiv3` | OpenAPI 3.1 で `oneof` も表現できるが Alpha(「output is not yet stable」)。安定後に gnostic から乗り換えを検討 |
| `connectrpc/connect-go` + `connect-es` | URL が `/pkg.Service/Method` 固定で `google.api.http` の REST パスを使わない。要件が「grpc-gateway による REST」のため不採用 |
| `vektra/mockery` | BSD-3。`go.uber.org/mock` で代替 |
| `cenkalti/backoff` | ジョブワーカーの backoff は数行で書けるため導入しない |
| `grpc-ecosystem/go-grpc-middleware/v2`(Apache-2.0, 6,762★) | 認証・recovery・ログのインターセプタは合計 100 行程度で自作できるため v1 では導入しない。必要になれば採用可(ライセンス上の問題なし) |

### 1.2 フロントエンド (WebConsole)

| 分類 | 採用 | Stars | ライセンス | バージョン |
| --- | --- | ---: | --- | --- |
| ビルド | Vite + `@vitejs/plugin-react` | 82,788 | MIT | 8.3.0 |
| UI ライブラリ | React | 250,036 | MIT | 19.3.0 |
| 言語 | TypeScript | 110,996 | Apache-2.0 | 7.0.2 |
| ルーティング | `react-router`(Data Mode) | 56,572 | MIT | 8.3.1 |
| サーバー状態 | `@tanstack/react-query` | 50,278 | MIT | 5.102.8 |
| CSS | Tailwind CSS v4 | 97,499 | MIT | 4.3.3 |
| コンポーネント | shadcn/ui(Radix) | 123,565 | MIT | CLI 4.21.0 |
| フォーム | `react-hook-form` + `zod` | 44,852 / 43,927 | MIT | 7.87.0 / 4.6.2 |
| API クライアント | `openapi-typescript` + `openapi-fetch` | 8,363 | MIT | 7.13.0 / 0.17.0 |
| 日付 | `date-fns` + `@date-fns/tz` | 36,646 | MIT | 4.4.0 / 1.5.0 |
| テスト | Vitest + Testing Library + Playwright | 17,090 / 19,650 / 95,963 | MIT / MIT / Apache-2.0 | 5.0.0 / 16.3.3 / 1.63.0 |
| Lint / Format | ESLint 10 + typescript-eslint + Prettier | 27,497 / 16,387 / 52,245 | MIT | |
| パッケージ管理 | pnpm | 36,487 | MIT | 12.3.4 |

不採用: Next.js(142k★, MIT)は Star 数で上回るが、Go バイナリが静的配信する SPA には SSR ランタイムが不要で、静的エクスポートすると利点が消えるため Vite を採用(→ [adr/0006](adr/0006-spa-vite.md))。dayjs(48k★)は TZ 対応がプラグイン依存で `Date` 互換性が薄いため date-fns v4 を採用。npm CLI は Artistic-2.0 のため pnpm。

### 1.3 インフラ

| 分類 | 採用 | 備考 |
| --- | --- | --- |
| MongoDB | `mongo:7`(公式イメージ) | スタンドアロンで可(トランザクション不使用) |
| コンテナ | Docker マルチステージビルド | `node:22` で web をビルド → `golang:1.25` で 3 バイナリ → `gcr.io/distroless/static`(ターゲット別イメージ) |
| オーケストレーション | Docker Compose | `manager` + `provider-*` + `mongo` |

## 2. 判断の補足

### 2.1 BSD ライセンスの例外採用

要件は「Apache もしくは MIT」だが、括弧書きで「特に GPL などコピーレフトが強いライセンスは避ける」とあり、意図はコピーレフト回避と解釈した。BSD-2/3-Clause は MIT と同等の許諾型ライセンスであり、以下を例外として採用する(→ [adr/0002-license-exceptions.md](adr/0002-license-exceptions.md))。

| ライブラリ | ライセンス | 代替の有無 |
| --- | --- | --- |
| `slack-go/slack` | BSD-2 | 実質的な代替なし(公式 Go SDK は存在せず、MIT の `slack-io/slacker` は本ライブラリのラッパーで更新停止) |
| `bwmarrin/discordgo` | BSD-3 | `disgoorg/disgo`(Apache-2.0)あり。Star 数で discordgo を選択したが、**ユーザー判断で disgo に切り替え可能** |
| `golang.org/x/oauth2` | BSD-3 | Go 公式。go-oidc の依存で回避不能 |
| `google.golang.org/protobuf` | BSD-3 | Google 公式。grpc-go の依存で回避不能 |
| `grpc-ecosystem/grpc-gateway` | BSD-3 | 要件で指定されたツール。Apache/MIT の同等品なし |
| `golang.org/x/text` | BSD-3 | Go 公式。NFKC 正規化は標準ライブラリに無く、grpc-gateway 等の依存としても既に推移的に入る |

この例外が受け入れられない場合、Discord は disgo に差し替える(Provider 抽象の範囲内で吸収可能)。Slack については BSD-2 を受け入れる以外に現実的な選択肢がない。

### 2.2 golangci-lint (GPL-3.0)

開発時に実行するだけで成果物にリンク・同梱されないため、GPL の伝播は発生しない。「配布物にコピーレフトコードを含めない」という趣旨には反しないと判断して採用する。組織方針で GPL ツールの使用自体が不可の場合は `go vet` + `staticcheck`(MIT)の個別実行に切り替える。

### 2.3 API は proto を単一ソースとし、REST・OpenAPI・TS 型を生成する

- WebConsole 向けサービスは `google.api.http` アノテーション付きの gRPC サービスとして定義し、grpc-gateway(`protoc-gen-grpc-gateway` の生成物 `*.pb.gw.go`)が REST へ変換する
- OpenAPI 3.0 は gnostic の `protoc-gen-openapi` で生成し(`naming=json`, `enum_type=string`, `fq_schema_naming=true`)、`openapi-typescript` で TS 型にする。gnostic は `oneof` と `optional` の nullable を表現しないため、`ReminderPolicy` のような oneof は「全プロパティが省略可能なオブジェクト」として型付けされる(実用上は許容し、必要なら TS 側で狭める)
- 入力制約は `buf.validate` で proto に書き、protovalidate の gRPC インターセプタで検証する。go-grpc-middleware のインターセプタは `buf.validate.Violations` を details に付けるため、フロントエンドで扱いやすい `google.rpc.BadRequest` に詰め替える薄いインターセプタを自作する
- 上記の `buf generate`(remote plugins: `protocolbuffers/go:v1.36.11`, `grpc/go:v1.6.2`, `grpc-ecosystem/gateway:v2.30.0`, `community/google-gnostic-openapi:v0.7.1`)が 2026-09-11 時点で成功し、REST パス(`/api/v1/events/{eventId}:end` 等)・enum 文字列・Duration/Timestamp の形式が期待どおり出力されることを確認済み

### 2.4 設定ライブラリ

設定は環境変数のみ(Twelve-Factor)。`caarlos0/env` は `env:"NAME,required" envDefault:"..."` タグで構造体へ直接読み込め、依存が `stdlib` のみで軽い。

### 2.5 gRPC / Protobuf ツールチェーン

- `grpc.Dial` は非推奨のため `grpc.NewClient` を使う(既定リゾルバが `dns` になり、Compose のサービス名 `dns:///manager:9090` を解決できる)
- `buf` の v2 設定(`buf.yaml`, `buf.gen.yaml`)でリモートプラグイン `buf.build/protocolbuffers/go` と `buf.build/grpc/go` を使い、生成物 `gen/` をコミットする。CI は `buf lint` と `buf breaking` を実行する
- `STANDARD` lint により、サービス名は `Service` 終わり、RPC ごとに `XxxRequest` / `XxxResponse` を専用定義、enum は `_UNSPECIFIED = 0`
- in-process テストは `bufconn` + `grpc.NewClient("passthrough:///bufnet", grpc.WithContextDialer(...))`(`passthrough` の明示が必要)

### 2.6 discordgo のバージョン固定

v0.29.0(2025-05)のピン留めエンドポイントは Discord 側で非推奨化されており、`master` で新エンドポイント(`PUT /channels/{id}/messages/pins/{mid}`)へ更新済み。`go get github.com/bwmarrin/discordgo@<master の特定コミット>` で疑似バージョンに固定し、`go.mod` に理由をコメントで残す。`PIN_MESSAGES`(`1<<51`)の定数は discordgo に無いため `internal/provider/discord/permissions.go` で定義する。

## 3. 検証済み API 要点(実装時に参照)

| 領域 | 要点 | 詳細 |
| --- | --- | --- |
| mongo-driver v2 | `mongo.Connect(opts)` は ctx を取らない。`bson.ObjectID` は `v2/bson` 直下。`IndexModel.Keys` は `bson.D`。TTL は `SetExpireAfterSeconds` | [research/go-backend.md](research/go-backend.md) §1 |
| net/http | Go 1.22+ の `ServeMux` はメソッド付きパターン(`GET /auth/google/callback`)と `{id}` を解決。graceful shutdown は `http.Server.Shutdown` | 同 §2 |
| go-oidc | `oidc.NewProvider(ctx, "https://accounts.google.com")`、`provider.Verifier(&oidc.Config{ClientID})`、PKCE は `oauth2.GenerateVerifier` / `S256ChallengeOption` / `VerifierOption` | 同 §4 |
| testcontainers | `mongodb.Run(ctx, "mongo:7")`、`ConnectionString(ctx)`。replica set は不要 | 同 §8 |
| x/text 正規化 | `norm.NFKC.String(s)` が NFKC 正規化。`norm.Form` は `NFC` / `NFD` / `NFKC` / `NFKD` の 4 値で、`String` / `Bytes` / `IsNormal` を持つ | [research/text-normalization.md](research/text-normalization.md) |
| grpc-gateway | `runtime.NewServeMux(WithMetadata, WithForwardResponseOption, WithMarshalerOption(JSONPb), WithErrorHandler)`、`RegisterXxxHandler(ctx, mux, conn)`(`HandlerServer` はインターセプタを迂回するため不使用)、`HTTPStatusFromCode`(FailedPrecondition→400, Unavailable→503 等)、`Cookie` は既定で `grpcgateway-cookie` として転送、`Authorization` は常に `authorization` へ転送 | [research/grpc-gateway.md](research/grpc-gateway.md) |
| protovalidate | `buf.validate.field` の `string.{min_len,max_len,pattern}`、`duration.{gte,lte}`、`repeated.{min_items,max_items,items}`、`required`、`oneof.required`、message 単位の `cel`。Go は `protovalidate.New()` + `Validate(msg)` | 同 |
| protojson | int64 は文字列、enum は名前、Timestamp は RFC 3339、Duration は `"604800s"`、FieldMask は camelCase | 同 |
| grpc-go | `NewClient` + `WithTransportCredentials`、`ChainUnaryInterceptor`、`metadata.FromIncomingContext` で Bearer 検証、`status.WithDetails(&errdetails.ErrorInfo{})`、`keepalive` の Client `Time` ≥ Server `EnforcementPolicy.MinTime`、`health.NewServer()`、service config `retryPolicy` | [research/grpc.md](research/grpc.md) |
| Slack ephemeral/DM | `PostEphemeralContext` + `MsgOptionDisableLinkUnfurl/MediaUnfurl`。`response_url` 経路は unfurl フラグが送られない。DM は `OpenConversationContext` → `PostMessageContext`(`im:write`) | [research/ephemeral-dm.md](research/ephemeral-dm.md) |
| Discord ephemeral/DM | `InteractionResponseData{Flags: Ephemeral \| SuppressEmbeds}`、Deferred 後は ephemeral 状態を変更不可。DM は `UserChannelCreate` → `ChannelMessageSendComplex`、`50007` は DM 拒否。コマンドは `Contexts: [Guild]` に限定 | 同 |
| slack-go | Socket Mode は `socketmode.New(api)`、`client.Ack(*evt.Request, payload)`。`CreateConversation(CreateConversationParams{IsPrivate: true})`、`InviteUsersToConversation`、`KickUserFromConversation`、`ArchiveConversation`、`AddPin(ch, NewRefToMessage(ch, ts))`、`OpenView(triggerID, ModalViewRequest)` | [research/slack.md](research/slack.md) |
| discordgo | `ApplicationCommandBulkOverwrite`、`InteractionRespond`(Deferred 5/6 → `InteractionResponseEdit` / `FollowupMessageCreate`)、`GuildChannelCreateComplex` + `PermissionOverwrite`、`ChannelPermissionSet/Delete`、`ChannelEdit`、`ChannelMessagePin`、`GuildMember(...).DisplayName()` | [research/discord.md](research/discord.md) |
| フロント | Vite `server.proxy` で `/api`,`/auth` を Go へ。react-router の loader で `/api/me` 401 → `redirect("/login")`。openapi-fetch `createClient<paths>({ baseUrl: "/api", credentials: "include" })` + 401 middleware | [research/frontend.md](research/frontend.md) |

## 4. バージョン固定方針

- Go: `go.mod` の `go` ディレクティブと `toolchain` で固定。Manager と Provider は同一モジュール・同一依存バージョン
- Proto プラグイン: `buf.gen.yaml` でバージョンを固定
- Node: `web/package.json` の `packageManager` フィールド(pnpm)と `.node-version`
- Docker イメージ: タグ + digest で固定
- 依存更新は Renovate / Dependabot(v1 では手動)
