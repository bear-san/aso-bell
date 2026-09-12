<!-- 裏取りレポート。設計本文は docs/06-provider.md §4 / docs/03-tech-stack.md を参照 -->

# チャンネル名の Unicode 正規化(`golang.org/x/text`)

調査日: 2026-09-12。Context7(`/golang/text`)と `golang.org/x/text@v0.41.0` のソース(`unicode/norm/normalize.go`)で確認した内容のみ記載する。

## 1. 必要になった背景

`internal/shared/channelname` は全角英数字・半角カナ・合成済み文字を含むイベント名から Slack / Discord のチャンネル名を作る(→ [06-provider.md](../06-provider.md) §4)。互換分解を伴う **NFKC** が必要で、標準ライブラリには正規化 API が無い。

## 2. 確認した API

```go
import "golang.org/x/text/unicode/norm"

norm.NFKC.String("Ｇｏ　１")  // "Go 1"
```

- `norm.Form` は `NFC` / `NFD` / `NFKC` / `NFKD` の 4 つの定数のみ(`NFC Form = iota` なのでゼロ値は `NFC`)
- `Form` のメソッド: `String(s string) string`、`Bytes(b []byte) []byte`、`IsNormal(b []byte) bool`、`IsNormalString(s string) bool`、`Append` / `AppendString`、`Span` 系、`Reader(io.Reader)` / `Writer(io.Writer)`
- 不正な UTF-8 バイト列は置換されずそのまま通過する。入力の UTF-8 検証は呼び出し側の責任
- `String` は `quickSpan` で正規化済みと判定した入力をそのまま返す(コピーもアロケーションもしない)

## 3. ライセンス

`golang.org/x/text` は **BSD-3-Clause**(Go 公式リポジトリ `github.com/golang/text`、807★)。ADR 0002 の BSD 例外に該当する(→ [03-tech-stack.md](../03-tech-stack.md) §2.1)。grpc-gateway や mongo-driver の推移的依存として既に `go.sum` に入っており、直接依存への昇格のみを行った。

## 4. 実装での使い方

`channelname.Slug` は NFKC → 小文字化 → 英数字と `_` 以外を `-` に置換 → 連続する `-` の畳み込み、の順に処理する。NFKC を先に置くことで、全角英数字が `-` 埋めされずに ASCII へ落ちる。
