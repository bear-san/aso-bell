package runtime_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bear-san/aso-bell/internal/provider/runtime"
	"github.com/bear-san/aso-bell/internal/shared/rpcauth"
)

func TestDialSucceedsWithoutManager(t *testing.T) {
	t.Parallel()

	conn, err := runtime.Dial(runtime.DialConfig{
		Address: "dns:///manager.invalid:9090",
		Token:   "0123456789abcdef0123456789abcdef",
	})
	require.NoError(t, err, "NewClient は I/O をしないため Manager 未起動でも成功する")
	t.Cleanup(func() { _ = conn.Close() })

	assert.Equal(t, "dns:///manager.invalid:9090", conn.Target())
}

func TestDialRequiresAddress(t *testing.T) {
	t.Parallel()

	_, err := runtime.Dial(runtime.DialConfig{Token: "t"})

	require.Error(t, err)
}

func TestDialRejectsPartialTLSSettings(t *testing.T) {
	t.Parallel()

	_, err := runtime.Dial(runtime.DialConfig{
		Address: "dns:///manager:9090",
		TLS:     rpcauth.TLSFiles{CertFile: "cert.pem"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "transport credentials")
}
