package bot_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bear-san/aso-bell/internal/manager/bot"
	"github.com/bear-san/aso-bell/internal/manager/domain"
)

func TestActionIDRoundTrip(t *testing.T) {
	t.Parallel()

	id := bot.ActionID(domain.ActionPeek, "66e0a1b2c3d4e5f607182930")
	assert.Equal(t, "asobell:peek:66e0a1b2c3d4e5f607182930", id)

	action, eventID, err := bot.ParseActionID(id)
	require.NoError(t, err)
	assert.Equal(t, domain.ActionPeek, action)
	assert.Equal(t, "66e0a1b2c3d4e5f607182930", eventID)
}

func TestParseActionIDRejectsUnknown(t *testing.T) {
	t.Parallel()

	for _, id := range []string{"", "asobell:join", "other:join:1", "asobell:explode:1", "asobell:join:"} {
		t.Run(id, func(t *testing.T) {
			t.Parallel()

			_, _, err := bot.ParseActionID(id)
			require.ErrorIs(t, err, bot.ErrUnknownAction)
		})
	}
}
