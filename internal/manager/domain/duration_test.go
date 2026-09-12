package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bear-san/aso-bell/internal/manager/domain"
)

func TestParseDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    time.Duration
		wantErr bool
	}{
		{name: "hours", in: "3h", want: 3 * time.Hour},
		{name: "compound", in: "1h30m", want: 90 * time.Minute},
		{name: "days", in: "7d", want: 168 * time.Hour},
		{name: "days and hours", in: "1d12h", want: 36 * time.Hour},
		{name: "fractional days", in: "1.5d", want: 36 * time.Hour},
		{name: "negative days", in: "-2d", want: -48 * time.Hour},
		{name: "spaces trimmed", in: "  3h  ", want: 3 * time.Hour},
		{name: "empty", in: "", wantErr: true},
		{name: "bare unit", in: "d", wantErr: true},
		{name: "unknown unit", in: "3y", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := domain.ParseDuration(tc.in)
			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestFormatDuration(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "168h0m0s", domain.FormatDuration(168*time.Hour))
}

func TestIsID(t *testing.T) {
	t.Parallel()

	assert.True(t, domain.IsID("66e0a1b2c3d4e5f607182930"))
	assert.True(t, domain.IsID("66E0A1B2C3D4E5F607182930"))
	assert.False(t, domain.IsID("66e0a1b2c3d4e5f60718293"))
	assert.False(t, domain.IsID("66e0a1b2c3d4e5f60718293g"))
	assert.False(t, domain.IsID(""))
}
