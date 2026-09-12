package mongo

import (
	"context"
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/bear-san/aso-bell/internal/manager/domain"
)

const (
	metaProviderID = "provider"
	metaSchemaID   = "schema"
)

type metaRepo struct {
	coll *mongo.Collection
}

// GetProvider は記録済みの Provider 状態を返す。未記録なら domain.ErrNotFound。
func (r *metaRepo) GetProvider(ctx context.Context) (*domain.ProviderState, error) {
	var doc providerDoc
	if err := r.coll.FindOne(ctx, bson.M{"_id": metaProviderID}).Decode(&doc); err != nil {
		return nil, mapNotFound(err, "get provider")
	}

	return decodeProvider(doc), nil
}

// SaveProvider は Provider 状態を upsert する。firstSeenAt は初回のみ記録する。
func (r *metaRepo) SaveProvider(ctx context.Context, state *domain.ProviderState) error {
	set := bson.M{
		"kind":    string(state.Kind),
		"address": state.Address,
		"capabilities": capabilitiesDoc{
			Forms:         state.Capabilities.Forms,
			Ephemeral:     state.Capabilities.Ephemeral,
			DirectMessage: state.Capabilities.DirectMessage,
		},
		"version":    state.Version,
		"botUserId":  state.BotUserID,
		"connected":  state.Connected,
		"status":     string(state.Status),
		"failures":   state.Failures,
		"lastSeenAt": utc(state.LastSeenAt),
		"updatedAt":  utc(state.UpdatedAt),
	}

	update := bson.M{
		"$set":         set,
		"$setOnInsert": bson.M{"firstSeenAt": utc(state.FirstSeenAt)},
	}

	if _, err := r.coll.UpdateOne(
		ctx,
		bson.M{"_id": metaProviderID},
		update,
		options.UpdateOne().SetUpsert(true),
	); err != nil {
		return fmt.Errorf("save provider: %w", err)
	}

	return nil
}

// SchemaVersion は記録済みのスキーマバージョンを返す。未記録なら 0。
func (r *metaRepo) SchemaVersion(ctx context.Context) (int, error) {
	var doc schemaDoc
	if err := r.coll.FindOne(ctx, bson.M{"_id": metaSchemaID}).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return 0, nil
		}

		return 0, fmt.Errorf("get schema version: %w", err)
	}

	return doc.Version, nil
}

// SetSchemaVersion はスキーマバージョンを記録する。
func (r *metaRepo) SetSchemaVersion(ctx context.Context, version int) error {
	update := bson.M{"$set": bson.M{"version": version}}

	if _, err := r.coll.UpdateOne(
		ctx,
		bson.M{"_id": metaSchemaID},
		update,
		options.UpdateOne().SetUpsert(true),
	); err != nil {
		return fmt.Errorf("set schema version: %w", err)
	}

	return nil
}
