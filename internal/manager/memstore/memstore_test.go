package memstore_test

import (
	"testing"

	"github.com/bear-san/aso-bell/internal/manager/memstore"
	"github.com/bear-san/aso-bell/internal/manager/storetest"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
)

// TestContract は memstore が Mongo 実装と同じ契約を満たすことを確かめる。
// Fake の挙動が実装から乖離すると usecase のテストが実態と合わなくなるため(docs/13 §3)。
func TestContract(t *testing.T) {
	t.Parallel()

	storetest.RunContract(t, func(*testing.T) usecase.Repository { return memstore.New() })
}
