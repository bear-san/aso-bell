package mongo

import (
	"context"
	"fmt"
	"maps"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/bear-san/aso-bell/internal/manager/domain"
)

type reminderLogRepo struct {
	coll *mongo.Collection
}

// Append はリマインド送信結果を記録する。
func (r *reminderLogRepo) Append(ctx context.Context, log *domain.ReminderLog) (*domain.ReminderLog, error) {
	eventID, err := objectID(log.EventID)
	if err != nil {
		return nil, err
	}

	doc := reminderLogDoc{
		ID:           bson.NewObjectID(),
		EventID:      eventID,
		Sequence:     log.Sequence,
		ScheduledFor: utc(log.ScheduledFor),
		SentAt:       utc(log.SentAt),
	}

	for _, t := range log.Targets {
		doc.Targets = append(doc.Targets, reminderTargetDoc{
			Kind:      string(t.Kind),
			ChannelID: t.ChannelID,
			MessageID: t.MessageID,
			OK:        t.OK,
			Error:     t.Error,
		})
	}

	if _, insErr := r.coll.InsertOne(ctx, doc); insErr != nil {
		return nil, mapWriteError(insErr, "append reminder log")
	}

	return decodeReminderLog(doc), nil
}

// ListByEvent はイベントのリマインド履歴を送信順で返す。
func (r *reminderLogRepo) ListByEvent(ctx context.Context, eventID string) ([]*domain.ReminderLog, error) {
	oid, err := objectID(eventID)
	if err != nil {
		return nil, err
	}

	cur, err := r.coll.Find(ctx, bson.M{"eventId": oid}, options.Find().SetSort(bson.D{{Key: "sentAt", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("list reminder logs: %w", err)
	}

	var docs []reminderLogDoc
	if allErr := cur.All(ctx, &docs); allErr != nil {
		return nil, fmt.Errorf("list reminder logs: %w", allErr)
	}

	out := make([]*domain.ReminderLog, 0, len(docs))
	for _, doc := range docs {
		out = append(out, decodeReminderLog(doc))
	}

	return out, nil
}

// SentAt は同じ予定時刻のリマインドが送信済みかを返す。ポリシー再生成時の二重送信を防ぐ。
func (r *reminderLogRepo) SentAt(ctx context.Context, eventID string, scheduledFor time.Time) (bool, error) {
	return r.exists(ctx, eventID, bson.M{"scheduledFor": utc(scheduledFor)}, "check reminder schedule")
}

// SentSequence は同じ連番のリマインドが送信済みかを返す。
func (r *reminderLogRepo) SentSequence(ctx context.Context, eventID string, sequence int) (bool, error) {
	return r.exists(ctx, eventID, bson.M{"sequence": sequence}, "check reminder sequence")
}

func (r *reminderLogRepo) exists(ctx context.Context, eventID string, extra bson.M, op string) (bool, error) {
	oid, err := objectID(eventID)
	if err != nil {
		return false, err
	}

	filter := bson.M{"eventId": oid}
	maps.Copy(filter, extra)

	n, err := r.coll.CountDocuments(ctx, filter, options.Count().SetLimit(1))
	if err != nil {
		return false, fmt.Errorf("%s: %w", op, err)
	}

	return n > 0, nil
}
