package mongo_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	driver "go.mongodb.org/mongo-driver/v2/mongo"

	store "github.com/bear-san/aso-bell/internal/manager/store/mongo"
	"github.com/bear-san/aso-bell/internal/manager/storetest"
	"github.com/bear-san/aso-bell/internal/manager/testutil"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
)

func newDatabase(t *testing.T) *driver.Database {
	t.Helper()

	db := testutil.StartMongo(t)
	require.NoError(t, store.EnsureIndexes(t.Context(), store.New(db)))

	return db
}

func findIndex(t *testing.T, db *driver.Database, coll string, keys ...string) driver.IndexSpecification {
	t.Helper()

	specs, err := db.Collection(coll).Indexes().ListSpecifications(t.Context())
	require.NoError(t, err)

	for _, spec := range specs {
		if matchesKeys(spec, keys) {
			return spec
		}
	}

	require.Failf(t, "index not found", "collection %s has no index on %v", coll, keys)

	return driver.IndexSpecification{}
}

func matchesKeys(spec driver.IndexSpecification, keys []string) bool {
	elems, err := spec.KeysDocument.Elements()
	if err != nil || len(elems) != len(keys) {
		return false
	}

	for i, elem := range elems {
		if elem.Key() != keys[i] {
			return false
		}
	}

	return true
}

func TestStoreContract(t *testing.T) {
	storetest.RunContract(t, func(t *testing.T) usecase.Repository {
		t.Helper()

		db := testutil.StartMongo(t)
		s := store.New(db)
		require.NoError(t, store.EnsureIndexes(t.Context(), s))

		return s
	})
}

func TestEnsureIndexes(t *testing.T) {
	db := newDatabase(t)

	t.Run("idempotent", func(t *testing.T) {
		require.NoError(t, store.EnsureIndexes(t.Context(), store.New(db)))
	})

	t.Run("unique", func(t *testing.T) {
		unique := map[string][]string{
			"workspaces":      {"provider", "externalId"},
			"participations":  {"eventId", "chatUserId"},
			"jobs":            {"dedupeKey"},
			"console_users":   {"googleSub"},
			"chat_identities": {"workspaceId", "chatUserId"},
		}

		for coll, keys := range unique {
			spec := findIndex(t, db, coll, keys...)
			assert.True(t, spec.Unique != nil && *spec.Unique, "%s %v should be unique", coll, keys)
		}
	})

	t.Run("sparse dedupe key", func(t *testing.T) {
		// dedupeKey を持たないジョブを一意インデックスで弾かないため sparse が要る(docs/05-database.md §3.5)。
		spec := findIndex(t, db, "jobs", "dedupeKey")
		assert.True(t, spec.Sparse != nil && *spec.Sparse)
	})

	t.Run("ttl", func(t *testing.T) {
		// 実際の削除はタイミング依存のため、インデックスの存在だけを検証する(docs/13-testing.md §4.3)。
		ttl := map[string]string{
			"jobs":         "finishedAt",
			"link_tokens":  "expiresAt",
			"sessions":     "expiresAt",
			"oauth_states": "expiresAt",
		}

		for coll, key := range ttl {
			spec := findIndex(t, db, coll, key)
			assert.NotNil(t, spec.ExpireAfterSeconds, "%s.%s should be a TTL index", coll, key)
		}
	})
}
