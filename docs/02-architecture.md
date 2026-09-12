# 02. アーキテクチャ

## 1. 全体構成

```mermaid
flowchart TB
    subgraph Chat["チャットツール"]
        Slack[Slack<br/>Socket Mode]
        Discord[Discord<br/>Gateway]
    end
    subgraph P["provider コンテナ(provider-slack または provider-discord)"]
        A[Adapter<br/>slack / discord]
        R[Provider Runtime<br/>gRPC server :9091 / client]
    end
    subgraph M["manager コンテナ"]
        RPC[gRPC server :9090<br/>ManagerService(Provider 向け)<br/>Event/Workspace/Account/ProviderStatus Service(Console 向け)]
        GW[grpc-gateway :8080<br/>REST → gRPC、Cookie → metadata]
        AUTH[Auth HTTP handlers<br/>/auth/google/*]
        PC[Provider Client<br/>ProviderService 呼び出し]
        Bot[Bot Core]
        UC[Usecase 層]
        Jobs[Job Worker]
        SPA[WebConsole 静的配信 :8080]
        Store[Store]
    end
    Mongo[(MongoDB)]
    Browser[ブラウザ]
    Google[Google OIDC]

    Slack -. "どちらか一方" .-> A
    Discord -. "どちらか一方" .-> A
    A <--> R
    R -- "HandleCommand / HandleAction / ReportWorkspaces ..." --> RPC
    PC -- "GetInfo / CreatePrivateChannel / PostMessage ..." --> R
    RPC --> Bot --> UC
    RPC --> UC
    Jobs --> UC
    GW --> RPC
    AUTH --> UC
    UC --> PC
    UC --> Store --> Mongo
    Browser --> SPA
    Browser --> GW
    Browser --> AUTH
    AUTH <--> Google
```

## 2. コンポーネントと配置

| コンテナ | バイナリ | 責務 | 外部依存 |
| --- | --- | --- | --- |
| `manager` | `cmd/manager` | ドメインロジック、MongoDB、ジョブワーカー、gRPC サーバー(Provider 向け `ManagerService` と Console 向けサービス群)、grpc-gateway による REST 公開、Google 認証、WebConsole 配信、Provider への gRPC クライアント | MongoDB、Google、Provider |
| `provider` | `cmd/provider-slack` または `cmd/provider-discord` | チャットツールへの接続(Slack: Socket Mode、Discord: Gateway)、インバウンドイベントの正規化と Manager への転送、`ProviderService` gRPC サーバー | Slack または Discord、Manager |
| `mongo` | `mongo:7` | データストア。Manager のみが接続する | – |

- **Manager 1 : Provider 1**。1 グループは Slack か Discord のどちらか一方を使う。複数グループに提供する場合は、グループごとに `manager` + `provider` + `mongo` のセットを用意する
- Provider は **ステートレス**(DB を持たない)。資格情報は環境変数から受け取り、状態はすべて Manager が持つ
- 互いのアドレスは環境変数で静的に設定する(`ASOBELL_PROVIDER_ADDR` / `ASOBELL_MANAGER_ADDR`)。Manager は起動時に `GetInfo` で Provider の種別を確認し、DB に記録された種別と一致することを検証する(→ [16-grpc.md](16-grpc.md))

## 3. 設計判断

### 3.1 Manager と Provider の分離

Bot を「チャットツールに接続するアダプタ(Provider)」と「意味を解釈し状態を持つ側(Manager)」に分け、別コンテナで動かす(→ [adr/0008-grpc-topology.md](adr/0008-grpc-topology.md))。

- Provider の再起動・差し替えが Manager に影響しない
- チャットツールの SDK 依存が Provider バイナリに閉じる
- コマンドの解釈と文言(Bot Core)は Manager に置くため、Provider は薄い変換層に留まる。新しいチャットツール対応は `ProviderService` を実装する Provider バイナリを 1 つ追加すればよい
- Manager 1 : Provider 1 に限定することで、Provider の登録・発見・複数接続の管理が不要になり、両側の設定はアドレス 1 つずつで済む

### 3.2 Manager 内はモジュラーモノリス

Manager 内部の Usecase / Job Worker / HTTP API / gRPC サーバーは 1 プロセスに同居する(→ [adr/0001-modular-monolith.md](adr/0001-modular-monolith.md))。Usecase 層はインターフェース越しにしか外部へ依存しない。

### 3.3 API はすべて gRPC で定義し、REST は grpc-gateway で導出する

Provider 向けもフロントエンド向けも、Manager の API は `proto/asobell/v1/*.proto` の gRPC サービスとして定義する。フロントエンド向けサービスには `google.api.http` アノテーションを付け、grpc-gateway が `/api/v1/...` の REST(JSON)へ変換する。OpenAPI 文書と TypeScript 型は proto から生成する(→ [08-api.md](08-api.md)、[adr/0010-grpc-gateway.md](adr/0010-grpc-gateway.md))。手書きの OpenAPI や HTTP フレームワーク(gin)は使わない。

例外は HTTP リダイレクトが本質の Google ログイン(`/auth/google/login`, `/auth/google/callback`)、ヘルスチェック、SPA 静的配信で、これらは `net/http` のハンドラとしてゲートウェイと同じ `http.ServeMux` に載せる。

### 3.4 認証は Manager の責務

Google OIDC のフロー、セッションの発行・検証・失効はすべて Manager が行う。フロントエンドはセッショントークンを保持する HttpOnly Cookie を持つだけで、トークンの値を読んだり、認証ロジックを持ったりしない。ゲートウェイが Cookie を gRPC メタデータへ写し、gRPC の認証インターセプタが検証するため、Console 向けサービスのハンドラは認証済みユーザーを `context` から取り出すだけでよい(→ [10-auth-security.md](10-auth-security.md))。

### 3.5 依存の方向

```text
cmd/manager ─────────> internal/manager/app (組み立て)
                          ├─> internal/manager/rpc (全 gRPC サービス実装) ─┐
                          ├─> internal/manager/gateway (grpc-gateway, HTTP mux, SPA) │
                          ├─> internal/manager/auth (Google OIDC, セッション, インターセプタ) ─┼─> internal/manager/usecase ─> internal/manager/domain
                          ├─> internal/manager/scheduler   ─┘        │
                          ├─> internal/manager/bot (usecase を利用)   │
                          ├─> internal/manager/providerclient (usecase が定義する Provider ポートを gRPC で実装)
                          └─> internal/manager/store/mongo (usecase が定義する Repository を実装)

cmd/provider-slack ───> internal/provider/app (組み立て)
cmd/provider-discord ─┘   ├─> internal/provider/runtime (Manager 疎通・gRPC サーバー・インバウンド転送)
                          ├─> internal/provider/adapter (Adapter インターフェース、共通型)
                          ├─> internal/provider/slack | discord | fake (Adapter 実装)
                          └─> internal/provider/render (Message → Block Kit / Components)

共通: gen/asobell/v1 (生成コード)、internal/shared/{rpcauth, rpcsrv, rpcerr, markup, channelname, logging}
```

- `internal/manager/domain`: エンティティ・値オブジェクト・ドメインエラー。外部依存なし
- `internal/manager/usecase`: ユースケース。`Repository`、`ProviderPort`(Provider への操作)、`Clock` のインターフェースに依存
- `internal/manager/providerclient`: `ProviderPort` を `ProviderServiceClient` で実装し、gRPC ステータスをドメインエラーへ変換。単一の `ClientConn` と Provider 状態監視(`GetInfo` ポーリング)を持つ
- `internal/manager/rpc`: すべての gRPC サービス実装。`ManagerService` は Bot Core に委譲、Console 向けサービスは Usecase を呼ぶ。認証済み主体(`auth.Principal`)は `context` から取り出す
- `internal/manager/app`: 依存の組み立てと起動・停止順序。`manager serve` / `migrate` / `healthcheck` の実体
- `internal/manager/gateway`: grpc-gateway の `ServeMux` 構築(Cookie → metadata、エラー変換、Set-Cookie 転送)、`/auth/*`・`/healthz`・SPA を含む `http.ServeMux` の組み立て、CSRF・セキュリティヘッダのミドルウェア
- `internal/manager/bot`: コマンド解釈、文言、フォーム定義。Provider 非依存
- `internal/provider/adapter`: Provider プロセス内の `Adapter` インターフェース(チャットツール操作)と `InboundSink`(受信イベントの通知先)
- `internal/provider/runtime`: `Adapter` を `ProviderServiceServer` として公開し、`InboundSink` を `ManagerServiceClient` 呼び出しに変換する共通ランタイム。Slack / Discord の両バイナリが共有する

## 4. ディレクトリ構成

```text
aso-bell/
├── cmd/
│   ├── manager/main.go
│   ├── provider-slack/main.go
│   └── provider-discord/main.go
├── proto/asobell/v1/               # すべての API 契約(単一ソース)
│   ├── common.proto                # 共通型
│   ├── manager.proto               # ManagerService(Provider → Manager)
│   ├── provider.proto              # ProviderService(Manager → Provider)
│   ├── event.proto                 # EventService(Console、REST 化)
│   ├── workspace.proto             # WorkspaceService(Console、REST 化)
│   ├── account.proto               # AccountService(Console、REST 化)
│   └── status.proto                # ProviderStatusService(Console、REST 化)
├── gen/
│   ├── asobell/v1/                 # *.pb.go, *_grpc.pb.go, *.pb.gw.go(buf generate の出力、コミットする)
│   └── openapi/openapi.yaml        # proto から生成した OpenAPI 3.0(web の型生成に使う)
├── internal/
│   ├── shared/
│   │   ├── rpcauth/                # Bearer トークンのインターセプタ(サーバー・クライアント)
│   │   ├── rpcsrv/                 # gRPC サーバー共通の recovery / logging インターセプタ、キープアライブ設定
│   │   ├── rpcerr/                 # reason 付き gRPC エラーの生成・解析(docs/16 §3 の契約)
│   │   ├── markup/                 # 共通マークアップのパース
│   │   ├── channelname/            # チャンネル名正規化
│   │   └── logging/                # slog 設定
│   ├── manager/
│   │   ├── app/                    # 組み立て、起動・停止順序
│   │   ├── config/
│   │   ├── domain/
│   │   ├── usecase/                # event, participation, reminder, workspace, provider status, identity/link
│   │   ├── bot/                    # コマンド解釈、文言、フォーム定義
│   │   ├── rpc/                    # gRPC サービス実装(manager_service.go, event_service.go, ...)、エラー変換
│   │   ├── gateway/                # grpc-gateway ServeMux、HTTP mux、CSRF/セキュリティヘッダ、SPA 配信
│   │   ├── providerclient/         # ProviderPort の gRPC 実装、Dial、状態監視
│   │   ├── scheduler/              # ジョブワーカー
│   │   ├── store/mongo/
│   │   ├── auth/                   # Google OIDC ハンドラ、セッション、認証インターセプタ、連携トークン
│   │   └── testutil/
│   └── provider/
│       ├── app/                    # 組み立て(Adapter 選択、Runtime 起動)
│       ├── config/                 # Provider 共通設定 + Slack/Discord 固有設定
│       ├── adapter/                # Adapter / InboundSink インターフェース、型、エラー
│       ├── runtime/                # ProviderService サーバー、Manager クライアント、インバウンド転送、DM フォールバック
│       ├── render/                 # Message → 各プラットフォーム形式
│       ├── slack/
│       ├── discord/
│       └── fake/                   # テスト用 Adapter と、Manager テスト用の ProviderService フェイク
├── web/                            # WebConsole
├── deploy/
│   ├── Dockerfile                  # --target manager | provider-slack | provider-discord
│   ├── compose.yaml
│   └── slack-manifest.yaml
├── docs/
├── buf.yaml
├── buf.gen.yaml
├── Taskfile.yml
├── .golangci.yml
├── go.mod                          # 単一モジュール
└── compose.dev.yaml
```

Go モジュールは 1 つ(`github.com/<owner>/aso-bell`)。Manager と Provider は同じモジュールから別バイナリをビルドする。`internal/manager` と `internal/provider` は互いに import しない(`depguard` で強制)。共有するのは `gen/` と `internal/shared/` のみ。

## 5. 主要フロー

### 5.1 Manager 起動順序

1. 設定読み込み。必須項目欠落は即終了
2. MongoDB 接続、`Ping`、`EnsureIndexes`、スキーマバージョン確認
3. Usecase / Bot Core / Provider Client を組み立て、`ASOBELL_PROVIDER_ADDR` へ `ClientConn` を作成。`GetInfo` を backoff で試み、成功したら種別を `meta` と照合(不一致は起動中止)し `ListConnectedWorkspaces` で同期。到達不能でも起動は続行し、バックグラウンドで試み続ける
4. gRPC サーバー(全サービス + Health)を `:9090` で開始
5. grpc-gateway が loopback の `:9090` へ `grpc.NewClient` で接続し、HTTP サーバー(ゲートウェイ + `/auth/*` + 静的配信)を `:8080` で開始
6. Job Worker、Provider 状態監視ループ(`GetInfo` 15 秒ごと)開始
7. SIGINT / SIGTERM で逆順に停止。HTTP → Job Worker(実行中ジョブ完了待ち、最大 30 秒)→ gRPC(`GracefulStop` 10 秒)→ MongoDB

### 5.2 Provider 起動順序

1. 設定読み込み(資格情報・Manager アドレス)
2. gRPC サーバー(`ProviderService` + Health)開始
3. `ReportWorkspaces`(疎通確認)を成功するまで指数バックオフで再試行
4. Adapter でチャットツールへ接続、スラッシュコマンド登録
5. ワークスペースが判明したら `ReportWorkspaces`
6. SIGTERM でチャット接続を閉じ、インバウンド処理完了を待ち、gRPC を `GracefulStop`

### 5.3 チャット操作の流れ

`Chat → Adapter(受信) → Runtime(正規化、ACK) → ManagerService.HandleXxx → Bot Core → Usecase → (Repository, ProviderPort → ProviderService → Adapter(送信), JobQueue) → Reply → Runtime(描画) → Chat`

Runtime は 3 秒制約のため ACK を先に返し、Manager 呼び出しは goroutine で行う(Slack のモーダルを開く場合のみ `trigger_id` の 3 秒以内に `HandleCommand` の応答を待つ)。

### 5.4 WebConsole 操作の流れ

`Browser → HTTP middleware(CSRF, headers) → grpc-gateway(Cookie → metadata) → gRPC(loopback) → auth interceptor → validate interceptor → rpc.EventService → Usecase → (Repository, ProviderPort → ProviderService, JobQueue)`

WebConsole からのイベント作成はチャットからの作成と同じ `usecase.CreateEvent` を呼ぶ。主催者はログインユーザーの ChatIdentity。

### 5.5 副作用の指揮(Usecase 内)

「DB 更新 → Provider RPC → DB 反映」を明示的に順序付ける。Provider RPC は失敗しうるため、DB は「呼び出し前に意図を記録し、成功後に結果を記録する」二段階で更新し、ジョブで追いつけるようにする。Provider が offline の場合、対話的操作は `provider_unavailable` を返し、ジョブは backoff リトライする。

## 6. 横断的関心事

| 関心事 | 方針 |
| --- | --- |
| ログ | `log/slog` JSON。`request_id` / `rpc_method` / `job_id` / `event_id` を属性に付与。gRPC のメタデータで `request_id` を Manager ⇄ Provider 間で伝播 |
| エラー | Manager 内は `fmt.Errorf("...: %w")` + センチネル。gRPC 境界で `status` + `ErrorInfo` へ変換し、REST はゲートウェイが `google.rpc.Status` JSON と HTTP ステータスへ写す |
| 時刻 | `Clock` インターフェース。DB 保存は UTC |
| ID | `bson.NewObjectID()`。Provider には文字列として渡す |
| 設定 | 環境変数のみ(Provider はコマンドライン引数でも上書き可) |
| 秘密情報 | チャットトークンは Provider の環境変数のみ。RPC トークンは両側の環境変数。ログに出さない |
| 並行性 | Provider は受信処理と gRPC サーバーを独立した goroutine で処理。Job Worker は設定した並列数 |
| 可観測性 | Manager: `/healthz`, `/readyz`(Mongo ping)、gRPC Health(ゲートウェイ経由で `/api/v1/healthz` も可)。Provider: gRPC Health(チャット接続断で `NOT_SERVING`) |
