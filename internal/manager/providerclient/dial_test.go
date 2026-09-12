package providerclient_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bear-san/aso-bell/internal/manager/providerclient"
	"github.com/bear-san/aso-bell/internal/shared/rpcauth"
)

func TestDialSucceedsWithoutProvider(t *testing.T) {
	conn, err := providerclient.Dial(providerclient.DialConfig{
		Address: "dns:///provider.invalid:9091",
		Token:   "0123456789abcdef0123456789abcdef",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	assert.Equal(t, "dns:///provider.invalid:9091", conn.Target())
}

func TestDialRequiresAddress(t *testing.T) {
	_, err := providerclient.Dial(providerclient.DialConfig{Token: "t"})

	require.Error(t, err)
}

func TestDialRejectsPartialTLSSettings(t *testing.T) {
	_, err := providerclient.Dial(providerclient.DialConfig{
		Address: "dns:///provider:9091",
		TLS:     rpcauth.TLSFiles{CertFile: "cert.pem"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "transport credentials")
}
