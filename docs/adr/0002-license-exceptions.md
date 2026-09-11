# ADR 0002: BSD ライセンスのライブラリを例外として採用する

- 状態: 採用(2026-09-11 ユーザー承認: BSD 系ライセンスを許容、golangci-lint の開発時利用を許容)
- 日付: 2026-09-11

## 文脈

要件は「ライセンスは Apache もしくは MIT(特に GPL などコピーレフトが強いライセンスは避ける)」。Go の Slack / Discord ライブラリで最も利用されているものは BSD ライセンスである。

| ライブラリ | ライセンス | Stars | 代替 |
| --- | --- | ---: | --- |
| `slack-go/slack` | BSD-2-Clause | 4,959 | なし(公式 Go SDK は存在せず、MIT の `slack-io/slacker` はこのライブラリのラッパーで 2024-11 以降更新なし) |
| `bwmarrin/discordgo` | BSD-3-Clause | 5,985 | `disgoorg/disgo`(Apache-2.0, 607★, 保守活発) |
| `golang.org/x/oauth2` | BSD-3-Clause | 5,898 | なし(`go-oidc` の必須依存、Go 公式) |
| `google.golang.org/protobuf` | BSD-3-Clause | 3,349 | なし(`grpc-go` の必須依存、Google 公式) |
| `grpc-ecosystem/grpc-gateway` | BSD-3-Clause | 20,002 | なし(要件で指定) |

## 決定

BSD-2/3-Clause は MIT と同種の許諾型ライセンスであり、括弧書きの意図(コピーレフト回避)に反しないと解釈して例外採用する。ユーザーは BSD を許容すると決定した。Discord は Star 数を優先して discordgo を選ぶ。将来 discordgo の保守が問題になれば disgo(Apache-2.0)へ切り替える(Adapter の差し替えで 1〜2 人日)。

## 結果

- `go.mod` と `THIRD_PARTY_LICENSES.md` に例外を明記する
- 開発時のみ使うツール(`golangci-lint` GPL-3.0)は成果物に含まれないため許容する(ユーザー承認済み)
