package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bear-san/aso-bell/internal/manager/domain"
)

func TestParseTimeOfDay(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    domain.TimeOfDay
		wantErr bool
	}{
		{name: "evening", in: "19:00", want: domain.TimeOfDay{Hour: 19, Minute: 0}},
		{name: "midnight", in: "00:00", want: domain.TimeOfDay{}},
		{name: "last minute", in: "23:59", want: domain.TimeOfDay{Hour: 23, Minute: 59}},
		{name: "single digit hour", in: "9:00", wantErr: true},
		{name: "hour out of range", in: "24:00", wantErr: true},
		{name: "minute out of range", in: "12:60", wantErr: true},
		{name: "no colon", in: "1900", wantErr: true},
		{name: "empty", in: "", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := domain.ParseTimeOfDay(tc.in)
			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.in, got.String())
		})
	}
}

func TestTimeOfDayOn(t *testing.T) {
	t.Parallel()

	loc, err := time.LoadLocation("Asia/Tokyo")
	require.NoError(t, err)

	day := time.Date(2026, time.September, 20, 3, 0, 0, 0, time.UTC)
	got := domain.TimeOfDay{Hour: 19, Minute: 30}.On(day, loc)

	assert.Equal(t, time.Date(2026, time.September, 20, 19, 30, 0, 0, loc), got)
}
