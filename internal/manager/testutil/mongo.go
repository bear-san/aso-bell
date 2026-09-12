package testutil

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

// MongoImage は本番と同じメジャーバージョンに固定する(docs/03 §1.3)。
const MongoImage = "mongo:7"

const (
	startTimeout = 2 * time.Minute
	// Mongo の DB 名は 64 バイト未満。サフィックスの余地を残して切り詰める。
	dbNameMaxLen = 40
	dbNameSalt   = 1_000_000
)

// StartMongoURI は testcontainers で MongoDB を起動し、接続 URI とテストごとに固有の DB 名を返す。
// 設定から自分で接続する側(app など)のテストで使う。
// -short 指定時はスキップし、Docker 無しでも単体テストが回るようにする。
func StartMongoURI(t *testing.T) (string, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping testcontainers test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	defer cancel()

	ctr, err := mongodb.Run(ctx, MongoImage)
	testcontainers.CleanupContainer(t, ctr)
	require.NoError(t, err, "start mongo container")

	uri, err := ctr.ConnectionString(ctx)
	require.NoError(t, err)

	return uri, dbName(t)
}

// StartMongo は testcontainers で MongoDB を起動し、テストごとに固有の Database を返す。
func StartMongo(t *testing.T) *mongo.Database {
	t.Helper()

	uri, name := StartMongoURI(t)

	ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	defer cancel()

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Disconnect(context.Background()) })
	require.NoError(t, client.Ping(ctx, readpref.Primary()))

	return client.Database(name)
}

// dbName は Mongo の DB 名制約(64 バイト未満、/ \ . " $ 空白 禁止)に合わせてテスト名を整形する。
func dbName(t *testing.T) string {
	r := strings.NewReplacer("/", "_", "\\", "_", ".", "_", "\"", "_", "$", "_", " ", "_")
	name := "test_" + r.Replace(t.Name())
	if len(name) > dbNameMaxLen {
		name = name[:dbNameMaxLen]
	}
	return fmt.Sprintf("%s_%d", name, time.Now().UnixNano()%dbNameSalt)
}
