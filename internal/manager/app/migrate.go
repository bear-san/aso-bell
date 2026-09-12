package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/bear-san/aso-bell/internal/manager/config"
	mongostore "github.com/bear-san/aso-bell/internal/manager/store/mongo"
)

// Migrate はインデックス作成とスキーマバージョンの記録だけを行って終了する(`manager migrate`)。
// EnsureIndexes は冪等なので、serve の前に何度実行してもよい。
func Migrate(ctx context.Context, cfg *config.Config, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}

	client, store, err := connectMongo(ctx, cfg)
	if err != nil {
		return err
	}

	defer disconnect(context.WithoutCancel(ctx), client, logger)

	return migrateStore(ctx, store, logger)
}

func migrateStore(ctx context.Context, store *mongostore.Store, logger *slog.Logger) error {
	if err := mongostore.EnsureIndexes(ctx, store); err != nil {
		return fmt.Errorf("ensure indexes: %w", err)
	}

	current, err := store.Meta().SchemaVersion(ctx)
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	if current == mongostore.SchemaVersion {
		logger.InfoContext(ctx, "schema up to date", "schema_version", current)

		return nil
	}

	if err = store.Meta().SetSchemaVersion(ctx, mongostore.SchemaVersion); err != nil {
		return fmt.Errorf("write schema version: %w", err)
	}

	logger.InfoContext(ctx, "schema migrated", "from", current, "to", mongostore.SchemaVersion)

	return nil
}
