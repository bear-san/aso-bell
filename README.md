# あそベル (aso-bell)

Slack / Discord で遊びの予定を立ち上げ、連絡用チャンネルと参加者・リマインドを管理する Bot と管理コンソール。Manager と Provider(チャットツール接続)は 1:1 の対として別コンテナで稼働し、gRPC で通信する。

- 設計ドキュメント: [docs/README.md](docs/README.md)
- 開発時の作業指示(コーディング規約・確定事項): [AGENTS.md](AGENTS.md)
- API 契約(gRPC + grpc-gateway、Provider 向け・WebConsole 向けとも): [proto/asobell/v1/](proto/asobell/v1/)

実装は [docs/15-implementation-plan.md](docs/15-implementation-plan.md) のマイルストーンに沿って進める。
