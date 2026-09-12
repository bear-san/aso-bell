package mongo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
)

type participationRepo struct {
	coll *mongo.Collection
}

// Upsert は (eventId, chatUserId) を単一の原子的更新で登録・更新し、更新前後のレコードを返す。
// 参加済みユーザーがチラ見を押しても降格しないよう、役割の決定は更新パイプライン内で行う。
func (r *participationRepo) Upsert(
	ctx context.Context,
	in usecase.ParticipationUpsert,
) (usecase.ParticipationChange, error) {
	eventID, err := objectID(in.EventID)
	if err != nil {
		return usecase.ParticipationChange{}, err
	}

	workspaceID, err := objectID(in.WorkspaceID)
	if err != nil {
		return usecase.ParticipationChange{}, err
	}

	filter := bson.M{"eventId": eventID, "chatUserId": in.ChatUserID}
	pipeline := participationUpdatePipeline(in, eventID, workspaceID)
	opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.Before)

	var change usecase.ParticipationChange

	var beforeDoc participationDoc

	switch decErr := r.coll.FindOneAndUpdate(ctx, filter, pipeline, opts).Decode(&beforeDoc); {
	case decErr == nil:
		change.Before = decodeParticipation(beforeDoc)
	case errors.Is(decErr, mongo.ErrNoDocuments):
	default:
		return usecase.ParticipationChange{}, mapWriteError(decErr, "upsert participation")
	}

	after, err := r.GetByUser(ctx, in.EventID, in.ChatUserID)
	if err != nil {
		return usecase.ParticipationChange{}, err
	}

	change.After = after

	return change, nil
}

// Get は ID で参加レコードを取得する。
func (r *participationRepo) Get(ctx context.Context, id string) (*domain.Participation, error) {
	oid, err := objectID(id)
	if err != nil {
		return nil, err
	}

	var doc participationDoc
	if findErr := r.coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&doc); findErr != nil {
		return nil, mapNotFound(findErr, "get participation")
	}

	return decodeParticipation(doc), nil
}

// GetByUser はイベントとチャットユーザーの組で参加レコードを取得する。
func (r *participationRepo) GetByUser(ctx context.Context, eventID, chatUserID string) (*domain.Participation, error) {
	oid, err := objectID(eventID)
	if err != nil {
		return nil, err
	}

	filter := bson.M{"eventId": oid, "chatUserId": chatUserID}

	var doc participationDoc
	if findErr := r.coll.FindOne(ctx, filter).Decode(&doc); findErr != nil {
		return nil, mapNotFound(findErr, "get participation by user")
	}

	return decodeParticipation(doc), nil
}

// SetStatus は参加レコードの状態を更新する。active 以外にする場合はチラ見の期限も消す。
func (r *participationRepo) SetStatus(
	ctx context.Context,
	id string,
	status domain.ParticipationStatus,
	now time.Time,
) error {
	oid, err := objectID(id)
	if err != nil {
		return err
	}

	set := bson.M{"status": string(status), "updatedAt": utc(now)}
	if status != domain.ParticipationActive {
		set["expiresAt"] = nil
	}

	res, err := r.coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": set})
	if err != nil {
		return fmt.Errorf("set participation status: %w", err)
	}

	if res.MatchedCount == 0 {
		return domain.ErrNotFound
	}

	return nil
}

// ListByEvent はイベントの参加レコードを参加順で返す。statuses を指定すると状態で絞り込む。
func (r *participationRepo) ListByEvent(
	ctx context.Context,
	eventID string,
	statuses ...domain.ParticipationStatus,
) ([]*domain.Participation, error) {
	return listByEvent(ctx, r.coll, eventID, statuses, "joinedAt", decodeParticipation, "list participations")
}

func (r *participationRepo) CountActive(
	ctx context.Context,
	eventID string,
	role domain.ParticipationRole,
) (int, error) {
	oid, err := objectID(eventID)
	if err != nil {
		return 0, err
	}

	filter := bson.M{"eventId": oid, "status": string(domain.ParticipationActive)}
	if role != "" {
		filter["role"] = string(role)
	}

	n, err := r.coll.CountDocuments(ctx, filter)
	if err != nil {
		return 0, fmt.Errorf("count participations: %w", err)
	}

	return int(n), nil
}

func participationUpdatePipeline(
	in usecase.ParticipationUpsert,
	eventID, workspaceID bson.ObjectID,
) mongo.Pipeline {
	now := utc(in.Now)
	keepsParticipant := bson.M{"$and": bson.A{
		bson.M{"$eq": bson.A{"$role", string(domain.RoleParticipant)}},
		bson.M{"$eq": bson.A{"$status", string(domain.ParticipationActive)}},
	}}

	var requestedExpiry any
	if in.ExpiresAt != nil {
		requestedExpiry = in.ExpiresAt.UTC()
	}

	peekIncrement := 0
	if in.Role == domain.RolePeeker {
		peekIncrement = 1
	}

	return mongo.Pipeline{bson.D{{Key: "$set", Value: bson.M{
		"eventId":     eventID,
		"workspaceId": workspaceID,
		"chatUserId":  in.ChatUserID,
		"displayName": in.DisplayName,
		"joinedAt":    bson.M{"$ifNull": bson.A{"$joinedAt", now}},
		"status":      string(domain.ParticipationActive),
		"role":        bson.M{"$cond": bson.A{keepsParticipant, string(domain.RoleParticipant), string(in.Role)}},
		"expiresAt":   bson.M{"$cond": bson.A{keepsParticipant, "$expiresAt", requestedExpiry}},
		"peekCount": bson.M{"$add": bson.A{
			bson.M{"$ifNull": bson.A{"$peekCount", 0}},
			bson.M{"$cond": bson.A{keepsParticipant, 0, peekIncrement}},
		}},
		"updatedAt": now,
	}}}}
}
