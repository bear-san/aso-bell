// Package config は Provider(Slack / Discord)の環境変数・コマンドライン引数を読み込み、検証する。
package config

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"

	"github.com/bear-san/aso-bell/internal/shared/logging"
	"github.com/bear-san/aso-bell/internal/shared/rpcauth"
)

// Common は provider-slack / provider-discord の両方に共通する設定。
type Common struct {
	ManagerAddr    string `env:"ASOBELL_MANAGER_ADDR,required"`
	RPCToken       string `env:"ASOBELL_RPC_TOKEN,required"`
	RPCTLS         rpcauth.TLSFiles
	ListenAddr     string        `env:"ASOBELL_PROVIDER_LISTEN_ADDR"  envDefault:":9091"`
	CommandName    string        `env:"ASOBELL_COMMAND_NAME"          envDefault:"asobell"`
	ManagerTimeout time.Duration `env:"ASOBELL_MANAGER_TIMEOUT"       envDefault:"25s"`
	LogLevel       string        `env:"ASOBELL_LOG_LEVEL"             envDefault:"info"`
	LogFormat      string        `env:"ASOBELL_LOG_FORMAT"            envDefault:"json"`
}

// Slack は provider-slack の設定。
type Slack struct {
	Common

	BotToken string `env:"ASOBELL_SLACK_BOT_TOKEN,required"`
	AppToken string `env:"ASOBELL_SLACK_APP_TOKEN,required"`
}

// Discord は provider-discord の設定。
type Discord struct {
	Common

	BotToken      string `env:"ASOBELL_DISCORD_BOT_TOKEN,required"`
	ApplicationID string `env:"ASOBELL_DISCORD_APPLICATION_ID,required"`
}

// Slack / Discord ともコマンド名は英小文字・数字・記号 1〜32 文字に制限されるため、共通の緩い方を採る。
var commandNameRe = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

// LoadSlack は environ(nil なら os.Environ)と args から Slack 設定を構築する。
func LoadSlack(environ map[string]string, args []string) (*Slack, error) {
	var cfg Slack
	if err := load(&cfg, environ, args); err != nil {
		return nil, err
	}
	var errs []error
	if err := cfg.validate(); err != nil {
		errs = append(errs, err)
	}
	if !strings.HasPrefix(cfg.BotToken, "xoxb-") {
		errs = append(errs, errors.New("ASOBELL_SLACK_BOT_TOKEN must start with xoxb-"))
	}
	if !strings.HasPrefix(cfg.AppToken, "xapp-") {
		errs = append(errs, errors.New("ASOBELL_SLACK_APP_TOKEN must start with xapp-"))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// LoadDiscord は environ(nil なら os.Environ)と args から Discord 設定を構築する。
func LoadDiscord(environ map[string]string, args []string) (*Discord, error) {
	var cfg Discord
	if err := load(&cfg, environ, args); err != nil {
		return nil, err
	}
	var errs []error
	if err := cfg.validate(); err != nil {
		errs = append(errs, err)
	}
	if strings.TrimSpace(cfg.BotToken) == "" {
		errs = append(errs, errors.New("ASOBELL_DISCORD_BOT_TOKEN must not be empty"))
	}
	if _, err := parseSnowflake(cfg.ApplicationID); err != nil {
		errs = append(errs, fmt.Errorf("ASOBELL_DISCORD_APPLICATION_ID: %w", err))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func load(cfg any, environ map[string]string, args []string) error {
	merged := make(map[string]string)
	if environ == nil {
		merged = env.ToMap(os.Environ())
	} else {
		maps.Copy(merged, environ)
	}
	allowed := make(map[string]struct{})
	envNames(reflect.TypeOf(cfg).Elem(), allowed)
	if err := applyArgs(merged, args, allowed); err != nil {
		return err
	}
	if err := env.ParseWithOptions(cfg, env.Options{Environment: merged}); err != nil {
		return fmt.Errorf("parse env: %w", err)
	}
	return nil
}

func (c *Common) validate() error {
	var errs []error
	if len(c.RPCToken) < rpcauth.MinTokenLength {
		errs = append(errs, fmt.Errorf("ASOBELL_RPC_TOKEN must be at least %d bytes", rpcauth.MinTokenLength))
	}
	if err := c.RPCTLS.Validate(); err != nil {
		errs = append(errs, err)
	}
	if c.ListenAddr == "" {
		errs = append(errs, errors.New("ASOBELL_PROVIDER_LISTEN_ADDR must not be empty"))
	}
	if !commandNameRe.MatchString(c.CommandName) {
		errs = append(errs, fmt.Errorf("ASOBELL_COMMAND_NAME %q must match %s", c.CommandName, commandNameRe))
	}
	if c.ManagerTimeout <= 0 {
		errs = append(errs, errors.New("ASOBELL_MANAGER_TIMEOUT must be positive"))
	}
	if _, err := logging.ParseLevel(c.LogLevel); err != nil {
		errs = append(errs, fmt.Errorf("ASOBELL_LOG_LEVEL: %w", err))
	}
	if err := logging.ValidateFormat(c.LogFormat); err != nil {
		errs = append(errs, fmt.Errorf("ASOBELL_LOG_FORMAT: %w", err))
	}
	return errors.Join(errs...)
}

func parseSnowflake(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) < 15 || len(s) > 22 {
		return "", errors.New("must be a Discord snowflake (15-22 digits)")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return "", errors.New("must contain digits only")
		}
	}
	return s, nil
}
