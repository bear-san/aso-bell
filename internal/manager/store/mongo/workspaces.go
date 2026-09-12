package mongo

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/bear-san/aso-bell/internal/manager/domain"
)

type workspaceRepo struct {
	coll *mongo.Collection
}

// Get は ID でワークスペースを取得する。
func (r *workspaceRepo) Get(ctx context.Context, id string) (*domain.Workspace, error) {
	oid, err := objectID(id)
	if err != nil {
		return nil, err
	}

	var doc workspaceDoc
	if findErr := r.coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&doc); findErr != nil {
		return nil, mapNotFound(findErr, "get workspace")
	}

	return decodeWorkspace(doc)
}

// GetByExternalID は (provider, externalId) でワークスペースを取得する。
func (r *workspaceRepo) GetByExternalID(
	ctx context.Context,
	provider domain.ProviderKind,
	externalID string,
) (*domain.Workspace, error) {
	filter := bson.M{"provider": string(provider), "externalId": externalID}

	var doc workspaceDoc
	if err := r.coll.FindOne(ctx, filter).Decode(&doc); err != nil {
		return nil, mapNotFound(err, "get workspace by external id")
	}

	return decodeWorkspace(doc)
}

// List は全ワークスペースを名前順で返す。
func (r *workspaceRepo) List(ctx context.Context) ([]*domain.Workspace, error) {
	cur, err := r.coll.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "name", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}

	var docs []workspaceDoc
	if allErr := cur.All(ctx, &docs); allErr != nil {
		return nil, fmt.Errorf("list workspaces: %w", allErr)
	}

	out := make([]*domain.Workspace, 0, len(docs))

	for _, doc := range docs {
		ws, decErr := decodeWorkspace(doc)
		if decErr != nil {
			return nil, decErr
		}

		out = append(out, ws)
	}

	return out, nil
}

// Upsert は (provider, externalId) をキーに登録し、既存なら名前だけを更新する。設定は保持する。
func (r *workspaceRepo) Upsert(
	ctx context.Context,
	ws *domain.Workspace,
	now time.Time,
) (*domain.Workspace, error) {
	filter := bson.M{"provider": string(ws.Provider), "externalId": ws.ExternalID}
	update := bson.M{
		"$set": bson.M{"name": ws.Name, "updatedAt": utc(now)},
		"$setOnInsert": bson.M{
			"settings":  encodeWorkspaceSettings(ws.Settings),
			"createdAt": utc(now),
		},
	}
	opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)

	var doc workspaceDoc
	if err := r.coll.FindOneAndUpdate(ctx, filter, update, opts).Decode(&doc); err != nil {
		return nil, mapWriteError(err, "upsert workspace")
	}

	return decodeWorkspace(doc)
}

// UpdateSettings はワークスペース設定を丸ごと置き換える。
func (r *workspaceRepo) UpdateSettings(
	ctx context.Context,
	id string,
	settings domain.WorkspaceSettings,
	now time.Time,
) (*domain.Workspace, error) {
	oid, err := objectID(id)
	if err != nil {
		return nil, err
	}

	update := bson.M{"$set": bson.M{"settings": encodeWorkspaceSettings(settings), "updatedAt": utc(now)}}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var doc workspaceDoc
	if updErr := r.coll.FindOneAndUpdate(ctx, bson.M{"_id": oid}, update, opts).Decode(&doc); updErr != nil {
		return nil, mapNotFound(updErr, "update workspace settings")
	}

	return decodeWorkspace(doc)
}
