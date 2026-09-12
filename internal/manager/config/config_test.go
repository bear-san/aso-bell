package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bear-san/aso-bell/internal/manager/config"
)

func minimalEnv() map[string]string {
	return map[string]string{
		"ASOBELL_PROVIDER_ADDR":        "dns:///provider:9091",
		"ASOBELL_RPC_TOKEN":            strings.Repeat("t", 32),
		"ASOBELL_BASE_URL":             "https://asobell.example.com/",
		"ASOBELL_MONGO_URI":            "mongodb://mongo:27017",
		"ASOBELL_GOOGLE_CLIENT_ID":     "cid",
		"ASOBELL_GOOGLE_CLIENT_SECRET": "secret",
	}
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := config.Load(minimalEnv())
	require.NoError(t, err)

	assert.Equal(t, ":8080", cfg.HTTPAddr)
	assert.Equal(t, ":9090", cfg.RPCAddr)
	assert.Equal(t, "https://asobell.example.com", cfg.BaseURL)
	assert.Equal(t, "asobell", cfg.MongoDB)
	assert.Equal(t, 168*time.Hour, cfg.SessionTTL)
	assert.True(t, cfg.CookieSecure)
	assert.Equal(t, 10*time.Minute, cfg.LinkTokenTTL)
	assert.Equal(t, 25*time.Second, cfg.ProviderTimeout)
	assert.Equal(t, 15*time.Second, cfg.ProviderPollInterval)
	assert.Equal(t, 3, cfg.ProviderOfflineAfterFailures)
	assert.Equal(t, 10*time.Second, cfg.JobsPollInterval)
	assert.Equal(t, 10, cfg.JobsBatchSize)
	assert.Equal(t, 2*time.Minute, cfg.JobsLease)
	assert.Equal(t, 4, cfg.JobsWorkers)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, "json", cfg.LogFormat)
	assert.False(t, cfg.DevLogin)
	assert.False(t, cfg.RPCTLS.Enabled())
	assert.Empty(t, cfg.AllowedEmails)
}

func TestLoadOverrides(t *testing.T) {
	e := minimalEnv()
	e["ASOBELL_HTTP_ADDR"] = ":18080"
	e["ASOBELL_ALLOWED_EMAILS"] = " Alice@Example.com ,bob@example.com,"
	e["ASOBELL_ALLOWED_DOMAINS"] = "@Example.org,example.net"
	e["ASOBELL_COOKIE_SECURE"] = "false"
	e["ASOBELL_DEV_LOGIN"] = "true"
	e["ASOBELL_JOBS_WORKERS"] = "8"
	e["ASOBELL_RPC_TLS_CERT"] = "c.pem"
	e["ASOBELL_RPC_TLS_KEY"] = "k.pem"
	e["ASOBELL_RPC_TLS_CA"] = "ca.pem"

	cfg, err := config.Load(e)
	require.NoError(t, err)
	assert.Equal(t, ":18080", cfg.HTTPAddr)
	assert.Equal(t, []string{"alice@example.com", "bob@example.com"}, cfg.AllowedEmails)
	assert.Equal(t, []string{"example.org", "example.net"}, cfg.AllowedDomains)
	assert.False(t, cfg.CookieSecure)
	assert.True(t, cfg.DevLogin)
	assert.Equal(t, 8, cfg.JobsWorkers)
	assert.True(t, cfg.RPCTLS.Enabled())
	assert.Equal(t, "ca.pem", cfg.RPCTLS.CAFile)
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]string)
		want   string
	}{
		{
			name:   "missing required",
			mutate: func(e map[string]string) { delete(e, "ASOBELL_PROVIDER_ADDR") },
			want:   "ASOBELL_PROVIDER_ADDR",
		},
		{
			name:   "short token",
			mutate: func(e map[string]string) { e["ASOBELL_RPC_TOKEN"] = "short" },
			want:   "ASOBELL_RPC_TOKEN",
		},
		{
			name:   "relative base url",
			mutate: func(e map[string]string) { e["ASOBELL_BASE_URL"] = "/console" },
			want:   "ASOBELL_BASE_URL",
		},
		{
			name:   "base url with query",
			mutate: func(e map[string]string) { e["ASOBELL_BASE_URL"] = "https://x.example?a=1" },
			want:   "ASOBELL_BASE_URL",
		},
		{
			name:   "bad mongo uri",
			mutate: func(e map[string]string) { e["ASOBELL_MONGO_URI"] = "postgres://x" },
			want:   "ASOBELL_MONGO_URI",
		},
		{
			name:   "partial tls",
			mutate: func(e map[string]string) { e["ASOBELL_RPC_TLS_CERT"] = "c.pem" },
			want:   "ASOBELL_RPC_TLS_CERT",
		},
		{
			name:   "zero duration",
			mutate: func(e map[string]string) { e["ASOBELL_JOBS_LEASE"] = "0s" },
			want:   "ASOBELL_JOBS_LEASE",
		},
		{
			name:   "bad duration",
			mutate: func(e map[string]string) { e["ASOBELL_SESSION_TTL"] = "week" },
			want:   "SessionTTL",
		},
		{
			name:   "zero workers",
			mutate: func(e map[string]string) { e["ASOBELL_JOBS_WORKERS"] = "0" },
			want:   "ASOBELL_JOBS_WORKERS",
		},
		{
			name:   "bad log level",
			mutate: func(e map[string]string) { e["ASOBELL_LOG_LEVEL"] = "loud" },
			want:   "ASOBELL_LOG_LEVEL",
		},
		{
			name:   "bad log format",
			mutate: func(e map[string]string) { e["ASOBELL_LOG_FORMAT"] = "xml" },
			want:   "ASOBELL_LOG_FORMAT",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := minimalEnv()
			tc.mutate(e)
			_, err := config.Load(e)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestValidateCollectsAllErrors(t *testing.T) {
	e := minimalEnv()
	e["ASOBELL_RPC_TOKEN"] = "short"
	e["ASOBELL_LOG_LEVEL"] = "loud"
	_, err := config.Load(e)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ASOBELL_RPC_TOKEN")
	assert.Contains(t, err.Error(), "ASOBELL_LOG_LEVEL")
}

func TestAllowsEmail(t *testing.T) {
	cfg := &config.Config{AllowedEmails: []string{"alice@example.com"}, AllowedDomains: []string{"example.org"}}
	assert.True(t, cfg.AllowsEmail("Alice@Example.com"))
	assert.True(t, cfg.AllowsEmail("anyone@example.org"))
	assert.False(t, cfg.AllowsEmail("bob@example.com"))
	assert.False(t, cfg.AllowsEmail("not-an-email"))
	assert.False(t, (&config.Config{}).AllowsEmail("alice@example.com"))
}
