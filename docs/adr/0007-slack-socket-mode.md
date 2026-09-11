# ADR 0007: Slack は Socket Mode で接続する

- 状態: 採用
- 日付: 2026-09-11

## 文脈

Slack のイベント・インタラクション受信は HTTP(Events API + Interactivity Request URL)か Socket Mode(アウトバウンド WebSocket)のいずれか。

## 決定

Socket Mode を採用する。

## 理由

- 公開 URL が不要で、自宅サーバーや NAT 内でも動作する
- 署名検証が不要で実装が単純
- 1 ワークスペース向けの自作 App という前提(Marketplace 配布不可の制約は影響しない)

## 結果

- App-Level Token(`xapp-`, `connections:write`)が追加で必要
- 同時接続は 10 まで。複数プロセス化する場合は接続数に注意
- 将来 HTTP 方式へ切り替える場合は、Provider の環境変数に `ASOBELL_SLACK_SIGNING_SECRET` を追加して署名検証を行う
