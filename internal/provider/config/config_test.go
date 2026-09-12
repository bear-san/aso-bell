package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bear-san/aso-bell/internal/provider/config"
)

const token = "0123456789abcdef0123456789abcdef"

func slackEnv() map[string]string {
	return map[string]string{
		"ASOBELL_MANAGER_ADDR":    "dns:///manager:9090",
		"ASOBELL_RPC_TOKEN":       token,
		"ASOBELL_SLACK_BOT_TOKEN": "xoxb-1",
		"ASOBELL_SLACK_APP_TOKEN": "xapp-1",
	}
}

func discordEnv() map[string]string {
	return map[string]string{
		"ASOBELL_MANAGER_ADDR":           "dns:///manager:9090",
		"ASOBELL_RPC_TOKEN":              token,
		"ASOBELL_DISCORD_BOT_TOKEN":      "bot-token",
		"ASOBELL_DISCORD_APPLICATION_ID": "123456789012345678",
	}
}

func TestLoadSlackDefaults(t *testing.T) {
	cfg, err := config.LoadSlack(slackEnv(), nil)
	require.NoError(t, err)
	assert.Equal(t, ":9091", cfg.ListenAddr)
	assert.Equal(t, "asobell", cfg.CommandName)
	assert.Equal(t, 25*time.Second, cfg.ManagerTimeout)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, "json", cfg.LogFormat)
	assert.Equal(t, "xoxb-1", cfg.BotToken)
}

func TestLoadDiscordDefaults(t *testing.T) {
	cfg, err := config.LoadDiscord(discordEnv(), nil)
	require.NoError(t, err)
	assert.Equal(t, "123456789012345678", cfg.ApplicationID)
	assert.Equal(t, "dns:///manager:9090", cfg.ManagerAddr)
}

func TestArgsOverrideEnv(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "bot.txt")
	require.NoError(t, os.WriteFile(tokenFile, []byte("xoxb-from-file\n"), 0o600))

	e := slackEnv()
	e["ASOBELL_COMMAND_NAME"] = "fromenv"
	cfg, err := config.LoadSlack(e, []string{
		"--manager-addr=dns:///other:9090",
		"--command-name", "fromargs",
		"--slack-bot-token-file", tokenFile,
		"--log-level=debug",
	})
	require.NoError(t, err)
	assert.Equal(t, "dns:///other:9090", cfg.ManagerAddr)
	assert.Equal(t, "fromargs", cfg.CommandName)
	assert.Equal(t, "xoxb-from-file", cfg.BotToken)
	assert.Equal(t, "debug", cfg.LogLevel)
}

func TestArgsErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "unknown flag", args: []string{"--nope=1"}, want: "unknown flag --nope"},
		{name: "positional", args: []string{"serve"}, want: "unexpected argument"},
		{name: "missing value", args: []string{"--command-name"}, want: "requires a value"},
		{
			name: "missing file",
			args: []string{"--slack-app-token-file=/nonexistent"},
			want: "read --slack-app-token-file",
		},
		{name: "unknown file flag", args: []string{"--nope-file=/x"}, want: "unknown flag --nope"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := config.LoadSlack(slackEnv(), tc.args)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestLoadSlackErrors(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]string)
		want   string
	}{
		{
			name:   "missing manager addr",
			mutate: func(e map[string]string) { delete(e, "ASOBELL_MANAGER_ADDR") },
			want:   "ASOBELL_MANAGER_ADDR",
		},
		{
			name:   "short token",
			mutate: func(e map[string]string) { e["ASOBELL_RPC_TOKEN"] = "x" },
			want:   "ASOBELL_RPC_TOKEN",
		},
		{
			name:   "bad bot token prefix",
			mutate: func(e map[string]string) { e["ASOBELL_SLACK_BOT_TOKEN"] = "xoxp-1" },
			want:   "xoxb-",
		},
		{
			name:   "bad app token prefix",
			mutate: func(e map[string]string) { e["ASOBELL_SLACK_APP_TOKEN"] = "xoxb-1" },
			want:   "xapp-",
		},
		{
			name:   "bad command name",
			mutate: func(e map[string]string) { e["ASOBELL_COMMAND_NAME"] = "Aso Bell" },
			want:   "ASOBELL_COMMAND_NAME",
		},
		{
			name:   "long command name",
			mutate: func(e map[string]string) { e["ASOBELL_COMMAND_NAME"] = strings.Repeat("a", 33) },
			want:   "ASOBELL_COMMAND_NAME",
		},
		{
			name:   "zero timeout",
			mutate: func(e map[string]string) { e["ASOBELL_MANAGER_TIMEOUT"] = "0" },
			want:   "ASOBELL_MANAGER_TIMEOUT",
		},
		{
			name:   "partial tls",
			mutate: func(e map[string]string) { e["ASOBELL_RPC_TLS_KEY"] = "k.pem" },
			want:   "ASOBELL_RPC_TLS_CERT",
		},
		{
			name:   "bad log level",
			mutate: func(e map[string]string) { e["ASOBELL_LOG_LEVEL"] = "loud" },
			want:   "ASOBELL_LOG_LEVEL",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := slackEnv()
			tc.mutate(e)
			_, err := config.LoadSlack(e, nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestLoadDiscordErrors(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]string)
		want   string
	}{
		{
			name:   "empty bot token",
			mutate: func(e map[string]string) { e["ASOBELL_DISCORD_BOT_TOKEN"] = " " },
			want:   "ASOBELL_DISCORD_BOT_TOKEN",
		},
		{
			name:   "non numeric app id",
			mutate: func(e map[string]string) { e["ASOBELL_DISCORD_APPLICATION_ID"] = "12345678901234567x" },
			want:   "ASOBELL_DISCORD_APPLICATION_ID",
		},
		{
			name:   "short app id",
			mutate: func(e map[string]string) { e["ASOBELL_DISCORD_APPLICATION_ID"] = "123" },
			want:   "ASOBELL_DISCORD_APPLICATION_ID",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := discordEnv()
			tc.mutate(e)
			_, err := config.LoadDiscord(e, nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}
