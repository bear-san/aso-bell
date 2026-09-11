# ADR 0006: WebConsole は Vite + React の SPA とし Go バイナリに埋め込む

- 状態: 採用
- 日付: 2026-09-11

## 文脈

Star 数では Next.js(142k)が Vite(83k)を上回る。しかし WebConsole は Go バイナリが配信する管理画面であり、SSR や RSC は不要。

## 決定

Vite + React 19 + TypeScript の SPA を `web/` に置き、ビルド成果物を `embed` して Go から配信する。ルーティングは react-router(Data Mode)。

## 理由

- Node ランタイムを本番に持ち込まず、デプロイ物が Go バイナリ 1 つに収まる
- Next.js を静的エクスポートすると利点(SSR、Route Handlers、Image 最適化)の大半が使えず、制約だけが残る
- react-router の loader で `/api/me` を確認し `redirect("/login")` する保護ルートが公式パターンとして確立している

## 結果

- SEO や初期表示速度は考慮しない(ログイン必須の管理画面)
- 開発時は Vite dev server のプロキシで Go API に接続する
