// Package config は Manager の環境変数設定を読み込み、起動前に検証する。
package config

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"

	"github.com/bear-san/aso-bell/internal/shared/logging"
	"github.com/bear-san/aso-bell/internal/shared/rpcauth"
)

// Config は Manager プロセス全体の設定。docs/12-config-deploy.md §1 と 1:1 に対応する。
type Config struct {
	HTTPAddr     string `env:"ASOBELL_HTTP_ADDR"              envDefault:":8080"`
	RPCAddr      string `env:"ASOBELL_RPC_ADDR"               envDefault:":9090"`
	ProviderAddr string `env:"ASOBELL_PROVIDER_ADDR,required"`
	RPCToken     string `env:"ASOBELL_RPC_TOKEN,required"`
	RPCTLS       rpcauth.TLSFiles

	BaseURL string `env:"ASOBELL_BASE_URL,required"`

	MongoURI string `env:"ASOBELL_MONGO_URI,required"`
	MongoDB  string `env:"ASOBELL_MONGO_DB"           envDefault:"asobell"`

	GoogleClientID     string   `env:"ASOBELL_GOOGLE_CLIENT_ID,required"`
	GoogleClientSecret string   `env:"ASOBELL_GOOGLE_CLIENT_SECRET,required"`
	AllowedEmails      []string `env:"ASOBELL_ALLOWED_EMAILS"`
	AllowedDomains     []string `env:"ASOBELL_ALLOWED_DOMAINS"`

	SessionTTL   time.Duration `env:"ASOBELL_SESSION_TTL"    envDefault:"168h"`
	CookieSecure bool          `env:"ASOBELL_COOKIE_SECURE"  envDefault:"true"`
	LinkTokenTTL time.Duration `env:"ASOBELL_LINK_TOKEN_TTL" envDefault:"10m"`

	ProviderTimeout              time.Duration `env:"ASOBELL_PROVIDER_TIMEOUT"                envDefault:"25s"`
	ProviderPollInterval         time.Duration `env:"ASOBELL_PROVIDER_POLL_INTERVAL"          envDefault:"15s"`
	ProviderOfflineAfterFailures int           `env:"ASOBELL_PROVIDER_OFFLINE_AFTER_FAILURES" envDefault:"3"`

	JobsPollInterval time.Duration `env:"ASOBELL_JOBS_POLL_INTERVAL" envDefault:"10s"`
	JobsBatchSize    int           `env:"ASOBELL_JOBS_BATCH_SIZE"    envDefault:"10"`
	JobsLease        time.Duration `env:"ASOBELL_JOBS_LEASE"         envDefault:"2m"`
	JobsWorkers      int           `env:"ASOBELL_JOBS_WORKERS"       envDefault:"4"`

	LogLevel  string `env:"ASOBELL_LOG_LEVEL"  envDefault:"info"`
	LogFormat string `env:"ASOBELL_LOG_FORMAT" envDefault:"json"`

	DevLogin bool `env:"ASOBELL_DEV_LOGIN" envDefault:"false"`
}

// Load は environ(nil なら os.Environ)から Config を構築し、Validate まで行う。
func Load(environ map[string]string) (*Config, error) {
	var cfg Config
	opts := env.Options{}
	if environ != nil {
		opts.Environment = environ
	}
	if err := env.ParseWithOptions(&cfg, opts); err != nil {
		return nil, fmt.Errorf("parse env: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Validate は値の整合性を検査し、正規化(URL 末尾スラッシュ除去、メール小文字化)を行う。
func (c *Config) Validate() error {
	var errs []error
	if len(c.RPCToken) < rpcauth.MinTokenLength {
		errs = append(errs, fmt.Errorf("ASOBELL_RPC_TOKEN must be at least %d bytes", rpcauth.MinTokenLength))
	}
	if err := c.RPCTLS.Validate(); err != nil {
		errs = append(errs, err)
	}
	if base, err := parseBaseURL(c.BaseURL); err != nil {
		errs = append(errs, fmt.Errorf("ASOBELL_BASE_URL: %w", err))
	} else {
		c.BaseURL = base
	}
	if !strings.HasPrefix(c.MongoURI, "mongodb://") && !strings.HasPrefix(c.MongoURI, "mongodb+srv://") {
		errs = append(errs, errors.New("ASOBELL_MONGO_URI must start with mongodb:// or mongodb+srv://"))
	}
	if c.MongoDB == "" {
		errs = append(errs, errors.New("ASOBELL_MONGO_DB must not be empty"))
	}
	c.AllowedEmails = normalizeList(c.AllowedEmails, "")
	c.AllowedDomains = normalizeList(c.AllowedDomains, "@")

	for name, d := range map[string]time.Duration{
		"ASOBELL_SESSION_TTL":            c.SessionTTL,
		"ASOBELL_LINK_TOKEN_TTL":         c.LinkTokenTTL,
		"ASOBELL_PROVIDER_TIMEOUT":       c.ProviderTimeout,
		"ASOBELL_PROVIDER_POLL_INTERVAL": c.ProviderPollInterval,
		"ASOBELL_JOBS_POLL_INTERVAL":     c.JobsPollInterval,
		"ASOBELL_JOBS_LEASE":             c.JobsLease,
	} {
		if d <= 0 {
			errs = append(errs, fmt.Errorf("%s must be positive", name))
		}
	}
	for name, n := range map[string]int{
		"ASOBELL_PROVIDER_OFFLINE_AFTER_FAILURES": c.ProviderOfflineAfterFailures,
		"ASOBELL_JOBS_BATCH_SIZE":                 c.JobsBatchSize,
		"ASOBELL_JOBS_WORKERS":                    c.JobsWorkers,
	} {
		if n < 1 {
			errs = append(errs, fmt.Errorf("%s must be at least 1", name))
		}
	}
	if _, levelErr := logging.ParseLevel(c.LogLevel); levelErr != nil {
		errs = append(errs, fmt.Errorf("ASOBELL_LOG_LEVEL: %w", levelErr))
	}
	if formatErr := logging.ValidateFormat(c.LogFormat); formatErr != nil {
		errs = append(errs, fmt.Errorf("ASOBELL_LOG_FORMAT: %w", formatErr))
	}
	return errors.Join(errs...)
}

// AllowsEmail は許可リスト(メール・ドメイン)に email が含まれるかを返す。
func (c *Config) AllowsEmail(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	if slices.Contains(c.AllowedEmails, email) {
		return true
	}
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return false
	}
	return slices.Contains(c.AllowedDomains, email[at+1:])
}

func parseBaseURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("must be an absolute http(s) URL")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("must not contain query or fragment")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String(), nil
}

func normalizeList(in []string, trimPrefix string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.ToLower(strings.TrimSpace(v))
		v = strings.TrimPrefix(v, trimPrefix)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}
