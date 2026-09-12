package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRunExitCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want int
	}{
		{"version", []string{"version"}, 0},
		{"未知のコマンド", []string{"bogus"}, exitUsage},
		{"設定不足", []string{"serve"}, exitError},
		{"フラグのみは serve 扱い", []string{"--manager-addr=127.0.0.1:1"}, exitError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, run(tc.args))
		})
	}
}

func TestIsFlag(t *testing.T) {
	t.Parallel()

	assert.True(t, isFlag("--manager-addr"))
	assert.False(t, isFlag("serve"))
	assert.False(t, isFlag(""))
}
