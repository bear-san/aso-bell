# あそベル (aso-bell) 設計ドキュメント

Slack / Discord 上で遊びの予定を立ち上げ、連絡用プライベートチャンネルと参加者・リマインドを管理する Bot と WebConsole の設計。すべての API は Protocol Buffers で定義し、フロントエンド向けは grpc-gateway で REST 化する。Manager と Provider(チャットツール接続)は別コンテナで稼働し、gRPC で通信する。1 グループにつき Manager 1 つと Provider 1 つ(Slack または Discord)を対で配置する。

エージェント・開発者向けの作業指示は [../AGENTS.md](../AGENTS.md) にまとめている(`CLAUDE.md` はそれを参照するのみ)。

## 読む順番

| # | ファイル | 内容 |
| --- | --- | --- |
| 01 | [01-requirements.md](01-requirements.md) | 用語、FR/NFR を ID 付きで整理、ユースケース、スコープ外 |
| 02 | [02-architecture.md](02-architecture.md) | コンテナ構成、Manager / Provider の責務、依存方向、ディレクトリ、起動順序 |
| 03 | [03-tech-stack.md](03-tech-stack.md) | ライブラリ選定(Star 数・ライセンス・バージョン)、例外と理由 |
| 04 | [04-domain-model.md](04-domain-model.md) | エンティティ、状態遷移、値オブジェクト、不変条件 |
| 05 | [05-database.md](05-database.md) | MongoDB コレクション、インデックス、原子的更新パターン |
| 06 | [06-provider.md](06-provider.md) | Provider プロセスの構成(Adapter / Runtime / Render)、Slack / Discord の対応表 |
| 07 | [07-bot-ux.md](07-bot-ux.md) | スラッシュコマンド、フォーム、ボタン、文言、連携コマンド、エッジケース |
| 08 | [08-api.md](08-api.md) | WebConsole 向け API(gRPC + grpc-gateway。契約は [../proto/asobell/v1/](../proto/asobell/v1/))、REST マッピング、エラー、生成物 |
| 09 | [09-web-console.md](09-web-console.md) | 画面、ルーティング、連携ページ、フォーム、API クライアント |
| 10 | [10-auth-security.md](10-auth-security.md) | Manager に集約した認証(Google OIDC、セッション、gRPC 認証インターセプタ)、チャット連携、CSRF、脅威と対策 |
| 11 | [11-scheduler.md](11-scheduler.md) | 永続ジョブキュー、claim/リース、各ジョブハンドラ、リマインド時刻計算 |
| 12 | [12-config-deploy.md](12-config-deploy.md) | Manager / Provider の環境変数、Docker、Compose、Slack Manifest、Discord/Google セットアップ、CI |
| 13 | [13-testing.md](13-testing.md) | テストの層、Fake、bufconn、重点シナリオ、手動 E2E チェックリスト |
| 14 | [14-coding-guidelines.md](14-coding-guidelines.md) | コメント規約(WHY のみ)、Go / Proto / TS 規約、Git |
| 15 | [15-implementation-plan.md](15-implementation-plan.md) | マイルストーン、依存関係、リスク |
| 16 | [16-grpc.md](16-grpc.md) | Manager ⇄ Provider(1:1)の gRPC 契約(proto は [../proto/asobell/v1/](../proto/asobell/v1/))、エラー対応表、認証、ライフサイクル |

## 判断記録 (ADR)

| # | タイトル |
| --- | --- |
| [0001](adr/0001-modular-monolith.md) | Manager 内はモジュラーモノリス(0008 により一部改訂) |
| [0002](adr/0002-license-exceptions.md) | BSD ライセンスの例外採用(承認済み) |
| [0003](adr/0003-mongo-job-queue.md) | cron ではなく MongoDB 永続ジョブキュー |
| [0004](adr/0004-discord-library.md) | discordgo(master 固定)の採用 |
| [0005](adr/0005-discord-private-channel-overwrites.md) | Discord プライベートチャンネルは overwrite 方式 |
| [0006](adr/0006-spa-vite.md) | WebConsole は Vite SPA を Go に埋め込む |
| [0007](adr/0007-slack-socket-mode.md) | Slack は Socket Mode |
| [0008](adr/0008-grpc-topology.md) | Manager と Provider(1:1)を別コンテナに分離し、相互に gRPC サーバーを持つ |
| [0009](adr/0009-account-linking.md) | チャットアカウント連携と WebConsole の認可根拠 |
| [0010](adr/0010-grpc-gateway.md) | すべての API を gRPC で定義し、REST は grpc-gateway で導出する |

## 裏取り資料

[research/](research/) に、ライブラリの Star 数・ライセンス・API シグネチャを Context7 / GitHub API / 公式ドキュメントで確認した調査レポートを置く(2026-09-11 時点)。

- [research/go-backend.md](research/go-backend.md) — MongoDB driver、gin、OIDC、テスト等
- [research/grpc.md](research/grpc.md) — grpc-go、protobuf-go、buf、bufconn、Compose での名前解決
- [research/grpc-gateway.md](research/grpc-gateway.md) — grpc-gateway、OpenAPI 3 生成(gnostic)、protovalidate、protojson の JSON 表現
- [research/slack.md](research/slack.md) — slack-go、Socket Mode、モーダル、チャンネル操作
- [research/ephemeral-dm.md](research/ephemeral-dm.md) — Slack / Discord のエフェメラル返信、DM、リンクプレビュー
- [research/discord.md](research/discord.md) — discordgo、権限、ピン留め、コンポーネント
- [research/frontend.md](research/frontend.md) — Vite、React、react-router、shadcn/ui 等

## 決定済み事項(2026-09-11)

1. **BSD ライセンス**(`slack-go/slack`, `bwmarrin/discordgo`, `golang.org/x/oauth2`, `google.golang.org/protobuf`)は許容する(ADR 0002)
2. **golangci-lint(GPL-3.0)**は開発時ツールとして許容する
3. **Manager 1 : Provider 1**。1 グループは 1 つのチャットツールのみを使い、複数グループには Manager + Provider のペアをグループごとに用意する(ADR 0008)
4. **すべての API は gRPC + grpc-gateway**で定義し、認証は Manager に集約、フロントエンドは Cookie を保持するのみ(ADR 0010)
