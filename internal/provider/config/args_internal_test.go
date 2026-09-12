package config

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnvNamesCoversEmbeddedAndNested(t *testing.T) {
	names := make(map[string]struct{})
	envNames(reflect.TypeFor[Slack](), names)
	for _, want := range []string{"ASOBELL_MANAGER_ADDR", "ASOBELL_RPC_TLS_CA", "ASOBELL_SLACK_BOT_TOKEN", "ASOBELL_LOG_FORMAT"} {
		assert.Contains(t, names, want)
	}
}
