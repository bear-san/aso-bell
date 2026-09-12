package runtime

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestWithDefaultsFillsUnsetValues(t *testing.T) {
	t.Parallel()

	got := Config{}.withDefaults()

	assert.NotNil(t, got.Logger)
	assert.Equal(t, DefaultTimeout, got.Timeout)
	assert.Equal(t, DefaultFastTimeout, got.FastTimeout)
	assert.Equal(t, DefaultMinBackoff, got.MinBackoff)
	assert.Equal(t, DefaultMaxBackoff, got.MaxBackoff)
}

func TestWithDefaultsKeepsMaxBackoffAboveMin(t *testing.T) {
	t.Parallel()

	got := Config{MinBackoff: time.Minute, MaxBackoff: time.Second}.withDefaults()

	assert.Equal(t, time.Minute, got.MaxBackoff, "上限が下限を下回ると backoff が縮むため、下限まで引き上げる")
}
