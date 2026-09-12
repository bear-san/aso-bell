package discord

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/stretchr/testify/require"

	"github.com/bear-san/aso-bell/internal/provider/adapter"
)

const (
	guildID   = "100000000000000001"
	botID     = "200000000000000002"
	organizer = "300000000000000003"
	guest     = "400000000000000004"
	channelID = "500000000000000005"
	messageID = "600000000000000006"
	appID     = "700000000000000007"
)

// recorded は スタブが受け取ったリクエスト 1 件。
type recorded struct {
	Method string
	Path   string
	Body   map[string]any
}

// stub は Discord REST API のテスト用スタブ。discordgo の Client を差し替えて使う。
type stub struct {
	t      *testing.T
	mux    *http.ServeMux
	server *httptest.Server

	mu       sync.Mutex
	requests []recorded
}

func newStub(t *testing.T) *stub {
	t.Helper()

	s := &stub{t: t, mux: http.NewServeMux()}
	s.server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.server.Close)

	return s
}

func (s *stub) serve(w http.ResponseWriter, r *http.Request) {
	body := map[string]any{}

	if raw, err := io.ReadAll(r.Body); err == nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, &body)
		r.Body = io.NopCloser(strings.NewReader(string(raw)))
	}

	s.mu.Lock()
	s.requests = append(s.requests, recorded{Method: r.Method, Path: r.URL.Path, Body: body})
	s.mu.Unlock()

	s.mux.ServeHTTP(w, r)
}

// handle は "METHOD /channels/{id}" 形式(API バージョン部分は省略)でハンドラを登録する。
func (s *stub) handle(pattern string, status int, body any) {
	method, path, _ := strings.Cut(pattern, " ")

	s.mux.HandleFunc(method+" /api/v"+discordgo.APIVersion+path, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)

		if body != nil {
			// ハンドラ内では require を使えないため、失敗はレスポンスの欠落として現れる。
			_ = json.NewEncoder(w).Encode(body)
		}
	})
}

// requestsTo は method と path 接尾辞に一致したリクエストを返す。
func (s *stub) requestsTo(method, suffix string) []recorded {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []recorded

	for _, req := range s.requests {
		if req.Method == method && strings.HasSuffix(req.Path, suffix) {
			out = append(out, req)
		}
	}

	return out
}

// rewrite は discord.com 宛のリクエストをスタブへ向ける RoundTripper。
// discordgo のエンドポイントはパッケージ変数で並行テストから書き換えられないため、
// Session.Client の差し替えで対応する。
type rewrite struct {
	base *url.URL
}

func (t rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	out := req.Clone(req.Context())
	out.URL.Scheme = t.base.Scheme
	out.URL.Host = t.base.Host
	out.Host = t.base.Host

	return http.DefaultTransport.RoundTrip(out) //nolint:wrapcheck // 転送するだけで文脈を足さない
}

// newTestAdapter はスタブへ向けた Adapter を作る。Gateway は開かないため Ready 相当の状態を自分で入れる。
func newTestAdapter(t *testing.T) (*Adapter, *stub) {
	t.Helper()

	s := newStub(t)
	base, err := url.Parse(s.server.URL)
	require.NoError(t, err)

	a, err := New(Config{
		Token:         "test-token",
		ApplicationID: appID,
		CommandName:   "asobell",
		Logger:        slog.New(slog.DiscardHandler),
		HTTPClient:    &http.Client{Transport: rewrite{base: base}},
	})
	require.NoError(t, err)

	a.session.State.User = &discordgo.User{ID: botID, Bot: true}
	// 429 の自動再試行はテストを待たせるだけなので切る。本番は docs/06 §7 のとおり ctx で打ち切る。
	a.session.ShouldRetryOnRateLimit = false

	return a, s
}

func workspace() adapter.WorkspaceRef {
	return adapter.WorkspaceRef{WorkspaceID: "66e0a1b2c3d4e5f607182930", ExternalID: guildID}
}
