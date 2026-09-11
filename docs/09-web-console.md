# 09. WebConsole 設計

## 1. 構成

- Vite + React 19 + TypeScript の SPA。`web/` 配下
- ビルド成果物(`web/dist`)を Go バイナリに `embed` し、`/` 配下で静的配信する。`/api/*`, `/auth/*`, `/healthz`, `/readyz` 以外の GET は `index.html` を返す(SPA フォールバック)
- 開発時は Vite dev server(`:5173`)から `/api`, `/auth` を Go(`:8080`)へプロキシする
- API 型は proto から生成した OpenAPI(`gen/openapi/openapi.yaml`)を `openapi-typescript` で TS 型にし、`openapi-fetch` で呼び出す。REST のパスは grpc-gateway が `google.api.http` アノテーションから導出する
- 認証ロジックは持たない。Manager が発行した HttpOnly Cookie がブラウザに保存され、`fetch` が自動送信するだけ。フロントエンドはトークンの値を読まない
- 状態管理はサーバー状態を `@tanstack/react-query`、フォーム状態を `react-hook-form` に任せ、グローバルストアは持たない

## 2. ディレクトリ

```text
web/
├── src/
│   ├── main.tsx                 # QueryClientProvider + RouterProvider
│   ├── router.tsx               # ルート定義、保護レイアウト loader
│   ├── lib/
│   │   ├── api/
│   │   │   ├── schema.d.ts      # gen/openapi/openapi.yaml からの生成物(コミット)
│   │   │   ├── client.ts        # createClient<paths>、401/CSRF middleware
│   │   │   └── queries.ts       # queryKey と hooks(useEvents, useEvent, ...)
│   │   ├── time.ts              # TZ 変換・表示フォーマット
│   │   └── duration.ts          # Go duration 文字列 ↔ 表示
│   ├── components/
│   │   ├── ui/                  # shadcn/ui 生成物
│   │   ├── layout/              # AppShell、Sidebar、Header
│   │   └── common/              # ProblemAlert、ConfirmDialog、EmptyState
│   ├── features/
│   │   ├── auth/                # LoginPage、useMe
│   │   ├── link/                # 連携ページ(/link/:token)
│   │   ├── account/             # 自分の連携一覧・解除
│   │   ├── events/              # 一覧、詳細、作成/編集フォーム、ReminderPolicyForm
│   │   ├── provider/            # Provider 状態(読み取り専用)
│   │   └── workspaces/          # 設定ページ
│   └── test/                    # setup、テストユーティリティ
├── index.html
├── vite.config.ts
├── tsconfig.json
├── eslint.config.mjs
├── components.json              # shadcn/ui
└── package.json
```

## 3. ルーティングと画面

| パス | 画面 | 概要 |
| --- | --- | --- |
| `/login` | ログイン | 「Google でログイン」ボタン(`<a href="/auth/google/login?redirect=...">`、Manager の HTTP ハンドラへのトップレベル遷移)。エラー(`?error=forbidden`)には「チャットで `/asobell login` を実行して連携 URL から入ってください」と案内 |
| `/link/:token` | 連携ページ | 未ログインなら `/login?redirect=/link/:token` へ。`GET /link-tokens/:token` で内容を表示し、「連携する」ボタンで `POST /me/identities/link`。期限切れ・消費済み・他アカウント連携済みを表示 |
| `/` | ダッシュボード | 自分のイベント(`mine=true`)、募集中イベント、Provider offline 警告 |
| `/events` | イベント一覧 | フィルタ(ワークスペース、ステータス、自分のみ)、ページング、作成ボタン |
| `/events/new` | イベント作成 | ワークスペース(自分が連携済みのもののみ選択可) → 投稿先チャンネル → 内容 → リマインドポリシー。主催者は自動的に自分の連携 ID |
| `/events/:id` | イベント詳細 | 概要、参加者テーブル(除外ボタン)、リマインド予定/履歴(即時送信)、終了/中止ボタン、編集ダイアログ |
| `/provider` | Provider 状態 | 種別、接続状態、アドレス、能力、担当ワークスペース、最終確認時刻、バージョン。読み取り専用 |
| `/workspaces` | ワークスペース一覧 | |
| `/workspaces/:id` | ワークスペース設定 | 募集チャンネル(セレクト、`/channels` から取得)、TZ、既定ポリシー、チラ見時間、自動終了、Discord カテゴリ |
| `/account` | アカウント | Google 情報、連携済みチャット ID の一覧と解除 |

`/login` と `/link/:token`(未ログイン時)以外は保護レイアウト配下。loader で `GET /api/v1/me` を呼び、401 なら `redirect("/login?redirect=<path>")`。

### 3.1 連携ページの状態

| 状態 | 表示 |
| --- | --- |
| 有効 | 「{{workspaceName}} の {{displayName}} を {{email}} と連携します」+「連携する」ボタン |
| 期限切れ / 消費済み(404) | 「このリンクは無効です。チャットで `/asobell login` を再実行してください」 |
| 他アカウントと連携済み(`alreadyLinked`) | 「このチャットユーザーは別の Google アカウントと連携済みです。先に `/asobell unlink` を実行してください」 |
| 連携成功 | 「連携しました」+ ダッシュボードへのリンク |

## 4. 主要コンポーネント設計

### 4.1 ReminderPolicyForm

3 モードをラジオで切り替える。

| モード | 入力 |
| --- | --- |
| offsets | チップ入力(`7d`, `1d`, `3h` のような表記を受理し、Go duration に変換)。プリセットボタン: 標準 / 前日と3時間前 |
| interval | 間隔(日数セレクト 1〜7)、時刻(`HH:MM`)、最終リマインド(開始の何時間前か) |
| none | なし |

zod スキーマで `mode` による discriminated union を定義し、API の `ReminderPolicy` 型と一致させる。

### 4.2 EventForm

| フィールド | UI | バリデーション |
| --- | --- | --- |
| workspaceId | セレクト(作成時のみ)。選択肢は `GET /me/identities` にあるワークスペース。連携が無い場合は「チャットで `/asobell login` を実行してください」を表示 | 必須 |
| originChannelId | セレクト(ワークスペース選択後に `/channels` を取得。Provider offline なら取得失敗を表示) | 必須 |
| title | テキスト | 1〜80 |
| startsAt | 日付 + 時刻(ワークスペース TZ で入力し UTC ISO に変換して送信) | 必須、作成時は未来 |
| endsAt | 日付 + 時刻(任意) | startsAt より後 |
| location | テキスト | ≤200 |
| description | テキストエリア | ≤1000 |
| reminderPolicy | ReminderPolicyForm | |

### 4.3 参加者テーブル

列: 表示名、ユーザー ID、役割(参加 / チラ見)、状態、参加日時、チラ見期限、操作(除外)。除外は ConfirmDialog を挟む。

### 4.4 エラー表示

ゲートウェイのエラー JSON(`google.rpc.Status`: `code`, `message`, `details[]`)を `ErrorAlert` で表示する。`details[]` の `google.rpc.BadRequest.fieldViolations[]` があればフォームの該当フィールド(`field` は proto のフィールドパス。camelCase へ変換)へ `setError` で反映する。`google.rpc.ErrorInfo.reason`(`IDENTITY_REQUIRED`, `PROVIDER_UNAVAILABLE` など)で文言を切り替える。

## 5. API クライアント

```ts
// パスは OpenAPI 側で /api/v1/... を含むため baseUrl は "/"
export const api = createClient<paths>({
  baseUrl: "/",
  credentials: "include",
  headers: { "X-Requested-With": "asobell" },
});

api.use({
  async onResponse({ response }) {
    if (response.status === 401 && !location.pathname.startsWith("/login")) {
      location.assign(`/login?redirect=${encodeURIComponent(location.pathname)}`);
    }
    return response;
  },
});
```

react-query の `retry` は 4xx では行わない。ミューテーション成功時は関連 `queryKey` を `invalidateQueries` する。ログアウトは `POST /api/v1/me:logout` を呼ぶだけで、Cookie の失効は Manager が `Set-Cookie` で行う。

JSON の表現は protojson に従う: 時刻は RFC 3339 文字列、`google.protobuf.Duration` は `"604800s"` のような秒表記文字列、enum は `EVENT_STATUS_OPEN` のような名前、`int64` は文字列。`lib/duration.ts` が秒表記と表示(`7日`)を相互変換する。

## 6. 時刻の扱い

- API とのやり取りは RFC 3339(UTC)
- 表示はイベントが属するワークスペースの `settings.timezone` で `format(date, "M/d(E) HH:mm", { in: tz(timezone) })`
- 入力フォームはワークスペース TZ の壁時計時刻を `TZDate` で組み立てて `toISOString()` する

## 7. スタイル

- Tailwind CSS v4 + shadcn/ui。テーマは既定(light)。ダークモードは v1 ではシステム設定追従のみ
- レイアウトは左サイドバー + メインカラム。モバイルは最低限のレスポンシブ(サイドバーをドロワー化)

## 8. テスト

- コンポーネント: Vitest + Testing Library。API は `msw` ではなく `fetch` のスタブ(`vi.stubGlobal`)で最小限に(依存を増やさない)
- 型検査: `tsc --noEmit` を CI で実行
- E2E: Playwright。ログインは Go 側のテスト用エンドポイント(`ASOBELL_DEV_LOGIN=true` のときのみ有効な `/auth/dev/login?email=`)でセッションを発行して回避する

## 9. ビルドと配信

- `pnpm build` → `web/dist`
- Go 側 `internal/api/spa.go` は `//go:embed dist` した `fs.FS` を `http.FileServer` で配信。`index.html` は `Cache-Control: no-cache`、ハッシュ付きアセットは `max-age=31536000, immutable`
- Dockerfile のマルチステージで web を先にビルドし、Go ビルドコンテキストへコピーする
