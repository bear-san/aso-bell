# 12. 設定・デプロイ

## 1. Manager の環境変数

すべて `ASOBELL_` prefix。`caarlos0/env` で構造体へ読み込み、起動時に検証する。

| 変数 | 必須 | 既定 | 説明 |
| --- | --- | --- | --- |
| `ASOBELL_HTTP_ADDR` | | `:8080` | HTTP 待受(grpc-gateway + `/auth/*` + WebConsole) |
| `ASOBELL_RPC_ADDR` | | `:9090` | gRPC 待受(Provider 向け `ManagerService` と Console 向けサービス群)。ゲートウェイは loopback でここへ接続する |
| `ASOBELL_PROVIDER_ADDR` | ✓ | | 対になる Provider の gRPC アドレス(例 `dns:///provider:9091`) |
| `ASOBELL_RPC_TOKEN` | ✓ | | Provider との共有トークン(32 バイト以上) |
| `ASOBELL_RPC_TLS_CERT` / `_KEY` / `_CA` | | | 設定時は mTLS |
| `ASOBELL_BASE_URL` | ✓ | | 外部から到達する URL。OAuth コールバック、連携 URL、CSRF Origin 検査に使用 |
| `ASOBELL_MONGO_URI` | ✓ | | 例 `mongodb://mongo:27017` |
| `ASOBELL_MONGO_DB` | | `asobell` | |
| `ASOBELL_GOOGLE_CLIENT_ID` | ✓ | | |
| `ASOBELL_GOOGLE_CLIENT_SECRET` | ✓ | | |
| `ASOBELL_ALLOWED_EMAILS` | | | 連携なしでログインを許可するメール(カンマ区切り、任意) |
| `ASOBELL_ALLOWED_DOMAINS` | | | 同ドメイン(任意) |
| `ASOBELL_SESSION_TTL` | | `168h` | |
| `ASOBELL_COOKIE_SECURE` | | `true` | 開発時 `false` |
| `ASOBELL_LINK_TOKEN_TTL` | | `10m` | 連携トークンの有効期限 |
| `ASOBELL_PROVIDER_TIMEOUT` | | `25s` | Provider RPC 1 回の上限 |
| `ASOBELL_PROVIDER_POLL_INTERVAL` | | `15s` | `GetInfo` による状態監視の間隔 |
| `ASOBELL_PROVIDER_OFFLINE_AFTER_FAILURES` | | `3` | `GetInfo` 連続失敗回数で offline とみなす |
| `ASOBELL_JOBS_POLL_INTERVAL` | | `10s` | |
| `ASOBELL_JOBS_BATCH_SIZE` | | `10` | |
| `ASOBELL_JOBS_LEASE` | | `2m` | |
| `ASOBELL_JOBS_WORKERS` | | `4` | |
| `ASOBELL_LOG_LEVEL` | | `info` | |
| `ASOBELL_LOG_FORMAT` | | `json` | |
| `ASOBELL_DEV_LOGIN` | | `false` | `true` で `/auth/dev/login?email=` を有効化(テスト専用) |

トークン生成: `openssl rand -base64 32`。

## 2. Provider の環境変数

共通(`provider-slack` / `provider-discord`):

| 変数 | 必須 | 既定 | 説明 |
| --- | --- | --- | --- |
| `ASOBELL_MANAGER_ADDR` | ✓ | | 対になる Manager の gRPC アドレス(例 `dns:///manager:9090`) |
| `ASOBELL_RPC_TOKEN` | ✓ | | Manager と同じ値 |
| `ASOBELL_RPC_TLS_CERT` / `_KEY` / `_CA` | | | 設定時は mTLS |
| `ASOBELL_PROVIDER_LISTEN_ADDR` | | `:9091` | gRPC 待受(`ProviderService`) |
| `ASOBELL_COMMAND_NAME` | | `asobell` | 登録するスラッシュコマンド名 |
| `ASOBELL_MANAGER_TIMEOUT` | | `25s` | Manager RPC の既定デッドライン(`new`/`edit`/フォーム送信は 2.5s 固定) |
| `ASOBELL_LOG_LEVEL` / `ASOBELL_LOG_FORMAT` | | `info` / `json` | |

Slack 固有:

| 変数 | 必須 | 説明 |
| --- | --- | --- |
| `ASOBELL_SLACK_BOT_TOKEN` | ✓ | `xoxb-...` |
| `ASOBELL_SLACK_APP_TOKEN` | ✓ | `xapp-...`(Socket Mode) |

Discord 固有:

| 変数 | 必須 | 説明 |
| --- | --- | --- |
| `ASOBELL_DISCORD_BOT_TOKEN` | ✓ | |
| `ASOBELL_DISCORD_APPLICATION_ID` | ✓ | |

すべての環境変数は同名のコマンドライン引数(`--manager-addr`, `--slack-bot-token` など)でも指定でき、引数が優先する。トークンを引数で渡すとプロセス一覧に露出するため、本番では環境変数またはシークレットファイル(`--slack-bot-token-file`)を推奨する。

## 3. CLI

```text
manager serve             # 全コンポーネント起動
manager migrate           # インデックス作成とスキーマ移行のみ実行して終了
manager version

provider-slack   [--manager-addr ADDR] [--slack-bot-token ...] ...
provider-discord [--manager-addr ADDR] [--discord-bot-token ...] ...
```

## 4. Docker

1 つの Dockerfile で 3 つのターゲットをビルドする。

```dockerfile
# deploy/Dockerfile
FROM node:22-alpine AS web
WORKDIR /src/web
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
COPY gen/openapi ../gen/openapi
RUN pnpm build

FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/ ./cmd/...

FROM gcr.io/distroless/static-debian12:nonroot AS manager
COPY --from=build /out/manager /manager
EXPOSE 8080 9090
ENTRYPOINT ["/manager"]
CMD ["serve"]

FROM gcr.io/distroless/static-debian12:nonroot AS provider-slack
COPY --from=build /out/provider-slack /provider-slack
EXPOSE 9091
ENTRYPOINT ["/provider-slack"]

FROM gcr.io/distroless/static-debian12:nonroot AS provider-discord
COPY --from=build /out/provider-discord /provider-discord
EXPOSE 9091
ENTRYPOINT ["/provider-discord"]
```

タイムゾーンデータは `import _ "time/tzdata"` で埋め込む。gRPC ヘルスチェック用に `grpc_health_probe` を同梱するか、Go 側で `--healthcheck` サブコマンド(自プロセスの Health を叩く)を用意する(distroless にシェルが無いため後者を採用)。

## 5. Docker Compose

1 グループ = `manager` + `provider` + `mongo` の 1 セット。Provider のイメージはグループが使うチャットツールに応じて選ぶ。

```yaml
# deploy/compose.yaml(Slack を使うグループの例)
services:
  manager:
    image: ghcr.io/<owner>/asobell-manager:latest
    env_file: manager.env                 # ASOBELL_BASE_URL, ASOBELL_GOOGLE_*, ASOBELL_RPC_TOKEN
    environment:
      ASOBELL_MONGO_URI: mongodb://mongo:27017
      ASOBELL_RPC_ADDR: ":9090"
      ASOBELL_PROVIDER_ADDR: dns:///provider:9091
    ports: ["8080:8080"]
    depends_on:
      mongo: { condition: service_healthy }
    healthcheck:
      test: ["/manager", "healthcheck"]
      interval: 10s
      timeout: 5s
      retries: 5
    restart: unless-stopped

  provider:
    image: ghcr.io/<owner>/asobell-provider-slack:latest   # Discord の場合は asobell-provider-discord
    env_file: provider.env                # ASOBELL_SLACK_BOT_TOKEN, ASOBELL_SLACK_APP_TOKEN, ASOBELL_RPC_TOKEN
    environment:
      ASOBELL_MANAGER_ADDR: dns:///manager:9090
      ASOBELL_PROVIDER_LISTEN_ADDR: ":9091"
    healthcheck:
      test: ["/provider-slack", "healthcheck"]
      interval: 10s
      timeout: 5s
      retries: 5
    restart: unless-stopped

  mongo:
    image: mongo:7
    volumes: ["mongo-data:/data/db"]
    healthcheck:
      test: ["CMD", "mongosh", "--quiet", "--eval", "db.runCommand({ ping: 1 }).ok"]
      interval: 10s
      timeout: 5s
      retries: 5
    restart: unless-stopped

volumes:
  mongo-data:
```

- `manager` と `provider` は互いに `depends_on` しない。どちらが先に起動しても再試行で接続する
- 複数グループに提供する場合は、Compose プロジェクト名(`docker compose -p groupA`)を分けて同じ構成を複数起動する。ネットワークとボリュームがプロジェクトごとに分離される。ホスト側ポート(`8080`)はグループごとに変える
- gRPC ポートはホストに公開しない(Compose ネットワーク内のみ)
- TLS 終端はリバースプロキシに任せる。`ASOBELL_BASE_URL` は `https://` で設定する
- 開発用 `compose.dev.yaml` は MongoDB のみを `27017` で公開し、Manager と Provider はホストで `go run` する(`ASOBELL_PROVIDER_ADDR=dns:///localhost:9091`, `ASOBELL_MANAGER_ADDR=dns:///localhost:9090`)

## 6. Slack App Manifest

```yaml
# deploy/slack-manifest.yaml
_metadata:
  major_version: 2
display_information:
  name: あそベル
  description: 遊びの予定を立ち上げて連絡チャンネルを管理する Bot
features:
  bot_user:
    display_name: あそベル
    always_online: true
  slash_commands:
    - command: /asobell
      description: 遊びの予定を立ち上げる・管理する
      usage_hint: "new | list | login | recruit set | end | remind 7d,1d,3h | help"
      should_escape: false
oauth_config:
  scopes:
    bot:
      - commands
      - chat:write
      - channels:manage
      - channels:read
      - groups:write
      - groups:read
      - im:write
      - pins:write
      - users:read
settings:
  socket_mode_enabled: true
  interactivity:
    is_enabled: true
  event_subscriptions:
    bot_events:
      - member_joined_channel
      - member_left_channel
  org_deploy_enabled: false
  token_rotation_enabled: false
```

セットアップ手順:

1. https://api.slack.com/apps で「From a manifest」を選び上記を貼り付ける
2. Basic Information → App-Level Tokens で `connections:write` のトークン(`xapp-`)を発行
3. Install to Workspace で Bot Token(`xoxb-`)を取得
4. `provider.env` に両トークンと `ASOBELL_RPC_TOKEN` を書き、`provider` サービス(Slack イメージ)を起動

## 7. Discord Application セットアップ

1. https://discord.com/developers/applications で Application を作成し、Bot を追加。Bot Token を取得
2. Privileged Gateway Intents は不要
3. 招待 URL: `https://discord.com/oauth2/authorize?client_id=<APP_ID>&scope=bot+applications.commands&permissions=2251800082169872`
4. `provider.env` に Bot Token、Application ID、`ASOBELL_RPC_TOKEN` を書き、`provider` サービス(Discord イメージ)を起動
5. 任意: イベント用カテゴリとアーカイブ用カテゴリを作成し、WebConsole のワークスペース設定で指定

## 8. Google OAuth クライアント

1. Google Cloud Console → 認証情報 → OAuth クライアント ID(ウェブアプリケーション)
2. 承認済みリダイレクト URI に `<ASOBELL_BASE_URL>/auth/google/callback` を登録(開発時は `http://localhost:5173/auth/google/callback` と `http://localhost:8080/auth/google/callback`)
3. OAuth 同意画面は「外部」+ 本番公開、または「内部」(Workspace)。連携 URL 経由で任意の Google アカウントがログインするため、「外部 + テストユーザー」のままだとテストユーザー以外が拒否される点に注意

## 9. CI(GitHub Actions)

| ジョブ | 内容 |
| --- | --- |
| `proto` | `buf lint`、`buf breaking --against 'https://github.com/<owner>/aso-bell.git#branch=main'`、`buf generate`(Go / gRPC / gateway / OpenAPI)の差分なし |
| `go` | `go mod tidy` 差分なし、`go generate` 差分なし、`golangci-lint run`、`go test -race ./...`(Docker 利用) |
| `web` | `pnpm install --frozen-lockfile`、`gen/openapi` からの `openapi-typescript` 差分なし、`eslint`、`tsc --noEmit`、`vitest run`、`pnpm build` |
| `docker` | main ブランチとタグで 3 ターゲットをビルドし GHCR へ push(`asobell-manager`, `asobell-provider-slack`, `asobell-provider-discord`) |

## 10. 運用

- バックアップ: `mongodump` を日次。チャットトークンは DB に無いため、`*.env` を別途安全に保管する
- ログ: 標準出力 JSON
- アップグレード: Manager → Provider の順に差し替える。どちらの再起動も相手側の再試行で自動的に再接続する。Manager 停止中に到来したジョブは起動後に順次実行される
- Provider だけ落ちている間のチャット操作は届かない(Slack は「コマンドが応答しません」表示)。復帰後、未送信のリマインド等のジョブは自動で再実行される
