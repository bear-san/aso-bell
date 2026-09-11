# ADR 0010: すべての API を gRPC で定義し、フロントエンド向け REST は grpc-gateway で導出する

- 状態: 採用
- 日付: 2026-09-11

## 文脈

要件変更により、フロントエンド(WebConsole)向けを含むすべての API を「gRPC + grpc-gateway による REST API」として定義することになった。あわせて認証は Manager の責務とし、フロントエンドは Cookie にトークンを保持するだけとする。従来案は手書きの OpenAPI 3.1 + oapi-codegen + gin だった。

## 決定

1. Manager の API はすべて `proto/asobell/v1/*.proto` の gRPC サービスとして定義する。Provider 向け(`ManagerService`)と WebConsole 向け(`EventService`, `WorkspaceService`, `AccountService`, `ProviderStatusService`)を同じ gRPC サーバーで提供する
2. WebConsole 向けサービスには `google.api.http` アノテーションを付け、grpc-gateway が `/api/v1/...` の REST(JSON)へ変換する。ゲートウェイは同一プロセス内で loopback の gRPC サーバーへ接続する
3. OpenAPI 文書と TypeScript 型は proto から生成する。手書きの OpenAPI、oapi-codegen、gin は廃止する
4. 入力制約は `buf.validate`(protovalidate)で proto に記述し、gRPC インターセプタで検証する
5. 認証は gRPC の認証インターセプタに集約する。ゲートウェイがセッション Cookie を gRPC メタデータへ転写し、インターセプタがサービス名に応じて RPC トークン(Provider)またはセッショントークン(Console)を検証する。フロントエンドはトークンの値に触れない
6. Google OIDC のリダイレクトフロー、ヘルスチェック、SPA 配信のみ `net/http` のハンドラとしてゲートウェイと同じ mux に載せる

## 理由

- API の契約が proto 1 種類に統一され、Provider 向けと WebConsole 向けで生成・検証・エラー表現の仕組みを共有できる
- 認証・バリデーション・エラー変換がインターセプタに集約され、REST と gRPC のどちらから来ても同じ挙動になる
- ゲートウェイを loopback 接続にすることで、`RegisterXxxHandlerServer`(直接呼び出し)がインターセプタを迂回する問題を避ける

## 結果

- JSON 表現は protojson に従う(Duration は `"604800s"`、enum は名前、int64 は文字列)。フロントエンドで表示用の変換が必要
- エラーは RFC 9457 ではなく `google.rpc.Status`(`code`, `message`, `details`)になる
- OpenAPI 生成器(gnostic)は `oneof` / `optional` の nullable を表現しない。2026-09-11 に `buf generate` を試行し、パス・enum・Duration/Timestamp の出力は期待どおりであることを確認した
- grpc-gateway は BSD-3-Clause(ADR 0002 の例外に追加)
- protovalidate のミドルウェアは `buf.validate.Violations` を返すため、`google.rpc.BadRequest` へ変換する薄いインターセプタを自作する
- `google.api` と `buf.validate` の proto を `buf.yaml` の依存として取得する(BSR)
