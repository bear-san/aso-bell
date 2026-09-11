<!-- 調査エージェントによる裏取りレポート(自動抽出)。設計本文は docs/03-tech-stack.md を参照 -->

# Go バックエンド ライブラリ選定レポート（REST API + スケジューラ + MongoDB + Google OIDC）

調査日: 2026-09-11。Star 数・ライセンス・最新版は GitHub API (`/repos/OWNER/REPO`, `/releases/latest`, `/tags`) で取得。API 使用例は Context7 および各リポジトリのタグ固定ソースで確認。

## サマリー表

| カテゴリ | 採用 | Stars | SPDX | 最新版 |
|---|---|---|---|---|
| MongoDB driver | `go.mongodb.org/mongo-driver/v2` | 8,539 | Apache-2.0 | v2.9.1 |
| HTTP router | `github.com/gin-gonic/gin` | 89,200 | MIT | v1.12.0 |
| Cron | `github.com/robfig/cron/v3` | 14,184 | MIT | v3.0.1 (tag) |
| OIDC | `github.com/coreos/go-oidc/v3` + `golang.org/x/oauth2` | 2,476 / 5,898 | Apache-2.0 / BSD-3-Clause(例外) | v3.21.0 / v0.37.0 |
| Session/JWT | `github.com/golang-jwt/jwt/v5` (Cookie に格納) | 9,222 | MIT | v5.3.1 |
| Config | `github.com/spf13/viper` | 30,458 | MIT | v1.21.0 |
| Validation | `github.com/go-playground/validator/v10` | 20,154 | MIT | v10.30.4 |
| Test | `stretchr/testify` / `testcontainers-go` / `go.uber.org/mock` | 26,203 / 4,974 / 3,404 | MIT / MIT / Apache-2.0 | v1.12.1 / v0.44.0 / v0.6.0 |
| OpenAPI | `oapi-codegen/oapi-codegen/v2` + `openapi-typescript` | 8,568 / 8,363 | Apache-2.0 / MIT | v2.8.0 / 7.13.0 (npm) |
| Logging | `log/slog` (stdlib) | – | BSD-3 (Go) | Go 1.21+ |
| Lint / Task | golangci-lint (GPL-3.0, dev tool) / `go-task/task` | 19,367 / 16,123 | GPL-3.0 / MIT | v2.13.2 / v3.53.1 |
| Retry | `github.com/cenkalti/backoff/v7` | 4,063 | MIT | v7.0.0 (tag) |

---

## 1. MongoDB driver — `go.mongodb.org/mongo-driver/v2`

候補は公式ドライバのみ (8,539★, Apache-2.0, v2.9.1, 最終 push 2026-09-10)。
Context7: `/websites/mongodb_drivers_go_current`, `/mongodb/mongo-go-driver`。

**確認済み事項**

- import: `go.mongodb.org/mongo-driver/v2/bson`, `.../v2/mongo`, `.../v2/mongo/options`
- `func Connect(opts ...*options.ClientOptions) (*Client, error)` — v2 では **ctx 引数なし** (mongo/client.go L110)。各操作に `ctx` を渡す。
- `bson.ObjectID` は `go.mongodb.org/mongo-driver/v2/bson` 直下 (`type ObjectID [12]byte`, `NewObjectID()`, `ObjectIDFromHex(s)`)。v1 の `primitive` パッケージは廃止。
- 時刻: Decoder はデフォルトで `time.Time` を **UTC** で復元 (`Decoder.UseLocalTimeZone()` で local に変更可, bson/decoder.go L127-131)。BSON datetime は TZ 情報を持たないので、アプリ側は常に `time.Now().UTC()` で統一するのが安全。
- `IndexModel.Keys` は順序保持型 (`bson.D`) 必須、`bson.M` 不可 (mongo/index_view.go コメント)。
- TTL: `options.Index().SetExpireAfterSeconds(seconds int32)` (options/indexoptions.go L241)。
- トランザクション: `func (s *Session) WithTransaction(ctx, fn func(ctx context.Context) (any, error), opts ...options.Lister[options.TransactionOptions]) (any, error)`。MongoDB マニュアルより「Transactions are supported on replica sets and sharded clusters」— **スタンドアロン不可**。開発/テストはシングルノード replica set で可 (後述 testcontainers)。

```go
client, err := mongo.Connect(options.Client().ApplyURI(uri))
defer client.Disconnect(ctx)
coll := client.Database("app").Collection("users")

type User struct {
    ID        bson.ObjectID `bson:"_id,omitempty"`
    Email     string        `bson:"email"`
    CreatedAt time.Time     `bson:"created_at"`
}

_, err = coll.InsertOne(ctx, User{Email: "a@b", CreatedAt: time.Now().UTC()})
var u User
err = coll.FindOne(ctx, bson.M{"email": "a@b"}).Decode(&u)
_, err = coll.UpdateOne(ctx, bson.D{{"_id", u.ID}}, bson.D{{"$set", bson.D{{"name", "x"}}}})

// unique + TTL index
_, err = coll.Indexes().CreateMany(ctx, []mongo.IndexModel{
    {Keys: bson.D{{"email", 1}}, Options: options.Index().SetUnique(true)},
    {Keys: bson.D{{"expires_at", 1}}, Options: options.Index().SetExpireAfterSeconds(0)},
})

// FindOneAndUpdate → 更新後ドキュメント
opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
err = coll.FindOneAndUpdate(ctx, filter, update, opts).Decode(&u)

// transaction (replica set 必須)
sess, _ := client.StartSession()
defer sess.EndSession(ctx)
_, err = sess.WithTransaction(ctx, func(ctx context.Context) (any, error) {
    return coll.InsertMany(ctx, docs)
}, options.Transaction().SetWriteConcern(writeconcern.Majority()))
```

Sources: https://www.mongodb.com/docs/drivers/go/current/ (crud/transactions, indexes, compound-operations), https://github.com/mongodb/mongo-go-driver/blob/v2.9.1/, https://www.mongodb.com/docs/manual/core/transactions/

---

## 2. HTTP router — `gin-gonic/gin`

| 候補 | Stars | SPDX | 最新版 |
|---|---|---|---|
| gin-gonic/gin | 89,200 | MIT | v1.12.0 (2026-02-28) |
| labstack/echo | 32,705 | MIT | v5.3.1 (2026-07-21; v5 系に移行済) |
| go-chi/chi | 22,812 | MIT | v5.3.2 |
| net/http (Go 1.22+) | – | BSD-3 (Go) | stdlib |

Star 数ルールで **gin** を採用。トレードオフ: gin は `*gin.Context` という独自ハンドラ型で `http.Handler` 互換ではなく、標準 middleware の再利用に adapter が要る。chi / net/http は完全 stdlib 互換で依存最小。Go 1.22 の `ServeMux` は `"GET /posts/{id}"` のメソッド付きパターン、`{name...}`、`{$}`、`r.PathValue("id")` をサポートし「最も具体的なパターンが勝つ」(go.dev/blog/routing-enhancements)。小規模なら net/http のみでも十分。oapi-codegen は gin/echo/chi/std-http 全て対応するので後で乗り換え可能。

**確認済み API** (Context7 `/gin-gonic/gin`)

```go
r := gin.New()
r.Use(gin.Recovery(), loggingMW())        // グローバル middleware
api := r.Group("/api")
v1 := api.Group("/v1"); v1.Use(AuthMiddleware())
{
    v1.GET("/users", ListUsers)
    v1.POST("/users", CreateUser)
}

type CreateUserReq struct {
    Email string `json:"email" binding:"required,email"`
    Age   int    `json:"age"   binding:"gte=0,lte=150"`
}
func CreateUser(c *gin.Context) {
    var req CreateUserReq
    if err := c.ShouldBindJSON(&req); err != nil { // validator v10 を内蔵
        c.JSON(400, gin.H{"error": err.Error()}); return
    }
    ctx := c.Request.Context()                     // DB 呼び出しへ渡す context
    ...
}

// graceful shutdown (gin docs/doc.md)
srv := &http.Server{Addr: ":8080", Handler: r}
go func() { if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) { log.Fatal(err) } }()
ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
defer stop()
<-ctx.Done()
shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
_ = srv.Shutdown(shutdownCtx)
```

Sources: https://github.com/gin-gonic/gin (docs/doc.md, _autodocs/03-router.md, 05-binding.md), https://go.dev/blog/routing-enhancements

---

## 3. Cron — `robfig/cron/v3`

| 候補 | Stars | SPDX | 最新版 | 備考 |
|---|---|---|---|---|
| robfig/cron | 14,184 | MIT | v3.0.1 (tag; GitHub Releases なし) | 最終 push **2024-07-08** |
| go-co-op/gocron | 7,157 | MIT | v2.22.0 | 活発 (2026-09-01) |
| time.Ticker | stdlib | – | – | 固定間隔のみ、cron 式・TZ なし |

Star 数ルールで **robfig/cron** を採用。ただし 2 年以上更新がない点は明記 (安定版で API は枯れている)。将来 分散ロックや job 単位の singleton 実行が要るなら gocron v2 (`gocron.NewScheduler(gocron.WithLocation(time.UTC))`, `s.NewJob(gocron.CronJob("*/30 * * * * *", true), gocron.NewTask(fn))`, `s.Start()`/`s.Shutdown()`) が候補。

**確認済み API** (Context7 `/robfig/cron`)

```go
import "github.com/robfig/cron/v3"

loc, _ := time.LoadLocation("Asia/Tokyo")
c := cron.New(cron.WithSeconds(), cron.WithLocation(loc)) // 6 フィールド: sec min hour dom mon dow
id, err := c.AddFunc("0 0 9 * * MON-FRI", func() { ... })
_, _ = c.AddFunc("CRON_TZ=UTC 0 */5 * * * *", func() { ... }) // ジョブ単位 TZ 上書き
c.Start()
...
stopCtx := c.Stop()   // 新規実行を止め、実行中ジョブ完了で Done
<-stopCtx.Done()
```

Source: https://github.com/robfig/cron, https://pkg.go.dev/github.com/robfig/cron/v3

---

## 4. Google OIDC — `coreos/go-oidc/v3` + `golang.org/x/oauth2`

| 候補 | Stars | SPDX | 最新版 |
|---|---|---|---|
| coreos/go-oidc | 2,476 | Apache-2.0 | v3.21.0 |
| golang/oauth2 | 5,898 | **BSD-3-Clause** | v0.37.0 |

`golang.org/x/oauth2` は Go 公式サブリポジトリで go-oidc 自体が依存しているため **BSD-3 例外として採用** (Go 本体と同ライセンス)。go-oidc は ID token 署名検証 (JWKS 自動取得) を担う。

**確認済み API** (Context7 `/coreos/go-oidc`, `/golang/oauth2`; oidc/verify.go, oidc/oidc.go @v3.21.0)

```go
import (
    "github.com/coreos/go-oidc/v3/oidc"
    "golang.org/x/oauth2"
)

provider, err := oidc.NewProvider(ctx, "https://accounts.google.com")
verifier := provider.Verifier(&oidc.Config{ClientID: clientID}) // aud/iss/exp/署名を検証
conf := &oauth2.Config{
    ClientID: clientID, ClientSecret: clientSecret, RedirectURL: redirectURL,
    Endpoint: provider.Endpoint(),
    Scopes:   []string{oidc.ScopeOpenID, "email", "profile"}, // oidc.ScopeEmail / ScopeProfile 定数あり
}

// /login
state := randomString(); pkce := oauth2.GenerateVerifier()   // state, verifier は Cookie/セッションに保存
url := conf.AuthCodeURL(state, oauth2.S256ChallengeOption(pkce))
http.Redirect(w, r, url, http.StatusFound)

// /callback  (state を照合してから)
tok, err := conf.Exchange(ctx, r.URL.Query().Get("code"), oauth2.VerifierOption(pkce))
rawID, ok := tok.Extra("id_token").(string)
idToken, err := verifier.Verify(ctx, rawID)   // *oidc.IDToken{Issuer, Audience, Subject, Expiry, IssuedAt, Nonce}
var claims struct {
    Sub           string `json:"sub"`
    Email         string `json:"email"`
    EmailVerified bool   `json:"email_verified"`
    Name          string `json:"name"`
    Picture       string `json:"picture"`
    HD            string `json:"hd"`
}
err = idToken.Claims(&claims)
```

Google 側仕様 (developers.google.com/identity/openid-connect): `sub` が唯一の永続 ID (email は変わり得る)、`hd` は Workspace ドメインで「ID token 内の値は信頼できる」が、ドメイン制限をするならサーバー側で `hd` を必ず検証すること。`email_verified` も確認する。

Sources: https://github.com/coreos/go-oidc/blob/v3/README.md, https://pkg.go.dev/golang.org/x/oauth2, https://developers.google.com/identity/openid-connect/openid-connect

---

## 5. Session / JWT — `golang-jwt/jwt/v5` + Cookie

| 候補 | Stars | SPDX | 最新版 |
|---|---|---|---|
| golang-jwt/jwt | 9,222 | MIT | v5.3.1 |
| gorilla/sessions | 3,151 | BSD-3-Clause | v1.4.0 (最終 push 2024-08) |
| gorilla/securecookie | 729 | BSD-3-Clause | v1.1.2 |

**推奨: HS256 署名 JWT を HttpOnly Cookie に格納** (gorilla は BSD-3 かつ更新停滞)。Cookie は `HttpOnly; Secure; SameSite=Lax; Path=/`、有効期限は短め (例 24h) + 必要なら MongoDB に session コレクション (TTL index) を持ち失効を可能にする。

**確認済み API** (Context7 `/golang-jwt/jwt`; module path `github.com/golang-jwt/jwt/v5`)

```go
type SessionClaims struct {
    Email string `json:"email"`
    jwt.RegisteredClaims
}
claims := SessionClaims{Email: email, RegisteredClaims: jwt.RegisteredClaims{
    Issuer: "app", Subject: googleSub,
    ExpiresAt: jwt.NewNumericDate(time.Now().Add(24*time.Hour)),
    IssuedAt:  jwt.NewNumericDate(time.Now()),
}}
signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)

parsed := &SessionClaims{}
tok, err := jwt.ParseWithClaims(cookieVal, parsed,
    func(t *jwt.Token) (any, error) { return secret, nil },
    jwt.WithValidMethods([]string{"HS256"}),   // alg 混同攻撃対策 (推奨)
    jwt.WithExpirationRequired(), jwt.WithIssuer("app"),
)
if err != nil || !tok.Valid { /* 401 */ }
```

Source: https://github.com/golang-jwt/jwt (_autodocs/api-reference/parser.md, parser-options.md, claims.md)

---

## 6. Config — `spf13/viper`

| 候補 | Stars | SPDX | 最新版 |
|---|---|---|---|
| spf13/viper | 30,458 | MIT | v1.21.0 |
| caarlos0/env | 6,308 | MIT | v11.4.1 |
| kelseyhightower/envconfig | 5,468 | MIT | v1.4.0 (2025-06 以降更新なし) |
| knadh/koanf | 4,194 | MIT | v2.3.6 |

Star ルールで **viper**。注意点: 依存が重い (mapstructure, fsnotify, 各種パーサ) こと、`AutomaticEnv` + `Unmarshal` の組合せは **`SetDefault` または `BindEnv` していないキーが struct に入らない** 既知の挙動があるため、全キーに `SetDefault` を置く。env 専用で軽量にしたいなら `caarlos0/env/v11` (`env.ParseAs[Config]()`, タグ `env:"PORT" envDefault:"8080"`, `env:"KEY,required"`) が第 2 候補。

**確認済み API** (Context7 `/websites/pkg_go_dev_github_com_spf13_viper`)

```go
type Config struct {
    Port     int    `mapstructure:"port"`
    MongoURI string `mapstructure:"mongo_uri"`
    Google   struct {
        ClientID string `mapstructure:"client_id"`
    } `mapstructure:"google"`
}
v := viper.New()
v.SetEnvPrefix("APP")                                   // APP_PORT, APP_GOOGLE_CLIENT_ID
v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
v.AutomaticEnv()
v.SetDefault("port", 8080); v.SetDefault("mongo_uri", ""); v.SetDefault("google.client_id", "")
var cfg Config
err := v.Unmarshal(&cfg)   // func (v *Viper) Unmarshal(rawVal any, opts ...DecoderConfigOption) error
```

Source: https://pkg.go.dev/github.com/spf13/viper, https://github.com/caarlos0/env

---

## 7. Validation — `go-playground/validator/v10` (20,154★, MIT, v10.30.4)

gin が内蔵しているものと同一。ドメイン層で単独使用する場合 (Context7 `/go-playground/validator`):

```go
validate := validator.New(validator.WithRequiredStructEnabled())
type User struct {
    Name  string `validate:"required,min=2,max=100"`
    Email string `validate:"required,email"`
    Age   int    `validate:"gte=0,lte=150"`
}
if err := validate.Struct(u); err != nil {
    for _, e := range err.(validator.ValidationErrors) { fmt.Println(e.Field(), e.Tag()) }
}
```

Source: https://github.com/go-playground/validator

---

## 8. Testing

| 候補 | Stars | SPDX | 最新版 |
|---|---|---|---|
| stretchr/testify | 26,203 | MIT | v1.12.1 |
| testcontainers/testcontainers-go | 4,974 | MIT | v0.44.0 |
| uber-go/mock | 3,404 | Apache-2.0 | v0.6.0 |
| vektra/mockery | 7,160 | **BSD-3-Clause** | v3.8.0 |

Mock は Star では mockery が上だが BSD-3 のため **uber-go/mock** (Apache-2.0, `go install go.uber.org/mock/mockgen@latest`, `//go:generate mockgen -source=repo.go -destination=mocks/mock_repo.go -package=mocks`, `ctrl := gomock.NewController(t); m := NewMockRepo(ctrl); m.EXPECT().Get(gomock.Eq(1)).Return(x, nil)`) を推奨。mockery を使うなら BSD 例外として明記。

**testcontainers-go MongoDB モジュール** (`github.com/testcontainers/testcontainers-go/modules/mongodb`, v0.44.0 のソースで確認):

- `func Run(ctx, img string, opts ...testcontainers.ContainerCustomizer) (*MongoDBContainer, error)`
- `func WithReplicaSet(replSetName string) testcontainers.CustomizeRequestOption` — **引数に replica set 名が必要** (Context7 の古い例は引数なしだが現行は string 必須)。トランザクション試験用のシングルノード RS を起動。
- `func (c *MongoDBContainer) ConnectionString(ctx) (string, error)`
- モジュール自身が `go.mongodb.org/mongo-driver/v2` に依存 (go.mod) なので v2 と整合。

```go
mc, err := mongodb.Run(ctx, "mongo:7", mongodb.WithReplicaSet("rs0"))
testcontainers.CleanupContainer(t, mc)
uri, _ := mc.ConnectionString(ctx)
client, _ := mongo.Connect(options.Client().ApplyURI(uri))
```

testify: `assert.Equal(t, want, got)`, `require.NoError(t, err)` (失敗で即停止), `suite.Suite` を埋め込み `SetupTest()`/`TearDownTest()`、`suite.Run(t, new(MyTestSuite))`。

Sources: https://github.com/testcontainers/testcontainers-go/blob/v0.44.0/modules/mongodb/mongodb.go, https://github.com/stretchr/testify, https://github.com/uber-go/mock

---

## 9. OpenAPI — `oapi-codegen` + `openapi-typescript`

| 候補 | Stars | SPDX | 最新版 | 方式 |
|---|---|---|---|---|
| swaggo/swag | 13,014 | MIT | v2.0.0-rc5 | code-first (コメント→spec) |
| oapi-codegen/oapi-codegen | 8,568 | Apache-2.0 | v2.8.0 | spec-first (spec→Go) |
| openapi-ts/openapi-typescript | 8,363 | MIT | 7.13.0 (npm) | spec→TS 型 |

swag の方が Star は多いが目的が逆 (コードから spec 生成)。TS クライアントと契約を共有する spec-first 運用のため **oapi-codegen** を採用 (用途が異なるための例外)。

**確認済み設定** (Context7 `/oapi-codegen/oapi-codegen`, docs/gin-server.md, README)

```yaml
# api/oapi-codegen.yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/oapi-codegen/oapi-codegen/v2.8.0/configuration-schema.json
package: api
generate:
  gin-server: true      # echo-server / chi-server / std-http-server も選択可
  strict-server: true   # 別のサーバ種別と併用必須
  models: true
  embedded-spec: true
output: gen.go
```

```go
//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config oapi-codegen.yaml openapi.yaml

// 生成物: StrictServerInterface { FindPets(ctx context.Context, req FindPetsRequestObject) (FindPetsResponseObject, error) }
//         NewStrictHandler(ssi StrictServerInterface, middlewares []StrictMiddlewareFunc) ServerInterface
//         RegisterHandlers(router gin.IRouter, si ServerInterface)
var _ api.StrictServerInterface = (*Server)(nil)
r := gin.Default()
api.RegisterHandlers(r, api.NewStrictHandler(&Server{}, nil))
```

TS: `npx openapi-typescript ./openapi.yaml -o ./src/api/schema.d.ts` (fetch クライアントは同 monorepo の `openapi-fetch`)。

Sources: https://github.com/oapi-codegen/oapi-codegen, https://registry.npmjs.org/openapi-typescript/latest, https://github.com/openapi-ts/openapi-typescript

---

## 10. Logging — `log/slog` (stdlib)

`func NewJSONHandler(w io.Writer, opts *HandlerOptions) *JSONHandler`。

```go
logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
slog.SetDefault(logger)
reqLog := logger.With("request_id", id)                   // 属性を固定した子ロガー
reqLog.InfoContext(ctx, "user created", slog.Group("user", "id", u.ID.Hex(), "email", u.Email))
// → {"time":"...","level":"INFO","msg":"user created","request_id":"...","user":{"id":"...","email":"..."}}
```

context 連携は `Handler.Handle(ctx, Record)` で ctx から trace id 等を取り出す custom handler を挟むか、`samber/slog-gin` (Context7 `/samber/slog-gin`) で gin のアクセスログを slog に流す。

Source: https://pkg.go.dev/log/slog

---

## 11. Lint / Formatter / Task runner

| ツール | Stars | SPDX | 最新版 |
|---|---|---|---|
| golangci/golangci-lint | 19,367 | **GPL-3.0** | v2.13.2 |
| dominikh/go-tools (staticcheck) | 6,890 | MIT | 2026.2.1 |
| mvdan/gofumpt | 4,075 | BSD-3-Clause | v0.12.0 |
| go-task/task | 16,123 | MIT | v3.53.1 |

**golangci-lint の GPL-3.0 について**: CI/開発時に実行するバイナリであり、成果物にリンクも同梱もされないため、GPL の伝播は発生しない。「配布物に GPL コードを含めない」ポリシーであれば **採用可** と判断する (staticcheck 等の各 linter は golangci-lint がラップして呼ぶだけ)。組織ポリシーで GPL ツールの使用自体を禁じている場合のみ `staticcheck` + `go vet` + `gofumpt` を個別実行に切り替える。gofumpt は BSD-3 だが同じく開発時ツールなので例外扱いで問題なし。

Task runner は Star 数と YAML の可読性、Windows 互換で **go-task/task** (Makefile は依存ゼロだがタブ/シェル差異が面倒)。

```yaml
version: '3'
dotenv: ['.env']
tasks:
  gen:  { cmds: ['go generate ./...'] }
  lint: { cmds: ['golangci-lint run ./...'] }
  test: { deps: [gen], cmds: ['go test -race ./...'] }
  run:  { cmds: ['go run ./cmd/server'] }
```

Sources: https://github.com/golangci/golangci-lint, https://taskfile.dev/usage/

---

## 12. Retry / Backoff — `cenkalti/backoff/v7`

| 候補 | Stars | SPDX | 最新版 |
|---|---|---|---|
| cenkalti/backoff | 4,063 | MIT | v7.0.0 (tag) |
| avast/retry-go | 2,950 | MIT | v5.0.0 |

Star ルールで **cenkalti/backoff**。v7 は generics + context 対応 (Context7 `/cenkalti/backoff`, retry.go @v7):

```go
import "github.com/cenkalti/backoff/v7"

res, err := backoff.Retry(ctx, func() (*Result, error) {
    r, err := callExternal(ctx)
    if isClientError(err) { return nil, backoff.Permanent(err) } // 再試行しない
    return r, err
}, backoff.WithMaxTries(5), backoff.WithMaxElapsedTime(30*time.Second),
   backoff.WithNotify(func(err error, d time.Duration) { slog.Warn("retry", "err", err, "in", d) }))
// func Retry[T any](ctx context.Context, operation Operation[T], opts ...RetryOption) (T, error)
```

Source: https://github.com/cenkalti/backoff/blob/v7/README.md

---

## ライセンス例外まとめ

- `golang.org/x/oauth2` — BSD-3-Clause (Go 公式、go-oidc の必須依存)。**例外として採用**。
- `mvdan/gofumpt` — BSD-3-Clause、開発時ツールのみ。**例外**。
- `golangci-lint` — GPL-3.0、開発時ツールのみ (非リンク)。**採用可と判断**。
- 不採用 (BSD-3 のため代替あり): gorilla/sessions, gorilla/securecookie, vektra/mockery。

## 注意事項

- robfig/cron は 2024-07 以降更新なし。Star ルール上の採用だが、保守性重視なら gocron v2 に差し替え可能。
- testcontainers-go の `WithReplicaSet` は現行版で `string` 引数必須 (Context7 の一部例は旧 API)。
- mongo-driver v2 は `Connect` に ctx を取らない、`primitive.ObjectID` → `bson.ObjectID` など v1 と非互換 (docs/migration-2.0.md)。
- Echo は 2026 年に v5 系 (v5.3.1) をリリース済み。oapi-codegen には `echo5-server` オプションあり。
