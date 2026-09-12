package logging_test

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bear-san/aso-bell/internal/shared/logging"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		in      string
		want    slog.Level
		wantErr bool
	}{
		{in: "debug", want: slog.LevelDebug},
		{in: "INFO", want: slog.LevelInfo},
		{in: " warn ", want: slog.LevelWarn},
		{in: "warning", want: slog.LevelWarn},
		{in: "error", want: slog.LevelError},
		{in: "verbose", wantErr: true},
		{in: "", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := logging.ParseLevel(tc.in)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestNew(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		var buf bytes.Buffer
		l, err := logging.New(&buf, "info", "json")
		require.NoError(t, err)
		l.Info("hello", "event_id", "e1")
		l.Debug("hidden")
		assert.Contains(t, buf.String(), `"msg":"hello"`)
		assert.Contains(t, buf.String(), `"event_id":"e1"`)
		assert.NotContains(t, buf.String(), "hidden")
	})
	t.Run("text", func(t *testing.T) {
		var buf bytes.Buffer
		l, err := logging.New(&buf, "debug", "text")
		require.NoError(t, err)
		l.Debug("shown")
		assert.Contains(t, buf.String(), "msg=shown")
	})
	t.Run("invalid format", func(t *testing.T) {
		_, err := logging.New(&bytes.Buffer{}, "info", "xml")
		require.Error(t, err)
	})
}
