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
		{"引数なし", nil, exitUsage},
		{"未知のコマンド", []string{"bogus"}, exitUsage},
		{"version", []string{"version"}, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, run(tc.args))
		})
	}
}
