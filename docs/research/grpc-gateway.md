<!-- 調査エージェントによる裏取りレポート(自動抽出)。設計本文は docs/08-api.md / docs/03-tech-stack.md を参照 -->

Everything needed is collected. Here is the report.

---

# grpc-gateway による REST 公開・OpenAPI/TS 型生成・バリデーション — 調査レポート

調査日: 2026-09-11。★数・ライセンスは GitHub API (`/repos/OWNER/REPO`, `/releases/latest`) の当日値。

## サマリ表

| カテゴリ | 採用 | ★ | ライセンス | 最新版 |
|---|---|---|---|---|
| REST ゲートウェイ | `grpc-ecosystem/grpc-gateway` (v2) | 20,002 | **BSD-3-Clause**(要フラグ、許容範囲) | v2.30.0 (2026-08-05) |
| BSR プラグイン | `buf.build/grpc-ecosystem/gateway`, `buf.build/grpc-ecosystem/openapiv2` | — | BSD-3-Clause | v2.30.0(bufbuild/plugins に登録済) |
| OpenAPI 3 生成 | `google/gnostic` `protoc-gen-openapi` / BSR `buf.build/community/google-gnostic-openapi` | 2,300 | Apache-2.0 | v0.7.1 タグ(release は v0.7.0, 2023-10)。BSR は v0.7.0/v0.7.1 |
| Swagger2→OAS3 変換(代替) | `Mermade/oas-kit` (`swagger2openapi`) | 747 | BSD-3-Clause | npm 7.0.8 (2021-07、更新停滞) |
| TS 型生成 | `openapi-ts/openapi-typescript` | 8,363 | MIT | 7.13.0 (2026-02) |
| バリデーション | `bufbuild/protovalidate` / `bufbuild/protovalidate-go` | 1,559 / 488 | Apache-2.0 | v1.2.2 / v1.4.0 (Go module: `buf.build/go/protovalidate`) |
| インターセプタ | `grpc-ecosystem/go-grpc-middleware` v2 | 6,762 | Apache-2.0 | v2.3.4 |
| gRPC | `grpc/grpc-go` | 23,052 | Apache-2.0 | v1.83.2 |
| protojson | `protocolbuffers/protobuf-go` | 3,349 | BSD-3-Clause | v1.36.12 |
| buf CLI | `bufbuild/buf` | 11,429 | Apache-2.0 | v1.73.0 |
| 代替案 | `connectrpc/connect-go` / `connect-es` | 4,067 / 1,808 | Apache-2.0 | v1.21.0 / v2.2.0 |

Context7 ID: `/websites/grpc-ecosystem_github_io_grpc-gateway`(score 86)、`/grpc-ecosystem/grpc-gateway`、`/websites/protovalidate`(score 83.6)、`/bufbuild/protovalidate`、`/grpc/grpc-go`。gnostic は Context7 未収録(GitHub ソースを直接参照)。

---

## 1. grpc-gateway v2

### 1.1 `google.api.http` アノテーション

`google/api/http.proto`(googleapis)の HttpRule ルール:
- パステンプレート `{id}` は URL パスで渡す。`{var}` は `{var=*}` と等価、変数テンプレートは入れ子不可。
- `body: "*"` → クエリパラメータなし、パス以外の全フィールドをボディで受ける。`body: "event"` → その1フィールドのみボディ、**残りのフィールドはクエリパラメータ**(パラメータ名はフィールドパス)。
- "Repeated message fields must not be mapped to URL query parameters"。
- `additional_bindings` で同一 RPC に複数バインディング。

```proto
import "google/api/annotations.proto";
import "google/protobuf/field_mask.proto";

service EventService {
  rpc GetEvent(GetEventRequest) returns (Event) {
    option (google.api.http) = { get: "/v1/events/{id}" };          // 他フィールドは ?query
  }
  rpc CreateEvent(CreateEventRequest) returns (Event) {
    option (google.api.http) = { post: "/v1/events" body: "event" };
  }
  rpc UpdateEvent(UpdateEventRequest) returns (Event) {
    option (google.api.http) = {
      patch: "/v1/events/{event.id}" body: "event"
      additional_bindings { put: "/v1/events/{event.id}" body: "event" }
    };
  }
  rpc DeleteEvent(DeleteEventRequest) returns (google.protobuf.Empty) {
    option (google.api.http) = { delete: "/v1/events/{id}" };
  }
}
message UpdateEventRequest { Event event = 1; google.protobf.FieldMask update_mask = 2; }
```

buf 依存: `deps: [buf.build/googleapis/googleapis]`(`buf registry module info` で存在確認済、作成 2022-09-06)。grpc-gateway 公式チュートリアル `docs/tutorials/adding_annotations.md` も同じ deps を使用。

### 1.2 buf.gen.yaml v2(リモートプラグイン)

bufbuild/plugins の `plugins/grpc-ecosystem/gateway/v2.30.0/buf.plugin.yaml` に登録済(deps: `protocolbuffers/go:v1.36.11`, `grpc/go:v1.6.2`、Go 1.25 以上)。

```yaml
version: v2
inputs:
  - directory: proto
plugins:
  - remote: buf.build/protocolbuffers/go:v1.36.11
    out: gen/go
    opt: paths=source_relative
  - remote: buf.build/grpc/go:v1.6.2
    out: gen/go
    opt: paths=source_relative
  - remote: buf.build/grpc-ecosystem/gateway:v2.30.0
    out: gen/go
    opt:
      - paths=source_relative
      - generate_unbound_methods=true   # HttpRule 無しの RPC も POST /pkg.Svc/Method で公開
  - remote: buf.build/grpc-ecosystem/openapiv2:v2.30.0
    out: gen/openapi
    opt:
      - output_format=yaml
      - allow_merge=true
      - merge_file_name=asobell        # → asobell.swagger.yaml
      - json_names_for_fields=true     # camelCase(protojson と一致)
```

`generate_unbound_methods`: 「POST method, URI path derived from service and method names (e.g., `/my.package.EchoService/Echo`)」、openapiv2 でも同オプション有効(docs/mapping/grpc_api_configuration)。openapiv2 のその他: `enums_as_ints`, `omit_enum_default_value`, `disable_default_errors`, `disable_service_tags`, `preserve_rpc_order`, `use_go_templates`, `enable_field_deprecation`(docs/mapping/customizing_openapi_output)。

### 1.3 ランタイム

**ヘッダ→メタデータのデフォルト(runtime/context.go)**
- `MetadataHeaderPrefix = "Grpc-Metadata-"`: このプレフィックス付き HTTP ヘッダはプレフィックスを外して metadata へ。
- `MetadataPrefix = "grpcgateway-"`: IANA 恒久ヘッダ(`Accept`, `Cookie`, `User-Agent`, `Content-Type` など `isPermanentHTTPHeader`)は `grpcgateway-<name>` として転送。**`Cookie` は恒久ヘッダなので、デフォルトでも `grpcgateway-cookie` キーで届く**。
- `Authorization` は例外的にプレフィックスなし。docs: 「The incoming `Authorization` HTTP header can not be removed or overwritten. It will always be forwarded in the gRPC metadata under the `authorization` key.」
- `X-Forwarded-For` / `X-Forwarded-Host` は自動付与。
- レスポンス側(runtime/mux.go): `defaultOutgoingHeaderMatcher` は gRPC ヘッダメタデータを **`Grpc-Metadata-<key>`** として HTTP レスポンスヘッダに、trailer は `Grpc-Trailer-` に出す。`WithOutgoingHeaderMatcher` で変更可。

**セッション Cookie を明示的にメタデータ化(`WithMetadata`)**。mux.go のドキュメントは用途として "retrieving authentication tokens from cookies for propagation" を挙げている。

```go
mux := runtime.NewServeMux(
    runtime.WithMetadata(func(ctx context.Context, r *http.Request) metadata.MD {
        md := metadata.MD{}
        if c, err := r.Cookie("session"); err == nil {
            md.Set("x-session-id", c.Value)
        }
        if m, ok := runtime.RPCMethod(ctx); ok { md.Set("x-rpc-method", m) } // docs/operations/annotated_context
        return md
    }),
    runtime.WithIncomingHeaderMatcher(func(key string) (string, bool) {
        switch key {
        case "X-Csrf-Token": return key, true
        default: return runtime.DefaultHeaderMatcher(key) // デフォルト規則を維持
        }
    }),
    runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
        MarshalOptions:   protojson.MarshalOptions{UseProtoNames: false, EmitUnpopulated: true},
        UnmarshalOptions: protojson.UnmarshalOptions{DiscardUnknown: true},
    }),
    runtime.WithErrorHandler(customErrorHandler),
    runtime.WithForwardResponseOption(forwardSetCookie),
)
```

**エラー処理(runtime/errors.go)**
- `type ErrorHandlerFunc func(context.Context, *ServeMux, Marshaler, http.ResponseWriter, *http.Request, error)`。
- `DefaultHTTPErrorHandler` は **`google.rpc.Status`(`code`, `message`, `details`)を Marshaler で JSON 化**。Unauthenticated のとき `WWW-Authenticate` を設定、サーバメタデータをレスポンスヘッダに転送、`*runtime.HTTPStatusError` でステータス上書き可。
- `HTTPStatusFromCode` マッピング(検証済): OK→200, Canceled→499, Unknown→500, **InvalidArgument→400**, DeadlineExceeded→504, **NotFound→404**, **AlreadyExists→409**, **PermissionDenied→403**, **Unauthenticated→401**, **ResourceExhausted→429**, **FailedPrecondition→400**(400 で確定), Aborted→409, OutOfRange→400, Unimplemented→501, Internal→500, **Unavailable→503**, DataLoss→500。

```go
func customErrorHandler(ctx context.Context, mux *runtime.ServeMux, m runtime.Marshaler,
    w http.ResponseWriter, r *http.Request, err error) {
    if st, ok := status.FromError(err); ok && st.Code() == codes.FailedPrecondition {
        err = &runtime.HTTPStatusError{HTTPStatus: http.StatusUnprocessableEntity, Err: err} // 422 にしたい場合
    }
    runtime.DefaultHTTPErrorHandler(ctx, mux, m, w, r, err)
}
```

**Set-Cookie(`WithForwardResponseOption` + `grpc.SetHeader`)**: gRPC 側で `grpc.SetHeader(ctx, metadata.Pairs("set-cookie", v))` すると、デフォルトでは `Grpc-Metadata-Set-Cookie` になってしまう。`runtime.ServerMetadataFromContext` で読み `w.Header().Add("Set-Cookie", ...)` する。

```go
func forwardSetCookie(ctx context.Context, w http.ResponseWriter, _ proto.Message) error {
    if md, ok := runtime.ServerMetadataFromContext(ctx); ok {
        for _, v := range md.HeaderMD.Get("set-cookie") { w.Header().Add("Set-Cookie", v) }
    }
    return nil
}
```
(docs/mapping/customizing_your_gateway の "Mutate response messages or set response headers" パターン。あるいは `WithOutgoingHeaderMatcher` で `set-cookie` → `Set-Cookie` にマップしてもよい。)

**登録関数の違い**(protoc-gen-grpc-gateway/internal/gengateway/template.go のコメント原文):
- `RegisterXxxHandlerFromEndpoint(ctx, mux, endpoint, dialOpts)`: 内部で接続を張る。
- `RegisterXxxHandler(ctx, mux, conn)`: 既存 `*grpc.ClientConn` を使う(チュートリアルは `grpc.NewClient` + これ)。
- `RegisterXxxHandlerServer(ctx, mux, srv)`: 「Note that using this registration option will cause many gRPC library features to stop working. ... **GRPC interceptors will not work for this type of registration.** To use interceptors, you must use the "runtime.WithMiddlewares" option in the "runtime.NewServeMux" call.」→ 認証・protovalidate をインターセプタで行う本件では **HandlerServer は不採用**。

**PATCH と FieldMask**(docs/mapping/patch_feature): 「If a binding is mapped to patch and the request message has exactly one FieldMask message in it, additional code is rendered ... that will populate the FieldMask based on the request body.」 `body: "*"` のときや PATCH 以外では FieldMask は通常フィールド(`updateMask` を JSON で明示)。デフォルト有効、`allow_patch_feature=false` で無効化。`optional` フィールドの未指定に関する明文はドキュメントに無い(自動マスクはボディに現れたキーから構築される)。

### 1.4 net/http との同居(Go 1.22+)

Go 1.22 の `ServeMux` はメソッド付きパターンと `{id}` ワイルドカード、`r.PathValue` を提供し、より特定的なパターンが優先(go.dev/blog/routing-enhancements)。

```go
root := http.NewServeMux()
root.Handle("/api/", gwmux)                                   // gateway (パスは /v1/... をそのまま使うなら "/v1/")
root.HandleFunc("GET /auth/callback", oauthCallback)          // plain net/http
root.Handle("/", spaHandler(staticFS))                        // SPA フォールバック
handler := csrf(cors(root))                                   // 通常のミドルウェア
srv := &http.Server{Addr: ":8080", Handler: handler}
```
`gwmux` は `http.Handler` なので `http.StripPrefix` を含め任意に合成可能。CORS/CSRF はゲートウェイ外で普通の `func(http.Handler) http.Handler` として掛ける(Cookie セッションなので CSRF トークン or `SameSite` + Origin 検査を推奨)。

---

## 2. OpenAPI 3 と TypeScript 型

| | grpc-gateway `protoc-gen-openapiv2` | gnostic `protoc-gen-openapi` |
|---|---|---|
| 出力 | **Swagger 2.0**(JSON/YAML) | **OpenAPI 3.0** |
| BSR | `buf.build/grpc-ecosystem/openapiv2:v2.30.0` | `buf.build/community/google-gnostic-openapi:v0.7.1`(Apache-2.0) |
| オプション | 上記 1.2 | `title`, `version`(既定 0.0.1), `description`, `naming=json\|proto`(既定 json: `updated_at`→`updatedAt`), `fq_schema_naming=true`(パッケージ名でプレフィックス), `enum_type=string\|integer`(既定 **integer**), `default_response=true`(既定; `google.rpc.Status` の default レスポンス追加), `depth`(循環メッセージ深さ, 既定 2) |
| Timestamp | string | `type: string, format: date-time` |
| Duration | string | `type: string, pattern: ^-?(?:0\|[1-9][0-9]{0,11})(?:\.[0-9]{1,9})?s$` |
| FieldMask | string | `type: string, format: field-mask` |
| int64 | string | `type: string`(format なし) |
| bytes | string/byte | `type: string, format: bytes` |
| enum | string(`enums_as_ints` で int) | `enum_type=string` で文字列 enum、既定は integer |
| oneof | 専用表現なし | **専用表現なし**(google/gnostic#251 未解決) |
| optional | `proto3_optional_nullable` あり | **nullable 表現なし**(google/gnostic#347) |
| メンテ | 活発(v2.30.0, 2026-08) | release は 2023-10 が最終、push は 2026-08 |

gnostic は `google.api.http` の get/post/put/patch/delete と `additional_bindings` を処理し、`body:"*"`/`body:"field"`/残りフィールド→クエリ(ネストは `foo.a` 形式)をサポート(generator.go)。

**openapi-typescript v7 は Swagger 2.0 非対応**(公式: "Supports OpenAPI 3.0 and 3.1"、"OpenAPI 2.x is supported with versions `5.x` and previous")。`swagger2openapi`(BSD-3-Clause、npm 7.0.8 は 2021 年、GitHub push 2023 年)で変換は可能だが更新が止まっている。

**推奨パス**: 
1. 第一候補: `buf.build/community/google-gnostic-openapi` で OAS 3.0 を直接生成(`naming=json`, `enum_type=string`, `fq_schema_naming=true`, `default_response=true`)→ `openapi-typescript` 7.x で `.d.ts`。oneof/optional の nullable は表現されない点を許容(SPA 側で `?` 扱い)。
2. 補足: grpc-gateway 本体にも **`protoc-gen-openapiv3`(OpenAPI 3.1, "Alpha — output is not yet stable")** が登場している(docs/mapping/openapi_v3)。int64→string、enum→string、**oneof を JSON Schema `oneOf` で表現**、Timestamp `format: date-time`。安定したら gnostic から乗り換える価値あり。現時点で production 採用は非推奨。
3. `openapiv2` + `swagger2openapi` は最後の手段。

---

## 3. リクエストバリデーション(protovalidate)

- buf 依存: `deps: [buf.build/bufbuild/protovalidate]`(CLI で存在確認済)。
- Go: **モジュールパスは `buf.build/go/protovalidate`**(README: "go get buf.build/go/protovalidate"、GitHub リポジトリ名は `bufbuild/protovalidate-go` v1.4.0)。
- インターセプタ: `github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/protovalidate`(v2.3.4)。ソース確認: 失敗時 **`codes.InvalidArgument`**、`st.WithDetails(valErr.ToProto())` で **`buf.validate.Violations` を details に添付**(google.rpc.BadRequest ではない)。CEL コンパイル失敗は `codes.Internal`。`WithIgnoreMessages` オプションあり。

```proto
import "buf/validate/validate.proto";

message CreateEventRequest {
  string title = 1 [(buf.validate.field).string = {min_len: 1, max_len: 100}];
  string slug  = 2 [(buf.validate.field).string.pattern = "^[a-z0-9-]+$"];
  Venue venue  = 3 [(buf.validate.field).required = true];
  google.protobuf.Timestamp starts_at = 4 [(buf.validate.field).timestamp.gt_now = true];
  repeated string tags = 5 [(buf.validate.field).repeated.max_items = 10];
  google.protobuf.Timestamp ends_at = 6;
  option (buf.validate.message).cel = {
    id: "ends_after_starts"
    message: "ends_at must be after starts_at"
    expression: "this.ends_at > this.starts_at"
  };
}
```
(`gt_now` は "can only be used with the within rule" 併用制約に注意。protovalidate.com/reference/rules)

```go
import (
    "buf.build/go/protovalidate"
    pvmw "github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/protovalidate"
)
validator, err := protovalidate.New()
srv := grpc.NewServer(grpc.ChainUnaryInterceptor(
    authInterceptor,                            // 4章
    pvmw.UnaryServerInterceptor(validator),
))
```
ゲートウェイ経由では InvalidArgument→400、JSON の `details[]` に `@type: type.googleapis.com/buf.validate.Violations` が入る。`google.rpc.BadRequest` 形式にしたい場合は自前インターセプタで `protovalidate.ValidationError` を `errdetails.BadRequest_FieldViolation{Field, Description}` に詰め替える。

---

## 4. サービス別認証インターセプタ(grpc-go)

`grpc.UnaryServerInfo.FullMethod` は "The full method string of the form /package.service/method"(pkg.go.dev)。`grpc.SetHeader`: "sets the header metadata to be sent from the server to the client. The context provided must be the context passed to the server's handler."

```go
func authInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo,
    handler grpc.UnaryHandler) (any, error) {
    md, _ := metadata.FromIncomingContext(ctx)
    switch {
    case strings.HasPrefix(info.FullMethod, "/asobell.v1.ProviderService/"):
        sid := first(md.Get("x-session-id"))            // WithMetadata で設定したキー
        // または first(md.Get("grpcgateway-cookie")) を自前パース(デフォルト転送)
        user, err := sessions.Lookup(ctx, sid)
        if err != nil { return nil, status.Error(codes.Unauthenticated, "login required") } // → 401
        ctx = withUser(ctx, user)
    case strings.HasPrefix(info.FullMethod, "/asobell.v1.PublicService/"):
        // 認証不要
    }
    return handler(ctx, req)
}

// ログイン RPC 内: Cookie 発行
_ = grpc.SetHeader(ctx, metadata.Pairs("set-cookie",
    (&http.Cookie{Name: "session", Value: sid, HttpOnly: true, Secure: true,
      SameSite: http.SameSiteLaxMode, Path: "/"}).String()))
```
metadata のキーは小文字正規化されるので `md.Get("x-session-id")` でよい。

---

## 5. protojson の JSON 表現(protobuf.dev/programming-guides/json)

| 型 | JSON |
|---|---|
| int64 / uint64 / fixed64 | **文字列**(`"1"`, `"-10"`)。2^53 超の精度保護のため。パーサは数値も受理 |
| enum | 値名の文字列(`"FOO_BAR"`)。数値も受理 |
| Timestamp | RFC 3339、Z 正規化、小数桁は 0/3/6/9 |
| Duration | 秒の小数文字列 + `s`(`"3s"`, `"1.000340012s"`、`"604800s"`) |
| FieldMask | lowerCamelCase のカンマ区切り文字列(`"title,startsAt"`) |
| フィールド名 | 既定 lowerCamelCase(`json_name` で上書き)。パーサは proto 名も受理 |
| デフォルト値 | presence のない既定値は省略。`EmitUnpopulated` で常時出力 |
| oneof | 同一 oneof の複数キー禁止 |

→ SPA の TS 型では `int64` が `string` になる点、gnostic 出力(`type: string`)と整合。

---

## 6. 代替: connect-go + connect-es

- `connectrpc/connect-go` 4,067★ / `connect-es` 1,808★、共に Apache-2.0(v1.21.0 / v2.2.0)。1 ポートで Connect / gRPC / gRPC-Web を同時提供、ブラウザは HTTP/1.1 上で "JSON-encoded Protobuf" を POST(冪等 RPC は GET 可)。
- ただし URL は **`/pkg.Service/Method` 固定で `google.api.http` の REST パスは使わない**(connectrpc.com/docs/introduction)。OpenAPI も一次成果物ではなく、型は `protoc-gen-es` で直接 TS に生成する設計。
- 本件は「REST via grpc-gateway」「OpenAPI/TS 型を proto から生成」「Cookie セッション」が要件で、`GET /v1/events/{id}` 形式の RESTful URL・OpenAPI ドキュメント・net/http ミドルウェアとの同居が必要なため **grpc-gateway が適合**。Connect は REST 形状が不要で TS クライアント直結を優先する場合の選択肢。

---

## 出典
- GitHub API: `api.github.com/repos/{grpc-ecosystem/grpc-gateway, google/gnostic, bufbuild/protovalidate, bufbuild/protovalidate-go, grpc-ecosystem/go-grpc-middleware, connectrpc/connect-go, connectrpc/connect-es, openapi-ts/openapi-typescript, Mermade/oas-kit, grpc/grpc-go, protocolbuffers/protobuf-go, bufbuild/buf}` および `/releases/latest`; npm registry (`openapi-typescript`, `swagger2openapi`)
- grpc-gateway docs: https://grpc-ecosystem.github.io/grpc-gateway/docs/mapping/customizing_your_gateway/ , …/mapping/customizing_openapi_output/ , …/mapping/patch_feature/ , …/mapping/grpc_api_configuration/ , …/mapping/openapi_v3/ , …/tutorials/adding_annotations/ , …/operations/annotated_context/
- grpc-gateway source: `runtime/errors.go`, `runtime/context.go`, `runtime/mux.go`, `protoc-gen-grpc-gateway/internal/gengateway/template.go` (main)
- googleapis: https://github.com/googleapis/googleapis/blob/master/google/api/http.proto
- bufbuild/plugins: `plugins/grpc-ecosystem/gateway/v2.30.0/buf.plugin.yaml`, `plugins/community/google-gnostic-openapi/{v0.7.0,v0.7.1}/`; buf docs https://buf.build/docs/configuration/v2/buf-gen-yaml/ ; `buf registry module info`(googleapis, protovalidate)
- gnostic: `cmd/protoc-gen-openapi/README.md`, `generator/generator.go`, `generator/reflector.go`, `generator/wellknown/schemas.go`; issues #251 (oneof), #347 (nullable), #365 (buf plugin)
- protovalidate: https://protovalidate.com/quickstart/grpc-go , https://protovalidate.com/reference/rules , https://protovalidate.com/schemas/custom-rules , protovalidate-go README; go-grpc-middleware `interceptors/protovalidate/protovalidate.go`
- protobuf JSON: https://protobuf.dev/programming-guides/json/
- grpc-go: https://pkg.go.dev/google.golang.org/grpc#UnaryServerInfo
- Go 1.22 routing: https://go.dev/blog/routing-enhancements
- openapi-typescript: https://openapi-ts.dev/introduction
- Connect: https://connectrpc.com/docs/introduction
