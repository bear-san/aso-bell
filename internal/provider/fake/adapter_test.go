package fake_test

import (
	"testing"

	"github.com/bear-san/aso-bell/internal/provider/adapter"
	"github.com/bear-san/aso-bell/internal/provider/adaptertest"
	"github.com/bear-san/aso-bell/internal/provider/fake"
)

func TestAdapterContract(t *testing.T) {
	t.Parallel()

	adaptertest.RunContract(t, func(*testing.T) adapter.Adapter { return fake.NewAdapter() })
}
