# 14. コーディング規約

## 1. コメント

**WHAT コメント(コードが何をしているかの言い換え)を禁止する。** 許容するのは次の 2 種類のみ。

1. godoc: エクスポートされた識別子の先頭コメント(`// EventService は ...`)。パッケージコメントも含む
2. WHY コメント: そのコードが「なぜ」そうなっているかの説明。仕様上の制約、外部サービスの挙動、回避策、意図的な省略など

```go
// NG(WHAT)
// ユーザーをチャンネルに追加する
if err := p.AddMember(ctx, ws, ch, uid); err != nil {

// OK(WHY)
// Discord は Bot 自身が持たない権限を overwrite に含めると 403 を返すため、付与する権限は最小限に絞る。
allow := discordgo.PermissionViewChannel | discordgo.PermissionSendMessages | discordgo.PermissionReadMessageHistory
```

- 参照 URL や issue 番号は WHY コメントに含めてよい
- `TODO` は `// TODO(owner): 内容` 形式で理由を添える。理由のない TODO は禁止
- コメントアウトされたコードはコミットしない

同じルールを TypeScript にも適用する(TSDoc は godoc 相当として許容)。

## 2. Go

- Go 1.25。`gofmt` + `goimports` 準拠。`golangci-lint` の設定は `.golangci.yml`(`errcheck`, `govet`, `staticcheck`, `unused`, `gocritic`, `revive`, `errorlint`, `bodyclose`, `contextcheck`, `nilerr`, `testifylint`)
- パッケージ名は小文字単語 1 つ。`util`, `common`, `helper` は禁止
- エラーは `fmt.Errorf("<動作>: %w", err)` でラップし、文脈を積む。センチネルは `var ErrXxx = errors.New(...)`、判定は `errors.Is` / `errors.As`
- `context.Context` は第 1 引数。goroutine 起動時は必ず親 ctx から派生させ、タイムアウトを付ける
- インターフェースは利用側パッケージで定義する(`manager/ports.go` に Repository、`provider/provider.go` に Provider)
- 構造体の依存はコンストラクタ `NewXxx(deps...)` で注入する。グローバル変数・`init()` での副作用は禁止
- 時刻は `Clock.Now()` から取得し、`time.Now()` の直接呼び出しは `main` と Clock 実装のみ
- ログは `slog`。メッセージは小文字の短い英語、属性名は snake_case(`event_id`, `job_kind`)
- テストは `testify/require`(前提条件)と `assert`(検証)を使い分ける。テーブル駆動テストは `t.Run(tc.name, ...)`
- テストヘルパーは `t.Helper()` を付ける。testcontainers を使うテストは冒頭で `if testing.Short() { t.Skip() }`
- 生成コード(`gen/`)は編集しない。lint 対象からも除外する

## 3. Protocol Buffers

- `buf lint`(STANDARD)に従う: サービス名は `Service` 終わり、RPC ごとに専用の `XxxRequest` / `XxxResponse`、enum は `<ENUM_NAME>_UNSPECIFIED = 0`、パッケージは `asobell.v1`
- フィールド番号は再利用しない。削除時は `reserved` を書く
- コメントは WHY のみ(フィールド名で分かることは書かない)。プラットフォーム制約など Provider 実装者が知るべきことを書く
- 生成物 `gen/` は編集しない。`buf generate` の結果をコミットし、CI で差分を検出する
- Console 向けサービスの RPC には必ず `google.api.http` を付け、パスは `/api/v1/<リソース複数形>/{id}`、カスタム動詞は `:end` のようにコロン区切り(AIP-136)
- 入力制約は `buf.validate` アノテーションで proto に書く。Go 側で同じ制約を重複して書かない(ドメイン不変条件のみドメイン層に書く)
- REST 化される RPC のフィールド名は snake_case で定義し、JSON では protojson の既定(lowerCamelCase)に任せる
- `internal/manager` と `internal/provider` は互いに import しない。共有は `gen/` と `internal/shared/` のみ(`depguard` で強制)

## 4. TypeScript / React

- `strict: true`, `noUncheckedIndexedAccess: true`
- 関数コンポーネント + hooks のみ。`default export` は route コンポーネントのみ許容
- API 型は生成物(`schema.d.ts`)から `components["schemas"]["asobell.v1.Event"]` のように参照し、手書きで重複定義しない
- サーバー状態は react-query、フォーム状態は react-hook-form。`useState` でサーバーデータを複製しない
- `dangerouslySetInnerHTML` 禁止(ESLint `react/no-danger`)
- 日付は `Date` を UTC として扱い、表示時のみ TZ 変換

## 5. Git

- ブランチ: `main` + 機能ブランチ(`feat/<topic>`, `fix/<topic>`, `docs/<topic>`)
- コミットメッセージ: Conventional Commits(`feat:`, `fix:`, `docs:`, `test:`, `refactor:`, `chore:`)。本文に WHY を書く
- 1 コミットはテストが通る状態を保つ
- 生成物(`gen/`, `web/src/lib/api/schema.d.ts`)は生成元の変更と同じコミットに含める

## 6. ドキュメント

- 設計変更は `docs/` の該当ファイルを更新し、判断の変更は `docs/adr/` に追記する(既存 ADR は書き換えず、新しい ADR で supersede する)
- README は「動かし方」に限定し、設計は `docs/` に置く
