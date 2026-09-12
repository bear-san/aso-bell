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

type jobRepo struct {
	coll *mongo.Collection
}

// Enqueue はジョブを登録する。dedupeKey が重複した場合は domain.ErrAlreadyExists を返す。
func (r *jobRepo) Enqueue(ctx context.Context, job domain.Job, now time.Time) (*domain.Job, error) {
	doc := jobDoc{
		ID:        bson.NewObjectID(),
		Kind:      string(job.Kind),
		RunAt:     utc(job.RunAt),
		DedupeKey: job.DedupeKey,
		Payload: jobPayloadDoc{
			EventID:         job.Payload.EventID,
			ParticipationID: job.Payload.ParticipationID,
			Sequence:        job.Payload.Sequence,
			ScheduledFor:    utc(job.Payload.ScheduledFor),
		},
		Status:      string(domain.JobPending),
		Attempts:    0,
		MaxAttempts: job.MaxAttempts,
		LastError:   "",
		CreatedAt:   utc(now),
		UpdatedAt:   utc(now),
	}

	if doc.MaxAttempts <= 0 {
		doc.MaxAttempts = domain.DefaultMaxAttempts
	}

	if job.EventID != "" {
		eventID, err := objectID(job.EventID)
		if err != nil {
			return nil, err
		}

		doc.EventID = &eventID
	}

	if _, err := r.coll.InsertOne(ctx, doc); err != nil {
		return nil, mapWriteError(err, "enqueue job")
	}

	return decodeJob(doc), nil
}

// Get は ID でジョブを取得する。
func (r *jobRepo) Get(ctx context.Context, id string) (*domain.Job, error) {
	oid, err := objectID(id)
	if err != nil {
		return nil, err
	}

	var doc jobDoc
	if findErr := r.coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&doc); findErr != nil {
		return nil, mapNotFound(findErr, "get job")
	}

	return decodeJob(doc), nil
}

// Claim は実行可能なジョブを 1 件だけ排他的に取得する。リースが切れた running も再取得の対象にする。
func (r *jobRepo) Claim(ctx context.Context, now time.Time, lease time.Duration) (*domain.Job, error) {
	at := utc(now)
	filter := bson.M{"$or": []bson.M{
		{"status": string(domain.JobPending), "runAt": bson.M{"$lte": at}},
		{"status": string(domain.JobRunning), "leaseUntil": bson.M{"$lte": at}},
	}}
	update := bson.M{
		"$set": bson.M{
			"status":     string(domain.JobRunning),
			"leaseUntil": at.Add(lease),
			"updatedAt":  at,
		},
		"$inc": bson.M{"attempts": 1},
	}
	opts := options.FindOneAndUpdate().
		SetSort(bson.D{{Key: "runAt", Value: 1}}).
		SetReturnDocument(options.After)

	var doc jobDoc
	if err := r.coll.FindOneAndUpdate(ctx, filter, update, opts).Decode(&doc); err != nil {
		return nil, mapNotFound(err, "claim job")
	}

	return decodeJob(doc), nil
}

// Complete はジョブを完了にする。
func (r *jobRepo) Complete(ctx context.Context, id string, now time.Time) error {
	at := utc(now)
	set := bson.M{
		"status":     string(domain.JobDone),
		"finishedAt": at,
		"leaseUntil": nil,
		"updatedAt":  at,
	}

	return r.update(ctx, id, bson.M{"$set": set}, "complete job")
}

// Retry は backoff 後の再実行を予約する。
func (r *jobRepo) Retry(ctx context.Context, id string, runAt time.Time, lastErr string, now time.Time) error {
	at := utc(now)
	set := bson.M{
		"status":     string(domain.JobPending),
		"runAt":      utc(runAt),
		"lastError":  lastErr,
		"leaseUntil": nil,
		"updatedAt":  at,
	}

	return r.update(ctx, id, bson.M{"$set": set}, "retry job")
}

// Fail はジョブを恒久的な失敗として打ち切る。
func (r *jobRepo) Fail(ctx context.Context, id, lastErr string, now time.Time) error {
	at := utc(now)
	set := bson.M{
		"status":     string(domain.JobFailed),
		"finishedAt": at,
		"lastError":  lastErr,
		"leaseUntil": nil,
		"updatedAt":  at,
	}

	return r.update(ctx, id, bson.M{"$set": set}, "fail job")
}

// Reschedule は前提が変わったジョブを別時刻へ積み直す。
// 実行そのものは行っていないため、Claim で増えた attempts を戻して再試行回数を消費しない。
func (r *jobRepo) Reschedule(ctx context.Context, id string, runAt time.Time, now time.Time) error {
	at := utc(now)
	update := bson.M{
		"$set": bson.M{
			"status":     string(domain.JobPending),
			"runAt":      utc(runAt),
			"leaseUntil": nil,
			"updatedAt":  at,
		},
		"$inc": bson.M{"attempts": -1},
	}

	return r.update(ctx, id, update, "reschedule job")
}

// ExtendLease は実行中ジョブのリースを延長する。
func (r *jobRepo) ExtendLease(ctx context.Context, id string, until time.Time, now time.Time) error {
	oid, err := objectID(id)
	if err != nil {
		return err
	}

	filter := bson.M{"_id": oid, "status": string(domain.JobRunning)}
	update := bson.M{"$set": bson.M{"leaseUntil": utc(until), "updatedAt": utc(now)}}

	res, err := r.coll.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("extend job lease: %w", err)
	}

	if res.MatchedCount == 0 {
		return domain.ErrNotFound
	}

	return nil
}

// CancelByEvent はイベントに紐づく pending ジョブをまとめて取り消し、取り消した件数を返す。
func (r *jobRepo) CancelByEvent(
	ctx context.Context,
	eventID string,
	kinds []domain.JobKind,
	now time.Time,
) (int, error) {
	oid, err := objectID(eventID)
	if err != nil {
		return 0, err
	}

	filter := bson.M{"eventId": oid, "status": string(domain.JobPending)}
	if len(kinds) > 0 {
		values := make([]string, 0, len(kinds))
		for _, k := range kinds {
			values = append(values, string(k))
		}

		filter["kind"] = bson.M{"$in": values}
	}

	at := utc(now)
	update := bson.M{"$set": bson.M{"status": string(domain.JobCanceled), "finishedAt": at, "updatedAt": at}}

	res, err := r.coll.UpdateMany(ctx, filter, update)
	if err != nil {
		return 0, fmt.Errorf("cancel jobs by event: %w", err)
	}

	return int(res.ModifiedCount), nil
}

// ListByEvent はイベントに紐づくジョブを実行予定順で返す。statuses を指定すると状態で絞り込む。
func (r *jobRepo) ListByEvent(
	ctx context.Context,
	eventID string,
	statuses ...domain.JobStatus,
) ([]*domain.Job, error) {
	return listByEvent(ctx, r.coll, eventID, statuses, "runAt", decodeJob, "list jobs by event")
}

func (r *jobRepo) update(ctx context.Context, id string, update bson.M, op string) error {
	oid, err := objectID(id)
	if err != nil {
		return err
	}

	res, err := r.coll.UpdateOne(ctx, bson.M{"_id": oid}, update)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	if res.MatchedCount == 0 {
		return domain.ErrNotFound
	}

	return nil
}
