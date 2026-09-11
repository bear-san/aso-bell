<!-- 調査エージェントによる裏取りレポート(自動抽出)。設計本文は docs/16-grpc.md / docs/03-tech-stack.md を参照 -->

# Go gRPC ツールチェーン調査レポート（Manager ⇄ Provider 構成向け）

調査日: 2026-09-11。★数・ライセンスは `gh api repos/OWNER/REPO`（`stargazers_count` / `license.spdx_id`）、最新版は同 API の `releases/latest` と `proxy.golang.org/.../@latest` で確認。

## サマリ

| カテゴリ | 採用 | Stars | SPDX | 最新版（日付） |
|---|---|---|---|---|
| gRPC ランタイム | **google.golang.org/grpc** (grpc/grpc-go) | 23,052 | Apache-2.0 | v1.83.2 (2026-08-25) |
| （比較）| connectrpc.com/connect (connectrpc/connect-go) | 4,067 | Apache-2.0 | v1.21.0 (2026-09-08) |
| Protobuf ランタイム / protoc-gen-go | **google.golang.org/protobuf** (protocolbuffers/protobuf-go) | 3,349 | **BSD-3-Clause（例外扱い・要フラグ）** | v1.36.12 (2026-08-10) |
| protoc-gen-go-grpc | google.golang.org/grpc/cmd/protoc-gen-go-grpc | (grpc-go 同梱) | Apache-2.0 | v1.6.2 (2026-05-07, proxy.golang.org) |
| コード生成・Lint | **bufbuild/buf** | 11,429 | Apache-2.0 | v1.73.0 (2026-09-11) |
| インターセプタ集 | github.com/grpc-ecosystem/go-grpc-middleware/v2 | 6,762 | Apache-2.0 | v2.3.4 (2026-08-28) |

判定: 星数・ライセンスの両基準で grpc-go / buf を採用。protobuf-go は代替不能（grpc-go が依存）だが BSD-3-Clause のため「許容例外」として明記する。

Context7 ID: `/grpc/grpc-go`, `/websites/buf_build`, `/bufbuild/buf`, `/protocolbuffers/protobuf-go`

---

## 1. gRPC ランタイム: grpc-go vs connect-go

**connect-go** は「slim library for building browser and gRPC-compatible HTTP APIs」で、Connect / gRPC / gRPC-Web の 3 プロトコルを `net/http` の `http.Handler` / `http.Client` 上で実装する（README: "Connect is just Protocol Buffers and the standard library"）。ブラウザ直結や HTTP ミドルウェア資産を活かす場合は魅力的だが、本件はコンテナ間の純 gRPC 通信であり、星数（23k vs 4k）・keepalive/health/retry/reflection といった純正機能の充実度から **grpc-go を採用**。

### 1.1 検証済み API（grpc-go v1.83.2、pkg.go.dev）

- `grpc.Dial` / `DialContext` は **非推奨確定**: pkg.go.dev に "Deprecated: use NewClient instead. Will be supported throughout 1.x." と記載。
- `grpc.NewClient(target, opts...)`: "creates a new gRPC 'channel' for the target URI provided. No I/O is performed." スキーム未指定時は `dns` リゾルバ。
- anti-patterns.md: "`grpc.Dial` uses 'passthrough' as the default name resolver ... while `grpc.NewClient` uses 'dns'"。`WithBlock` は使わず、個々の RPC に `WaitForReady(true)` を付ける方が粒度が細かい、と明記。
- `WaitForReady`: "configures the RPC's behavior when the client is in TRANSIENT_FAILURE ... By default, RPCs do not 'wait for ready'."

```go
// クライアント
conn, err := grpc.NewClient("dns:///manager:9090",
    grpc.WithTransportCredentials(insecure.NewCredentials()), // 開発時
    // 本番: grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{...}))
    grpc.WithDefaultServiceConfig(retryPolicy),
    grpc.WithKeepaliveParams(keepalive.ClientParameters{
        Time: 60 * time.Second, Timeout: 20 * time.Second, PermitWithoutStream: true,
    }),
    grpc.WithChainUnaryInterceptor(bearerTokenInjector(token)),
)
resp, err := client.Foo(ctx, req, grpc.WaitForReady(true))

// サーバ
srv := grpc.NewServer(
    grpc.Creds(credentials.NewTLS(tlsCfg)),          // 開発時は省略で平文
    grpc.ChainUnaryInterceptor(recoveryUnary, authUnary, loggingUnary),
    grpc.ChainStreamInterceptor(recoveryStream, authStream),
    grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
        MinTime: 30 * time.Second, PermitWithoutStream: true,
    }),
    grpc.KeepaliveParams(keepalive.ServerParameters{
        MaxConnectionIdle: 5 * time.Minute, Time: 2 * time.Minute, Timeout: 20 * time.Second,
    }),
)
pb.RegisterProviderServiceServer(srv, impl)
reflection.Register(srv)                              // google.golang.org/grpc/reflection
go func() { <-ctx.Done(); srv.GracefulStop() }()
if err := srv.Serve(lis); err != nil { ... }
```

`grpc.UnaryInterceptor` / `grpc.StreamInterceptor` は単一登録、`ChainUnaryInterceptor` / `ChainStreamInterceptor` は複数（クライアント側は `WithUnaryInterceptor` / `WithChainUnaryInterceptor`）。4 種類（unary/stream × client/server）が存在（examples/features/interceptor/README.md）。

### 1.2 Bearer トークン認証（metadata）

grpc-metadata.md: キーは自動で小文字化、バイナリは `-bin` サフィックスで base64 化。

```go
// クライアント側インターセプタ
func bearerTokenInjector(token string) grpc.UnaryClientInterceptor {
    return func(ctx context.Context, method string, req, reply any,
        cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
        ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
        return invoker(ctx, method, req, reply, cc, opts...)
    }
}

// サーバ側インターセプタ
func authUnary(ctx context.Context, req any, info *grpc.UnaryServerInfo,
    handler grpc.UnaryHandler) (any, error) {
    md, ok := metadata.FromIncomingContext(ctx)
    if !ok { return nil, status.Error(codes.Unauthenticated, "missing metadata") }
    vals := md.Get("authorization")
    if len(vals) == 0 || !validBearer(vals[0]) {
        return nil, status.Error(codes.Unauthenticated, "invalid token")
    }
    return handler(ctx, req)
}
// ストリームでは ss.Context() から FromIncomingContext する
```

`credentials.PerRPCCredentials`（`GetRequestMetadata` / `RequireTransportSecurity`）を実装して `grpc.WithPerRPCCredentials` で渡す方式も公式（credentials/credentials.go）。

### 1.3 エラーコード対応表

| 状況 | code |
|---|---|
| 認証失敗 | `codes.Unauthenticated` |
| 権限不足 | `codes.PermissionDenied` |
| リソース未存在 | `codes.NotFound` |
| レート制限・容量超過 | `codes.ResourceExhausted` |
| 一時障害（リトライ可） | `codes.Unavailable` |
| 期限超過 | `codes.DeadlineExceeded`（ctx の deadline で自動付与） |
| 状態不整合（前提未達） | `codes.FailedPrecondition` |
| 引数不正 | `codes.InvalidArgument` |

リッチエラー（examples/features/error_details/server/main.go, `google.golang.org/genproto/googleapis/rpc/errdetails`）:

```go
st := status.New(codes.ResourceExhausted, "Request limit exceeded.")
ds, err := st.WithDetails(&errdetails.QuotaFailure{
    Violations: []*errdetails.QuotaFailure_Violation{{
        Subject: "name:" + in.Name, Description: "Limit one greeting per person",
    }},
})
if err != nil { return nil, st.Err() }
return nil, ds.Err()
```

クライアント側は `status.Convert(err).Details()` で取り出す。

### 1.4 Keepalive（pkg.go.dev/google.golang.org/grpc/keepalive）

- `ClientParameters.Time`: 最小 10s（未満は切り上げ）。`Timeout` 既定 20s。`PermitWithoutStream=false` なら RPC 無し時は ping しない。
- `ServerParameters.MaxConnectionIdle` / `MaxConnectionAge`（±10% ジッタ付与）/ `MaxConnectionAgeGrace`: 既定は無限。`Time` 既定 2h。
- `EnforcementPolicy.MinTime` 既定 **5 分**、`PermitWithoutStream` 既定 false → クライアント `Time` をこれより短くすると GOAWAY (`too_many_pings`) で切断されるため、**両側で整合させる**。
- grpc.io keepalive ガイド: "recommended to avoid enabling keepalive without calls and for clients to avoid configuring their keepalive much below one minute."

### 1.5 Health checking

```go
import (
    "google.golang.org/grpc/health"
    healthpb "google.golang.org/grpc/health/grpc_health_v1"
)
hs := health.NewServer()
healthpb.RegisterHealthServer(srv, hs)
hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)          // 全体
hs.SetServingStatus("asobell.v1.ProviderService", healthpb.HealthCheckResponse_SERVING)
```

クライアント側透過チェックは `import _ "google.golang.org/grpc/health"` + service config の `"healthCheckConfig": {"serviceName": ""}`（examples/features/health/README.md）。Compose の `healthcheck` には `grpc_health_probe` が使える。

### 1.6 Service config リトライ（examples/features/retry/README.md）

```go
const retryPolicy = `{
  "methodConfig": [{
    "name": [{"service": "asobell.v1.ProviderService"}],
    "waitForReady": true,
    "retryPolicy": {
      "MaxAttempts": 4,
      "InitialBackoff": "0.1s",
      "MaxBackoff": "1s",
      "BackoffMultiplier": 2.0,
      "RetryableStatusCodes": ["UNAVAILABLE"]
    }
  }]
}`
conn, _ := grpc.NewClient(target, grpc.WithDefaultServiceConfig(retryPolicy), ...)
```

注意: リトライは冪等な RPC にのみ許可する設計にする（`name` をメソッド単位に絞れる）。

### 1.7 bufconn によるインプロセステスト

`grpc.NewClient` は既定が dns リゾルバなので **`passthrough:///` を明示**しないとカスタムダイヤラに到達しない（grpc-go issue #7343、`WithContextDialer` doc）。

```go
func newTestClient(t *testing.T) pb.ProviderServiceClient {
    lis := bufconn.Listen(1 << 20)
    srv := grpc.NewServer()
    pb.RegisterProviderServiceServer(srv, &fakeProvider{})
    go srv.Serve(lis)
    t.Cleanup(srv.GracefulStop)

    conn, err := grpc.NewClient("passthrough:///bufnet",
        grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
            return lis.DialContext(ctx)
        }),
        grpc.WithTransportCredentials(insecure.NewCredentials()),
    )
    if err != nil { t.Fatal(err) }
    t.Cleanup(func() { conn.Close() })
    return pb.NewProviderServiceClient(conn)
}
```

---

## 2. Protobuf（protobuf-go v1.36.12 / protoc-gen-go v1.36.12 / protoc-gen-go-grpc v1.6.2）

- Well-known types: `google/protobuf/timestamp.proto`, `duration.proto`, `empty.proto`, `struct.proto` を import。Go 側は `timestamppb.New(t)` / `timestamppb.Now()` / `.AsTime()`、`durationpb.New(d)` / `.AsDuration()`、`emptypb.Empty`、`structpb.NewStruct(map[string]any)`。`CheckValid()` で範囲検証可。
- **oneof**: 親メッセージに `isMsg_Field` インターフェース型のフィールド 1 つ + 各バリアントのラッパ struct（`*Msg_Variant{...}`）が生成される。読み出しは型 switch: `switch x := m.Payload.(type) { case *Event_Started: ... }`。
- **proto3 `optional`**: スカラは **ポインタ型**（例 `*int32`）で生成され、nil で「未設定」を判別。protoc-gen-go v1.36 系では標準対応（`--experimental_allow_proto3_optional` 不要）。
- enum 命名（protobuf.dev style guide + buf `ENUM_ZERO_VALUE_SUFFIX`/`ENUM_VALUE_PREFIX`）:

```proto
enum ProviderState {
  PROVIDER_STATE_UNSPECIFIED = 0;
  PROVIDER_STATE_READY = 1;
  PROVIDER_STATE_BUSY = 2;
}
```

- `go_package` の `;` 形式は protobuf.dev で "discouraged"（パスから導出可能）だが、buf と組み合わせて `asobellv1` のような別名を固定したい場合の慣用であり、buf の公式チュートリアルでも使われる。

---

## 3. コード生成 & Lint: buf v1.73.0

ディレクトリ:
```
proto/asobell/v1/manager.proto
proto/asobell/v1/provider.proto
gen/asobell/v1/*.pb.go, *_grpc.pb.go
```

`buf.yaml`（v2, buf.build/docs/breaking/quickstart で確認）:
```yaml
version: v2
modules:
  - path: proto
lint:
  use:
    - STANDARD
breaking:
  use:
    - FILE
```

`buf.gen.yaml`（v2, buf.build/docs/bsr/remote-plugins）:
```yaml
version: v2
plugins:
  - remote: buf.build/protocolbuffers/go:v1.36.12
    out: gen
    opt: paths=source_relative
  - remote: buf.build/grpc/go:v1.6.2
    out: gen
    opt: paths=source_relative
# オフライン運用なら:
#  - local: protoc-gen-go
#    out: gen
#    opt: paths=source_relative
```

proto ヘッダ:
```proto
syntax = "proto3";
package asobell.v1;
option go_package = "github.com/x/aso-bell/gen/asobell/v1;asobellv1";
```

コマンド: `buf generate` / `buf lint` / `buf breaking --against '.git#branch=main'`（CI では `--against "https://github.com/<org>/<repo>.git#branch=main"`）。

STANDARD lint が強制する設計規約（buf.build/docs/lint/rules）:
- `SERVICE_SUFFIX`: サービス名は `Service` で終わる（`ProviderService`）。
- `RPC_REQUEST_STANDARD_NAME` / `RPC_RESPONSE_STANDARD_NAME`: `MethodNameRequest` / `MethodNameResponse`。
- `RPC_REQUEST_RESPONSE_UNIQUE`: リクエスト/レスポンス型は RPC 間で共有しない（`Empty` の直接使用も NG → `PingRequest{}` / `PingResponse{}` を作る）。
- `PACKAGE_VERSION_SUFFIX`: `asobell.v1` / `v2alpha1` 等。`PACKAGE_DIRECTORY_MATCH`: パッケージとディレクトリ一致。
- `ENUM_ZERO_VALUE_SUFFIX`: `_UNSPECIFIED = 0`。

ページネーション（google.aip.dev/158）: リクエストに `int32 page_size`（省略可、負値は `INVALID_ARGUMENT`、上限超過は丸める）と `string page_token`（省略可）、レスポンスは繰り返しフィールドを field 1 に、`string next_page_token`（末尾なら空文字列）。任意で `int32 total_size`。ページネーションは後付けが破壊的変更になるため最初から入れる。

---

## 4. 双方向パターン比較（Manager ⇄ Provider）

**(a) 両側が gRPC サーバを持ち相互 dial**
- 各方向が通常の unary RPC で済み、リトライ/deadline/service config/health/reflection の純正機能をそのまま使える。
- 課題: Provider が Manager から到達可能である必要（Compose では service DNS で解決可、問題なし）。Provider 側にも TLS/認証設定が要る。

**(b) 単一サーバ + 長寿命 bidi ストリームで逆向き要求を運ぶ**
- grpc.io core-concepts: "the two streams are independent, so clients and servers can read and write in whatever order they like"、順序は各方向で保持。Provider が NAT 越し・inbound 不可の場合に有効。
- 課題: 逆向き RPC の相関 ID・タイムアウト・リトライを自前実装。grpc.io performance ガイド "streams ... sacrifice load balancing after startup"。keepalive で切断検知が必須（`ClientParameters.Time` ≥ 1 分推奨、`EnforcementPolicy.MinTime` と整合）。サーバ `MaxConnectionAge` を設定すると GOAWAY でストリームが切れるため再接続ロジックが必要（keepalive pkg doc: "closed by sending a GoAway"、±10% ジッタ）。

**推奨**: Compose 内で両者に到達性があるなら **(a)** が実装コスト・運用性で優位。将来 Provider をリモート配置する見込みがあるなら、(b) を Provider→Manager の「登録 + イベント通知」ストリームに限定し、要求/応答は (a) で行うハイブリッドが妥当。

---

## 5. Docker Compose ネットワーク

- docs.docker.com/compose/how-tos/networking: "Each service registers its name with an internal DNS server, so containers can reach each other using the service name directly."；サービス間通信は `CONTAINER_PORT` を使う（`ports:` のホスト側は不要）。
- grpc naming doc（grpc/grpc/doc/naming.md）: `dns:[//authority/]host[:port]`、"If no scheme prefix is specified or the scheme is unknown, the `dns` scheme is used by default."
- grpc-go: `NewClient` の既定リゾルバは `dns`（旧 `Dial` は `passthrough`）。よって **`manager:9090` と `dns:///manager:9090` は `NewClient` では等価**。明示性のため `dns:///manager:9090` を推奨。dns リゾルバは複数 A レコードがあれば全アドレスを取得するので、`replicas` を増やした場合は `"loadBalancingPolicy": "round_robin"` を service config に入れると分散する（既定は pick_first）。

---

## 6. テスト補助・インターセプタライブラリ

- bufconn: §1.7 参照（`passthrough:///` 必須）。
- **go-grpc-middleware/v2**（v2.3.4, Apache-2.0, 6,762★）: `interceptors/auth`（`auth.UnaryServerInterceptor(authFunc)`、`auth.AuthFromMD(ctx, "bearer")` で Bearer 抽出）、`logging`（slog/zap アダプタ）、`recovery`（panic→`codes.Internal`）、`retry`、`timeout`、`ratelimit`、`validator`/`protovalidate`、`selector`（メソッド別に適用可否）。README で `grpc.ChainUnaryInterceptor` による合成例あり。
- 判断: 本件で必要なのは auth / logging / recovery の 3 つで、いずれも 20〜40 行で自作可能。依存を増やしたくなければ手書きで十分だが、**`recovery` と `selector`（health/reflection を認証から除外）** は地味に手間なので、採用しても損はない。Apache-2.0 でライセンス上の問題もない。

---

## 出典

- GitHub API: `gh api repos/{grpc/grpc-go, connectrpc/connect-go, protocolbuffers/protobuf-go, bufbuild/buf, grpc-ecosystem/go-grpc-middleware}`（2026-09-11）
- proxy.golang.org `@latest`: google.golang.org/grpc v1.83.2、google.golang.org/protobuf v1.36.12、google.golang.org/grpc/cmd/protoc-gen-go-grpc v1.6.2、connectrpc.com/connect v1.21.0、go-grpc-middleware/v2 v2.3.4
- https://pkg.go.dev/google.golang.org/grpc（Dial 非推奨文、NewClient、WaitForReady）
- https://pkg.go.dev/google.golang.org/grpc/keepalive
- https://pkg.go.dev/google.golang.org/grpc/test/bufconn
- https://github.com/grpc/grpc-go/blob/master/Documentation/anti-patterns.md, grpc-metadata.md, grpc-auth-support.md, server-reflection-tutorial.md
- https://github.com/grpc/grpc-go/tree/master/examples/features/{interceptor,retry,keepalive,health,error_details,metadata_interceptor}
- https://github.com/grpc/grpc-go/issues/7343（NewClient + WithContextDialer）
- https://github.com/grpc/grpc/blob/master/doc/naming.md
- https://grpc.io/docs/guides/keepalive/ 、 https://grpc.io/docs/guides/performance/ 、 https://grpc.io/docs/what-is-grpc/core-concepts/
- https://buf.build/docs/breaking/quickstart 、 https://buf.build/docs/bsr/remote-plugins 、 https://buf.build/docs/lint/rules/ 、 https://buf.build/docs/bsr/ci-cd/setup
- https://google.aip.dev/158
- https://protobuf.dev/programming-guides/style/ 、 https://protobuf.dev/reference/go/go-generated/
- https://docs.docker.com/compose/how-tos/networking/
- https://github.com/connectrpc/connect-go (README) 、 https://github.com/grpc-ecosystem/go-grpc-middleware (README)
- Context7: `/grpc/grpc-go`, `/websites/buf_build`, `/protocolbuffers/protobuf-go`
