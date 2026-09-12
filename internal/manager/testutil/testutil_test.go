package testutil_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/bear-san/aso-bell/internal/manager/testutil"
)

func TestFakeClock(t *testing.T) {
	base := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	c := testutil.NewFakeClock(base)
	assert.Equal(t, base, c.Now())
	c.Advance(90 * time.Minute)
	assert.Equal(t, base.Add(90*time.Minute), c.Now())
	c.Set(base)
	assert.Equal(t, base, c.Now())
}

func TestFakeIDs(t *testing.T) {
	ids := testutil.NewFakeIDs("evt")
	assert.Equal(t, "evt-1", ids.New())
	assert.Equal(t, "evt-2", ids.New())
}

func TestStartMongo(t *testing.T) {
	db := testutil.StartMongo(t)
	ctx := t.Context()
	_, err := db.Collection("probe").InsertOne(ctx, bson.M{"ok": true})
	require.NoError(t, err)
	n, err := db.Collection("probe").CountDocuments(ctx, bson.M{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
}
