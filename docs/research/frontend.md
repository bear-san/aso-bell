<!-- 調査エージェントによる裏取りレポート(自動抽出)。設計本文は docs/03-tech-stack.md を参照 -->

# Web Console フロントエンド技術選定レポート

調査日: 2026-09-11。Star 数は GitHub API (`gh api repos/OWNER/REPO`)、バージョン・license は npm registry (`/latest`) の当日値。ライセンスは GitHub の `license.spdx_id` と npm `license` を併記。

## サマリー表

| カテゴリ | 採用 | Stars | License | 最新版 |
|---|---|---|---|---|
| ビルド/フレームワーク | Vite + @vitejs/plugin-react (SPA) | 82,788 / 1,157 | MIT | vite 8.3.0 / plugin-react 6.1.1 |
| UI ライブラリ | React | 250,036 | MIT | 19.3.0 |
| 言語 | TypeScript | 110,996 | Apache-2.0 | 7.0.2 |
| ルーティング | react-router (Data Mode) | 56,572 | MIT | 8.3.1 |
| サーバー状態 | @tanstack/react-query | 50,278 | MIT | 5.102.8 |
| UI | Tailwind CSS v4 + shadcn/ui (Radix) | 97,499 / 123,565 / 19,260 | MIT | tailwindcss 4.3.3, shadcn CLI 4.21.0 |
| フォーム | react-hook-form + zod + @hookform/resolvers | 44,852 / 43,927 | MIT | 7.87.0 / 4.6.2 / 5.9.1 |
| API クライアント | openapi-typescript + openapi-fetch | 8,363 (monorepo) | MIT | 7.13.0 / 0.17.0 |
| テスト | Vitest + @testing-library/react + Playwright | 17,090 / 19,650 / 95,963 | MIT / MIT / Apache-2.0 | 5.0.0 / 16.3.3 / 1.63.0 |
| Lint/Format | ESLint + typescript-eslint + Prettier | 27,497 / 16,387 / 52,245 | MIT | 10.10.0 / 8.70.0 / 3.9.6 |
| パッケージ管理 | pnpm | 36,487 | MIT | 12.3.4 |
| 日付/TZ | date-fns + @date-fns/tz | 36,646 / 268 | MIT (npm) | 4.4.0 / 1.5.0 |

---

## 1. ビルドツール / フレームワーク

| 候補 | Stars | License | 最新 |
|---|---|---|---|
| vitejs/vite | 82,788 | MIT | 8.3.0 |
| vercel/next.js | 142,234 | MIT | 16.3.4 |
| remix-run/react-router (Framework Mode) | 56,572 | MIT | 8.3.1 |

**推奨: Vite + React (純粋な SPA)**。Star 数では Next.js が上だが、本件は「Go バイナリが `dist/` を静的配信し SPA fallback する」構成。Next.js/React Router Framework Mode は Node ランタイム (SSR/RSC/loader) を前提にした価値が中心で、`output: 'export'` 等で静的化すると利点の大半を捨てて制約だけ残る。Vite は `build.outDir` の既定が `dist`、SPA 用途で余計なランタイムがなく、Go 側は `index.html` フォールバックだけ実装すればよい。React Router v8 の Framework Mode は Vite 7+ を要求するが、Data Mode はただのライブラリとして使える (remix.run/blog/react-router-v8)。

Context7 (`/vitejs/vite`) で確認した設定:

```ts
// vite.config.ts
import path from "node:path";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: { alias: { "@": path.resolve(__dirname, "./src") } },
  server: {
    proxy: {
      // /api と /auth を Go backend へ (docs/config/server-options.md)
      "/api": { target: "http://localhost:8080", changeOrigin: true },
      "/auth": { target: "http://localhost:8080", changeOrigin: true },
    },
  },
  build: { outDir: "dist", emptyOutDir: true }, // 既定 outDir は 'dist'
});
```

`server.proxy` は「キーで始まるパスを target に転送し、Vite の変換対象外にする」と文書化されている。`build.outDir` を Go の embed ディレクトリ (例 `../internal/web/dist`) に向ける場合、root 外なので `emptyOutDir: true` を明示する必要がある (docs/config/build-options.md)。

## 2. 言語: TypeScript

- microsoft/TypeScript: 110,996 stars, **Apache-2.0**, npm 最新 **7.0.2**。
- 7.0 は 2026-07-08 に GA した Go 製ネイティブコンパイラ版 (devblogs.microsoft.com/typescript/announcing-typescript-7-0/)。安定した programmatic API は 7.1 予定。Vite は esbuild/oxc でトランスパイルするため型チェックは `tsc --noEmit` を別途 CI で実行する。

## 3. ルーティング

| 候補 | Stars | License | 最新 |
|---|---|---|---|
| remix-run/react-router | 56,572 | MIT | 8.3.1 (2026-06-17) |
| TanStack/router | 15,071 | MIT | 1.170.35 |

**推奨: react-router v8 Data Mode** (Star 数優位、ESM-only、React 19.2.7+ / Node 22.22+ 必須)。Context7 `/websites/reactrouter`:

```tsx
import { createBrowserRouter, redirect, Outlet } from "react-router";
import { RouterProvider } from "react-router/dom";

const router = createBrowserRouter([
  { path: "/login", Component: Login },
  {
    // 保護レイアウト: loader で /api/me を確認し未ログインなら redirect
    loader: async () => {
      const res = await fetch("/api/me", { credentials: "include" });
      if (res.status === 401) throw redirect("/login");
      return res.json();
    },
    Component: () => <Outlet />,
    children: [
      { index: true, Component: Home },
      { path: "events", Component: EventList },
      { path: "events/:id", Component: EventEdit },
      { path: "connections", Component: Connections },
    ],
  },
]);

ReactDOM.createRoot(root).render(<RouterProvider router={router} />);
```

「loader から `throw redirect("/login")`」は公式の認証チェック例そのもの (reactrouter.com/api/utils/redirect)。TanStack Router は `beforeLoad` + `throw redirect({to:'/login'})` で同等 (Context7 `/tanstack/router` docs/router/how-to/setup-authentication.md) で型安全性は高いが Star 数で劣る。

## 4. サーバー状態: @tanstack/react-query v5

50,278 stars, MIT, 5.102.8。Context7 `/tanstack/query` (docs/framework/react/quick-start.md):

```tsx
const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: (n, e) => !(e instanceof UnauthorizedError) && n < 2 } },
});
<QueryClientProvider client={queryClient}><App /></QueryClientProvider>

const events = useQuery({ queryKey: ["events"], queryFn: listEvents });
const update = useMutation({
  mutationFn: updateEvent,
  onSuccess: () => queryClient.invalidateQueries({ queryKey: ["events"] }),
});
```

## 5. UI

| 候補 | Stars | License |
|---|---|---|
| shadcn-ui/ui | 123,565 | MIT |
| tailwindlabs/tailwindcss | 97,499 | MIT |
| ant-design/ant-design | 99,480 | MIT |
| mui/material-ui | 99,028 | MIT |
| mantinedev/mantine | 31,697 | MIT |
| radix-ui/primitives | 19,260 | MIT |

**推奨: Tailwind CSS v4 + shadcn/ui**。shadcn が最多 Star、コンポーネントはソースとしてコピーされるため依存ロックインが薄く、管理画面の Table/Dialog/Form が揃う。Context7 `/tailwindlabs/tailwindcss.com`, `/shadcn-ui/ui` と ui.shadcn.com/docs/installation/vite で確認:

```bash
pnpm add tailwindcss @tailwindcss/vite
pnpm add -D @types/node
pnpm dlx shadcn@latest init          # components.json を生成
pnpm dlx shadcn@latest add button table dialog form
```

```css
/* src/index.css */
@import "tailwindcss";
```

```json
// tsconfig.json / tsconfig.app.json
{ "compilerOptions": { "baseUrl": ".", "paths": { "@/*": ["./src/*"] } } }
```

`components.json` は `"$schema": "https://ui.shadcn.com/schema.json"`, `"style"`, `"tailwind"`, `"aliases": { "components": "@/components", "utils": "@/lib/utils", "ui": "@/components/ui", ... }` の形式。vite.config.ts は 1 節のものと同一 (公式が `react()` + `tailwindcss()` + `@` alias を示している)。

## 6. フォーム: react-hook-form + zod

react-hook-form 44,852 / zod 43,927、ともに MIT。Context7 `/react-hook-form/resolvers`:

```tsx
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";

const schema = z.object({ title: z.string().min(1), startsAt: z.string() });
const form = useForm({ resolver: zodResolver(schema) }); // 型は schema から推論
<form onSubmit={form.handleSubmit(onSubmit)}>
  <input {...form.register("title")} />
  {form.formState.errors.title?.message}
</form>
```

`transform` で入出力型が変わる場合は `useForm<z.input<S>, unknown, z.output<S>>` の 3 ジェネリクス指定が必要 (公式 configuration.md)。zod 4 (`zod` / `zod/v4`) に対応済み。

## 7. API クライアント: openapi-typescript + openapi-fetch

monorepo 8,363 stars, MIT。Context7 `/openapi-ts/openapi-typescript`, `/websites/openapi-ts_dev`:

```bash
pnpm add openapi-fetch
pnpm add -D openapi-typescript
npx openapi-typescript ../api/openapi.yaml -o ./src/lib/api/v1.d.ts   # YAML 直接入力可
```

```ts
import createClient, { type Middleware } from "openapi-fetch";
import type { paths } from "@/lib/api/v1";

// createClient は baseUrl 以外に「任意の fetch オプション (headers, mode, cache, signal …)」を既定値として受け取る
export const api = createClient<paths>({ baseUrl: "/api", credentials: "include" });

const on401: Middleware = {
  async onResponse({ response }) {
    if (response.status === 401) window.location.assign("/login");
    return response;
  },
};
api.use(on401);

const { data, error } = await api.GET("/events/{id}", { params: { path: { id } } });
```

公式は `noUncheckedIndexedAccess` の有効化と CI での `tsc --noEmit` を推奨。

## 8. テスト

| 候補 | Stars | License | 最新 |
|---|---|---|---|
| vitest-dev/vitest | 17,090 | MIT | 5.0.0 |
| testing-library/react-testing-library | 19,650 | MIT | 16.3.3 |
| microsoft/playwright | 95,963 | Apache-2.0 | 1.63.0 |

Context7 `/vitest-dev/vitest`, `/testing-library/testing-library-docs`:

```ts
// vitest.config.ts
import { defineConfig } from "vitest/config";
export default defineConfig({
  test: { environment: "jsdom", globals: true, setupFiles: ["./src/test/setup.ts"] },
});
// src/test/setup.ts
import "@testing-library/jest-dom/vitest";
```

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

test("shows greeting", async () => {
  render(<Welcome firstName="John" lastName="Doe" />);
  expect(screen.getByRole("heading")).toHaveTextContent("Welcome, John Doe");
});
```

E2E は Playwright (`@playwright/test`) で Google ログインはモック/セッション Cookie 注入で回避する。

## 9. Lint / Format

| 候補 | Stars | License | 最新 |
|---|---|---|---|
| eslint/eslint | 27,497 | MIT | 10.10.0 (v10: 2026-02-06) |
| typescript-eslint | 16,387 | MIT | 8.70.0 |
| prettier/prettier | 52,245 | MIT | 3.9.6 |
| biomejs/biome | 25,759 | Apache-2.0 (npm: MIT OR Apache-2.0) | 2.5.13 |

**推奨: ESLint (flat config) + typescript-eslint + Prettier**。Star 合計・型情報 lint (`recommendedTypeChecked`)・eslint-plugin-react-hooks 等のエコシステムで優位。Biome は高速で単一ツールだが、型付き lint とプラグイン互換が弱い。Context7 `/typescript-eslint/typescript-eslint`:

```js
// eslint.config.mjs
import js from "@eslint/js";
import { defineConfig } from "eslint/config";
import tseslint from "typescript-eslint";
export default defineConfig({
  files: ["**/*.{ts,tsx}"],
  extends: [js.configs.recommended, tseslint.configs.recommendedTypeChecked],
  languageOptions: { parserOptions: { projectService: true } },
});
```

`tseslint.config()` は非推奨となり ESLint core の `defineConfig()` が推奨。

## 10. パッケージマネージャ

- pnpm/pnpm: 36,487 stars, **MIT**, 12.3.4
- npm/cli: 10,109 stars, **Artistic-2.0** (GitHub spdx は NOASSERTION、npm `license` は Artistic-2.0)

**推奨: pnpm**。Star 数で上回り MIT。npm CLI は Artistic-2.0 で選定ルール (MIT/Apache-2.0) から外れる (実運用で問題になる license ではないが、ルール準拠のため)。`packageManager` フィールドで固定し corepack で配布。

## 11. 日付・タイムゾーン

| 候補 | Stars | License | 最新 |
|---|---|---|---|
| iamkun/dayjs | 48,665 | MIT | 1.11.23 |
| date-fns/date-fns | 36,646 | MIT (npm) | 4.4.0 |
| date-fns/tz | 268 | MIT | 1.5.0 |
| moment/luxon | 16,457 | MIT | 3.7.2 |
| fullcalendar/temporal-polyfill | 773 | MIT (npm) | 1.0.4 |
| js-temporal/temporal-polyfill | 788 | ISC | 0.5.1 |

**推奨: date-fns v4 + @date-fns/tz**。dayjs の方が Star は多いが、TZ 対応は plugin (`utc`+`timezone`) 依存で `Date` 互換性が薄く tree-shaking も効かない。date-fns v4 は first-class TZ サポートを公式に持ち (`in` オプション / `TZDate`)、shadcn/ui の Calendar 系も date-fns 前提。Temporal polyfill は API は理想的だがまだ提案段階・ISC/小規模。Context7 `/date-fns/tz`:

```ts
import { TZDate, tz } from "@date-fns/tz";
import { format, addDays } from "date-fns";

const tokyo = TZDate.tz("Asia/Tokyo", "2026-09-12T00:00:00Z");
format(addDays(tokyo, 1), "yyyy-MM-dd HH:mm");           // TZ を保持したまま演算
format(new Date(isoFromApi), "M/d HH:mm", { in: tz("Asia/Tokyo") });
```

API は UTC ISO 8601 で受け渡し、表示時のみ `Asia/Tokyo` に変換する。

---

## 認証パターン (HttpOnly セッション Cookie、JS にトークンを持たない)

**フロー**
1. SPA の「Google でログイン」ボタンは `<a href="/auth/google/login">` (トップレベルナビゲーション)。Go が `state` (CSRF 用 nonce、短命 Cookie に保存) と PKCE を生成し Google へ 302。
2. `/auth/google/callback?code&state` で Go が state 検証・code 交換・ID token 検証、許可ドメイン/メールで管理者判定し、サーバー側セッションを作って `Set-Cookie: session=<opaque id>; HttpOnly; Secure; SameSite=Lax; Path=/` を返し、`/` (または保存した戻り先) へ 302。
3. SPA 起動時とルート遷移時に `GET /api/me` を呼ぶ (react-router の保護レイアウトの loader、または react-query `useQuery(["me"])`)。200 ならユーザー情報、401 なら未ログイン。
4. ログアウトは `POST /auth/logout` でセッション削除 + Cookie 失効。

**CSRF (OWASP CSRF Prevention Cheat Sheet に基づく)**
- `SameSite=Lax` は「安全でないメソッド (POST 等) のクロスサイト送信をブロックする」が、OWASP は defense-in-depth 扱いで、単独でトークン方式の代替にはならないとしている。`Strict` だと Google からの callback 302 直後に Cookie が送られず不便なので `Lax` が実用的。
- 主防御はカスタムヘッダー方式: openapi-fetch の `createClient` 既定 `headers: { "X-Requested-With": "aso-bell" }` を付け、Go 側で状態変更メソッド (POST/PUT/PATCH/DELETE) はこのヘッダーがなければ 403。カスタムヘッダーはクロスオリジンでは CORS preflight を要求するため、CORS を許可しない限り攻撃者は付与できない (OWASP は SPA/AJAX 向けに明示推奨)。
- 加えて `Origin` / `Sec-Fetch-Site` を検査し、`cross-site` の非安全メソッドを拒否 (Fetch Metadata)。より厳格にするなら Signed Double-Submit Cookie (HMAC でセッション ID に紐付けたトークンを `X-CSRF-Token` で送る) を採用。
- CORS は有効化しない (同一オリジン配信なので不要)。`credentials: "include"` は同一オリジンでは実質 `same-origin` と同じだが、dev proxy 経由・将来の別ホスト化に備えて明示。
- XSS は全 CSRF 対策を無効化するため、`dangerouslySetInnerHTML` 禁止・CSP 設定を Go 側で行う。

**401 の扱い**
- openapi-fetch の `onResponse` middleware で `401` を検出し `/login?redirect=<現在の path>` へ遷移 (7 節)。react-query では 401 を `retry` 対象から除外し、`queryClient.clear()` でキャッシュを破棄。
- 保護レイアウトの loader で `/api/me` が 401 なら `throw redirect("/login")` (3 節) にすることで初回表示時のちらつきを防ぐ。
- Go 側の SPA fallback: `/api/*`, `/auth/*` 以外の GET で存在しないパスは `index.html` を返す。`/api/*` の未認証は HTML ではなく必ず JSON + 401 を返す (SPA が判別できるように)。

---

## 出典

- GitHub API (`gh api repos/...`): 上記各リポジトリの `stargazers_count`, `license.spdx_id` (2026-09-11)
- npm registry `https://registry.npmjs.org/<pkg>/latest` (version / license)
- Context7: `/vitejs/vite`, `/websites/reactrouter`, `/tanstack/query`, `/tanstack/router`, `/tailwindlabs/tailwindcss.com`, `/shadcn-ui/ui`, `/react-hook-form/resolvers`, `/openapi-ts/openapi-typescript`, `/websites/openapi-ts_dev`, `/vitest-dev/vitest`, `/testing-library/testing-library-docs`, `/typescript-eslint/typescript-eslint`, `/date-fns/tz`
- https://reactrouter.com/start/modes (3 モードの説明、v8.3.1)
- https://remix.run/blog/react-router-v8 (2026-06-17、ESM-only、Node 22.22+/React 19.2.7+、Data Mode 継続)
- https://ui.shadcn.com/docs/installation/vite
- https://devblogs.microsoft.com/typescript/announcing-typescript-7-0/ 、https://www.infoq.com/news/2026/08/typescript-7-released/
- https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html

備考: date-fns/date-fns と fullcalendar/temporal-polyfill は GitHub API の `license` が空/未検出だったため npm の `license: MIT` を採用。npm/cli は GitHub 上 NOASSERTION、npm 上 Artistic-2.0。
