# ADR 0004: Discord ライブラリに discordgo(master 固定)を採用する

- 状態: 採用(ADR 0002 の例外に依存)
- 日付: 2026-09-11

## 文脈

`bwmarrin/discordgo`(5,985★, BSD-3, 最終タグ v0.29.0 2025-05、master 更新 2026-02)と `disgoorg/disgo`(607★, Apache-2.0, 週次更新)を比較した。discordgo v0.29.0 のピン留めは Discord が非推奨化したエンドポイントを使い、master では修正済み。ピン留めに必要な `PIN_MESSAGES`(1<<51)定数はどちらの版にも無い。

## 決定

discordgo を master の特定コミットに `go.mod` で固定して採用する。`PIN_MESSAGES` は自前定義する。

## 理由

- Star 数・利用実績・Context7 でのドキュメント網羅性で優位
- 必要な API(スラッシュコマンド、コンポーネント、チャンネル/overwrite 操作、ピン)は揃っている

## 結果

- `go.mod` の疑似バージョンにコメントで理由を残す
- discordgo の保守が止まった場合は disgo へ移行する。移行範囲は `internal/provider/discord` のみ
