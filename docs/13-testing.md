# 13. テスト戦略

## 1. 原則

- 実装は「テストを書く → 通す → 次へ」を小さな単位で繰り返す。各マイルストーンの完了条件は `task test` が緑であること
- 外部サービス(Slack / Discord / Google)には実テストで接続しない。境界をインターフェースで切り、Fake で置き換える
- MongoDB はモックせず、testcontainers で実物を起動して検証する(クエリ・インデックス・原子性が本質的にテスト対象のため)
- 時刻は `manager.Clock` 経由で取得し、テストでは固定時刻の Fake Clock を注入する

## 2. テストの層

| 層 | 対象 | 手段 | 実行時間目安 |
| --- | --- | --- | --- |
| 単体 | `domain`(バリデーション、状態遷移、リマインド時刻計算、チャンネル名正規化、日時パース) | 純粋関数のテーブル駆動テスト | ms |
| 単体 | `manager/usecase` | Fake Repository(インメモリ)+ Fake ProviderPort + Fake Clock | ms |
| 単体 | `manager/bot`(コマンド解釈、文言、フォーム定義) | Fake Usecase | ms |
| 単体 | `shared/markup`, `shared/channelname`, `shared/rpcauth`, `shared/rpcsrv`, `provider/render` | 純粋関数・インターセプタ・描画の単体テスト | ms |
| 統合 | `manager/store/mongo` | testcontainers `mongo:7` | 秒 |
| 統合 | `manager/scheduler` | testcontainers + Fake ハンドラ | 秒 |
| 統合 | `manager/rpc`(Console 系サービス) | `bufconn` で gRPC サーバーを起動し、生成クライアントで呼ぶ。認証インターセプタ・protovalidate を通す | 秒 |
| 統合 | `manager/app` | testcontainers `mongo:7` + 到達できない Provider アドレスで `serve` を起動し、`/healthz`・`/readyz`・gRPC Health・Provider offline・積み残しジョブの実行を検証 | 秒 |
| 統合 | `manager/gateway` | `httptest` でゲートウェイ + HTTP ミドルウェアを起動し、Cookie 付き REST → gRPC → レスポンス JSON / エラー JSON / `Set-Cookie` を検証 | 秒 |
| 統合 | `manager/rpc` + `manager/providerclient` | `bufconn` で ManagerService と `fake.ProviderServer` を起動し、相互呼び出し(HandleAction 中の AddMember)を検証 | 秒 |
| 統合 | `provider/runtime` | `bufconn` で Fake ManagerService を起動し、`fake.Adapter` と組み合わせて疎通再試行・`GetInfo` 応答・ACK タイミング・DM フォールバック・エラー変換を検証 | 秒 |
| 統合 | `provider/app` | 実ポートの Fake ManagerService に対して `serve` を起動し、疎通 → チャット接続 → Health SERVING と共有トークン認証を検証 | 秒 |
| アダプタ | `provider/slack`, `provider/discord` | `httptest.Server` で Web API をスタブ(描画・エラー変換の検証)。Gateway/Socket Mode の接続は手動 E2E | 秒 |
| コンテナ | Compose 全体 | `docker compose up` で manager + `provider-fake`(fake.Adapter を載せたテスト用バイナリ)+ mongo を起動し、`GetInfo` 疎通 → WebConsole からイベント作成 → provider-fake に RPC が届くことを確認(nightly) | 分 |
| フロント | コンポーネント、フォームバリデーション、API クライアント middleware | Vitest + Testing Library | 秒 |
| E2E | ログイン→接続登録→イベント作成→終了 | Playwright + dev login | 分(手動 / nightly) |

## 3. Fake 実装

| Fake | 場所 | 概要 |
| --- | --- | --- |
| `fake.Adapter` | `internal/provider/fake` | Provider プロセス内 `Adapter` のインメモリ実装。チャンネル・メンバー・メッセージ・ピンを map で保持。`Calls()`、`FailNext(method, err)` |
| `fake.Responder` | `internal/provider/fake` | `Ack` / `Followup` / `OpenForm` の呼び出しを順番付きで記録する Responder。Runtime の ACK 順序の検証に使う |
| `fake.ProviderServer` | `internal/provider/fake` | `ProviderServiceServer` のインメモリ実装。Manager 側テストで `bufconn` 上に起動 |
| `fake.ManagerServer` | `internal/provider/fake` | `ManagerServiceServer` のインメモリ実装。Provider Runtime のテスト用。受け取った Command/Action を記録し、設定した Reply を返す |
| `memstore` | `internal/manager/memstore` | Repository インターフェースのインメモリ実装。usecase・scheduler・rpc の単体テスト専用 |
| `FakeClock` | `internal/testutil` | `Now()` を固定、`Advance(d)` で進める |
| `FakeIDs` | `internal/testutil` | 連番 ID |

Repository は `memstore` と `store/mongo` の両方が `internal/manager/storetest` の契約テストを通し、Fake の挙動が実装と乖離しないようにする。`Adapter` の契約テストは `internal/provider/adaptertest` に置き、`fake.Adapter` が通す。Slack / Discord の Adapter は Gateway / Socket Mode への実接続が要る `Connect` を持ち契約テストを走らせられないため、`httptest` スタブで同じ振る舞い(冪等性、センチネルエラーへの変換)を個別に検証する。gRPC 経由の `ProviderPort` 実装(`providerclient`)は `fake.ProviderServer` を bufconn で起動して検証する。

## 4. 重点シナリオ

### 4.1 ドメイン

- ReminderPolicy: offsets の重複・負値・30 件超、interval の `every < 1h`、`at` 形式、DST のある TZ(`America/New_York`)での時刻列
- Participation 遷移表(→ [04-domain-model.md](04-domain-model.md) §2.5.1)の全行
- チャンネル名正規化: 日本語、絵文字、80/100 文字境界、`archived_` 付与後の長さ
- 日時パース: `9/20 19:00`、`明日 19:00`、`+3h`、過去日付の翌年繰り上げ、TZ

### 4.2 Manager

- CreateEvent: 正常系、`ErrNameTaken` の再試行、Provider 失敗時の `provision_failed` とアーカイブジョブ登録
- Join/Peek: 同時押下(goroutine × 10)で副作用が 1 回、チラ見→参加で `peek_expire` が取消される
- EndEvent: 冪等(2 回目は no-op)、ピン解除・ボタン無効化・アーカイブジョブ登録、pending リマインドの取消
- SetReminderPolicy: ジョブ再生成、既送信時刻と同じリマインドは生成されない

### 4.3 Store(MongoDB)

- 一意インデックス(`participations`, `workspaces`, `jobs.dedupeKey`)
- `UpsertParticipation` の `ReturnDocument: Before` による遷移判定
- `ClaimJob` の排他性(並列 20 goroutine で 1 件のみ取得)、リース切れ再取得
- TTL インデックスの存在(実際の削除はタイミング依存のため検証しない)

### 4.4 Scheduler

- backoff 計算、`maxAttempts` 到達で `failed`
- `ErrPermanent` で即 `failed`
- 停止時に実行中ジョブの完了を待つ

### 4.5 gRPC

- Bearer トークン不一致で `UNAUTHENTICATED`、Health は認証不要
- `GetInfo` の種別が `meta.provider.kind` と不一致なら起動中止
- `GetInfo` 3 回連続失敗で `offline`、成功で `online`。`connected: false` でも `offline`
- Manager 起動時の `ListConnectedWorkspaces` と Provider からの `ReportWorkspaces` の両方で upsert される
- Provider ステータス → ドメインエラー変換(`ErrorInfo.reason` の全パターン)
- `HandleCommand(new)` が 2.5 秒デッドライン内にフォーム定義を返す(遅い Repository を注入して超過時の挙動も確認)
- Provider offline 時の `CreateEvent` が `provider_unavailable` を返す

### 4.6 連携

- `/asobell login` でトークンが発行され、`Reply` が EPHEMERAL・`suppress_preview` になる
- GET `/link-tokens/{token}` は消費しない。POST で消費、二重 POST は 409
- 期限切れ・別アカウント連携済みの拒否
- 連携トークン付き redirect による初回ログインでアカウントが作成され、トークン無し・許可リスト外は 403

### 4.7 API

- バリデーションエラーの `errors[]`

### 4.8 Provider Runtime / アダプタ

- Runtime: Manager 未到達時の固定文言、`OpenForm` の呼び分け、EPHEMERAL → DM フォールバックの条件(`ErrUnsupported`, `ErrNotFound`)、DM も失敗した場合のエラー返却

- Slack: Block Kit 描画(ボタン、メンション、`{{time}}` 変換)、`already_in_channel` → `ErrAlreadyMember` 等のエラー変換、`too_many_pins` → `ErrPinLimit`
- Discord: components 描画、`custom_id`、permission overwrite の allow/deny ビット、`archived_` リネームの冪等性、2000 文字切り詰め

## 5. ツールと実行

```yaml
# Taskfile.yml(抜粋)
tasks:
  test:        { cmds: ["go test -race -count=1 ./..."] }
  test:unit:   { cmds: ["go test -race -short ./..."] }        # testcontainers を使うテストは testing.Short() でスキップ
  test:web:    { dir: web, cmds: ["pnpm vitest run"] }
  lint:        { cmds: ["golangci-lint run ./...", "pnpm --dir web lint"] }
  gen:         { cmds: ["go generate ./...", "pnpm --dir web gen:api"] }
  check:       { deps: [gen, lint, test, test:web] }
```

- testcontainers は Docker が必要。`-short` で統合テストをスキップできるようにし、ローカルの高速ループを確保する
- カバレッジ目標: `domain` / `manager` 90% 以上、全体 75% 以上(CI で計測、閾値は警告のみ)
- `go test -race` を常時有効

## 6. 手動 E2E チェックリスト(リリース前)

| # | 手順 | 期待 |
| --- | --- | --- |
| 0 | `docker compose up` | manager が起動し、provider が WebConsole の Provider 状態に online で表示される |
| 1 | Slack で `/asobell new` → モーダル送信 | プライベートチャンネル作成、立ち上げメッセージがピン留め、概要がピン留め |
| 2 | 別ユーザーが「参加」 | チャンネルに追加、参加メッセージ、参加者数 +1 |
| 3 | 別ユーザーが「チラ見」→ 1 時間後(テストでは `peekDuration=2m`) | 追加 → 除外 |
| 4 | チラ見中に「参加」 | 昇格、除外されない |
| 5 | `/asobell remind 1m`(テスト用短縮) | 両チャンネルへリマインド |
| 6 | `/asobell end` | ピン解除、ボタン無効化、Slack アーカイブ |
| 7 | Discord 構成のグループで同様 | `archived_` リネーム、カテゴリ移動 |
| 8 | WebConsole で参加者除外 | チャンネルから除外、メッセージ投稿 |
| 9 | Manager 再起動 | Provider 状態が online に戻り、未送信リマインドが起動後に送信される |
| 10 | Provider 再起動 | Manager の監視で online に戻り、WebConsole からの操作が再び成功する |
| 11 | `/asobell login`(Slack / Discord) | 本人にだけ URL が見える。URL を開き Google ログイン → 連携完了。WebConsole の「自分のイベント」に表示される |
| 12 | WebConsole からイベント作成 | チャットにチャンネル・告知・ピンが作られ、主催者が `/asobell end` で終了できる |
| 13 | Discord で Bot からの DM を拒否した状態で `/asobell login` | エフェメラルで URL が届く(Discord はインタラクション応答なので DM 不要) |
