# AGENTS.md — aso-bell(あそベル)開発エージェント向け指示

このファイルが、このリポジトリで作業する AI エージェント(Claude Code、Codex、Copilot など)と人間の開発者に対する**指示の原本**である。`CLAUDE.md` はこのファイルを参照するだけで、独自の内容を持たない。ルールを変える場合はこのファイルと、根拠となる `docs/` を更新する。

## 1. プロジェクト概要

Slack / Discord で遊びの予定を立ち上げ、連絡用プライベートチャンネル・参加者・リマインドを管理する Bot と WebConsole。

- **Manager**(Go): ドメイン・永続化(MongoDB)・ジョブワーカー・WebConsole 向け API・認証を持つモジュラーモノリス
- **Provider**(Go、`provider-slack` / `provider-discord`): チャットツール接続のみを担う。DB を持たない
- **WebConsole**(Vite + React + TS): Manager が静的配信する SPA
- Manager と Provider は別コンテナで **1:1** の対をなし、双方向の gRPC で通信する
- すべての API は `proto/asobell/v1/*.proto` で定義し、WebConsole 向けは grpc-gateway で REST 化する

設計は `docs/` にすべて Markdown で管理している。**実装前に該当する設計文書を読み、設計と実装がずれたら設計文書も更新する**。

| 目的 | 読む文書 |
| --- | --- |
| 全体像・読む順番・確定事項 | [docs/README.md](docs/README.md) |
| 要件(FR/NFR、ユースケース) | [docs/01-requirements.md](docs/01-requirements.md) |
| 構成・ディレクトリ・依存方向 | [docs/02-architecture.md](docs/02-architecture.md) |
| ライブラリ選定とバージョン | [docs/03-tech-stack.md](docs/03-tech-stack.md) |
| ドメイン・状態遷移・不変条件 | [docs/04-domain-model.md](docs/04-domain-model.md) |
| MongoDB コレクション・原子的更新 | [docs/05-database.md](docs/05-database.md) |
| Provider(Adapter / Runtime)と Slack / Discord 対応 | [docs/06-provider.md](docs/06-provider.md) |
| Bot の UX・文言 | [docs/07-bot-ux.md](docs/07-bot-ux.md) |
| WebConsole 向け API(REST マッピング、エラー) | [docs/08-api.md](docs/08-api.md) |
| WebConsole 画面 | [docs/09-web-console.md](docs/09-web-console.md) |
| 認証・連携・セキュリティ | [docs/10-auth-security.md](docs/10-auth-security.md) |
| ジョブキュー・リマインド計算 | [docs/11-scheduler.md](docs/11-scheduler.md) |
| 環境変数・Docker・CI | [docs/12-config-deploy.md](docs/12-config-deploy.md) |
| テスト戦略・Fake・重点シナリオ | [docs/13-testing.md](docs/13-testing.md) |
| コーディング規約(本ファイル §5 の原本) | [docs/14-coding-guidelines.md](docs/14-coding-guidelines.md) |
| マイルストーンと進捗 | [docs/15-implementation-plan.md](docs/15-implementation-plan.md) |
| Manager ⇄ Provider の gRPC 契約 | [docs/16-grpc.md](docs/16-grpc.md) |
| 判断記録 | [docs/adr/](docs/adr/) |

## 2. 確定事項(蒸し返さない)

ユーザーが明示的に決定した事項。代替案を再提案したり、確認し直したりしない。

1. ライブラリのライセンスは **Apache-2.0 / MIT を原則、BSD-2/3 は許容**。GPL などコピーレフトは採用しない(ADR 0002)
2. **golangci-lint(GPL-3.0)は開発時ツールとして許容**する
3. **Manager 1 : Provider 1**。1 グループは 1 つのチャットツールのみ。複数グループには Manager + Provider のペアをグループごとに用意する(ADR 0008)
4. **すべての API は gRPC + grpc-gateway** で定義する。認証は Manager の責務で、フロントエンドは Cookie を保持するのみ(ADR 0010)
5. Provider の資格情報(Slack / Discord トークン)は Provider の環境変数・引数でのみ設定し、DB や WebConsole では扱わない
6. チャットアカウントと Google アカウントの連携は `/asobell login` が返す一度きりの URL で行う(ADR 0009)

## 3. 開発の進め方

- **設計が先**。新しい振る舞いは `docs/` に書いてから実装する。判断の変更は既存 ADR を書き換えず、新しい ADR で supersede する
- [docs/15-implementation-plan.md](docs/15-implementation-plan.md) のマイルストーン順に進め、完了したチェックボックスを更新する
- **テストを書く → 通す → 次へ** を小さな単位で繰り返す。すべての実装にはテストを含め、`task check`(生成物差分・tidy 差分・lint・テスト)が緑の状態で次の作業へ進む
- 外部ライブラリの使い方は思い込みで書かず、**Context7 や公式ドキュメント・タグ固定ソースで裏取り**する。裏取りの結果は `docs/research/` に残す
- ライブラリを追加するときは GitHub Star 数が多いものを優先し、ライセンス・バージョンを [docs/03-tech-stack.md](docs/03-tech-stack.md) の表に追記する
- コミット・push はユーザーの指示があるときだけ行う
- Go モジュールは `github.com/bear-san/aso-bell`(git remote と一致)

## 4. よく使うコマンド

```bash
task check        # gen:check + tidy:check + lint + test(CI と同じ)
task test         # go test -race ./...(testcontainers のため Docker が必要)
task test:unit    # go test -race -short ./...(Docker 不要)
task lint         # buf lint + golangci-lint run
task gen          # buf generate(proto → gen/)
task build        # bin/ に 3 バイナリ
task dev:mongo    # 開発用 MongoDB(compose.dev.yaml)
golangci-lint fmt ./...   # goimports + golines による整形
```

ツールのバージョン: Go 1.25 以上(go.mod)、buf 1.67+、golangci-lint v2.13+、task v3。`golangci-lint` は Go のバージョンと合わせて `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@<version>` で入れる。

## 5. コーディング規約

原本は [docs/14-coding-guidelines.md](docs/14-coding-guidelines.md)。要点を再掲する。

### 5.1 コメント(最重要)

**WHAT コメント(コードが何をしているかの言い換え)を禁止する。** 許容するのは次の 2 種類のみ。

1. **godoc**: エクスポートされた識別子の先頭コメント(`// EventService は ...`)。パッケージコメントも含む
2. **WHY コメント**: そのコードが「なぜ」そうなっているかの説明。仕様上の制約、外部サービスの挙動、回避策、意図的な省略など。参照 URL や issue 番号を含めてよい

```go
// NG(WHAT)
// ユーザーをチャンネルに追加する
if err := p.AddMember(ctx, ws, ch, uid); err != nil {

// OK(WHY)
// Discord は Bot 自身が持たない権限を overwrite に含めると 403 を返すため、付与する権限は最小限に絞る。
allow := discordgo.PermissionViewChannel | discordgo.PermissionSendMessages | discordgo.PermissionReadMessageHistory
```

- `TODO` は `// TODO(owner): 内容` 形式で理由を添える。理由のない TODO は禁止
- コメントアウトされたコードはコミットしない
- 同じルールを TypeScript にも適用する(TSDoc は godoc 相当として許容)
- コメントは日本語で書く(godoc も可)。lint の `godot` は日本語の句点に合わせてピリオド強制を無効化してある

### 5.2 Go

- Go 1.25。整形は `golangci-lint fmt`(goimports + golines、行長 120)。`.golangci.yml` は [maratori の golden config](https://gist.github.com/maratori/47a4d00457a92aa426dbd48a18776322) をベースに最小限をカスタマイズしたもの。変更点はファイル冒頭に列挙する
- パッケージ名は小文字単語 1 つ。`util`, `common`, `helper` は禁止
- エラーは `fmt.Errorf("<動作>: %w", err)` でラップし文脈を積む。センチネルは `var ErrXxx = errors.New(...)`、判定は `errors.Is` / `errors.As`
- `context.Context` は第 1 引数。goroutine 起動時は必ず親 ctx から派生させ、タイムアウトを付ける
- インターフェースは利用側パッケージで定義する(`usecase/ports.go` に Repository / ProviderPort、`provider/adapter` に Adapter)
- 依存はコンストラクタ `NewXxx(deps...)` で注入する。グローバル変数・`init()` での副作用は禁止(例外: `-ldflags` で差し替える `version.Version`)
- 時刻は `Clock.Now()` から取得する。`time.Now()` の直接呼び出しは `main` と Clock 実装、テストヘルパーのみ
- ログは `slog`。メッセージは小文字の短い英語、属性名は snake_case(`event_id`, `job_kind`)。ctx がスコープにあるときは `InfoContext` 等を使う
- 設定は環境変数のみ(`caarlos0/env`)。トークン等は起動時に検証し、ログに出さない
- `internal/manager` と `internal/provider` は互いに import しない。共有は `gen/` と `internal/shared/` のみ(`depguard` で強制)
- 生成コード(`gen/`)は編集しない。lint 対象からも除外する

### 5.3 テスト

- `testify/require`(前提条件)と `assert`(検証)を使い分ける。テーブル駆動テストは `t.Run(tc.name, ...)`
- テストは外部パッケージ(`package xxx_test`)で書く。非公開関数を直接試す必要があるときだけ `*_internal_test.go` に内部パッケージテストを置く
- テストヘルパーは `t.Helper()` を付ける。`context.Background()` ではなく `t.Context()` を使う
- 外部サービス(Slack / Discord / Google)には実テストで接続しない。境界をインターフェースで切り、Fake で置き換える
- MongoDB はモックせず testcontainers(`mongo:7`)で実物を起動する。そのテストは冒頭で `if testing.Short() { t.Skip() }`(`internal/manager/testutil.StartMongo` が行う)
- 時刻は `testutil.FakeClock` を注入する
- gRPC は `bufconn` + `grpc.NewClient("passthrough:///bufnet", ...)` でインプロセス起動して検証する
- Fake(`fake.Adapter`, `fake.ProviderServer`, `fake.ManagerServer`, `memstore`)と実装は同じ契約テストを通す
- カバレッジ目標: `domain` / `manager` 90% 以上、全体 75% 以上

### 5.4 Protocol Buffers

- `buf lint`(STANDARD)に従う: サービス名は `Service` 終わり、RPC ごとに専用の `XxxRequest` / `XxxResponse`、enum は `<ENUM_NAME>_UNSPECIFIED = 0`、パッケージは `asobell.v1`
- フィールド番号は再利用しない。削除時は `reserved` を書く
- コメントは WHY のみ。プラットフォーム制約など Provider 実装者が知るべきことを書く
- Console 向けサービスの RPC には必ず `google.api.http` を付け、パスは `/api/v1/<リソース複数形>/{id}`、カスタム動詞は `:end` のようにコロン区切り(AIP-136)
- 入力制約は `buf.validate` で proto に書き、Go 側で重複して書かない(ドメイン不変条件のみドメイン層)
- 変更後は `buf generate` を実行し、生成物 `gen/` を同じコミットに含める。CI が `buf breaking` と生成差分を検査する

### 5.5 TypeScript / React(`web/`)

- `strict: true`, `noUncheckedIndexedAccess: true`
- 関数コンポーネント + hooks のみ。`default export` は route コンポーネントのみ許容
- API 型は生成物 `web/src/lib/api/schema.d.ts` から `components["schemas"]["asobell.v1.Event"]` のように参照し、手書きで重複定義しない
- サーバー状態は react-query、フォーム状態は react-hook-form。`useState` でサーバーデータを複製しない
- `dangerouslySetInnerHTML` 禁止
- 日付は `Date` を UTC として扱い、表示時のみ TZ 変換

### 5.6 Git

- ブランチ: `main` + 機能ブランチ(`feat/<topic>`, `fix/<topic>`, `docs/<topic>`)
- コミットメッセージ: Conventional Commits(`feat:`, `fix:`, `docs:`, `test:`, `refactor:`, `chore:`)。本文に WHY を書く
- 1 コミットはテストが通る状態を保つ
- 生成物(`gen/`, `web/src/lib/api/schema.d.ts`)は生成元の変更と同じコミットに含める

### 5.7 ドキュメント

- 設計変更は `docs/` の該当ファイルを更新し、判断の変更は `docs/adr/` に追記する
- README は「動かし方」に限定し、設計は `docs/` に置く

## 6. ディレクトリ構成の要点

```text
cmd/{manager,provider-slack,provider-discord}   # main のみ。組み立ては internal/*/app
proto/asobell/v1/                               # すべての API 契約(単一ソース)
gen/asobell/v1/, gen/openapi/openapi.yaml        # buf generate の出力(コミットする、編集しない)
internal/shared/{rpcauth,rpcsrv,rpcerr,logging,version,markup,channelname}   # Manager / Provider 共通
internal/manager/{app,config,domain,usecase,bot,rpc,gateway,providerclient,scheduler,store/mongo,auth,testutil}
internal/provider/{app,config,adapter,runtime,render,slack,discord,fake}
web/                                            # WebConsole(Vite + React)
deploy/                                         # Dockerfile, compose.yaml, slack-manifest.yaml
docs/                                           # 設計文書・ADR・裏取り資料
```

依存方向: `cmd` → `app` → `usecase` → `domain`。`store/mongo`, `providerclient`, `rpc`, `gateway`, `auth` は `usecase` のポートを実装する。詳細は [docs/02-architecture.md](docs/02-architecture.md) §4。
