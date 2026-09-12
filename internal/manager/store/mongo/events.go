package mongo

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/bear-san/aso-bell/internal/manager/domain"
	"github.com/bear-san/aso-bell/internal/manager/usecase"
)

const (
	defaultEventPageSize = 20
	maxEventPageSize     = 100
	cursorParts          = 2
)

type eventRepo struct {
	coll *mongo.Collection
}

// Create はイベントを挿入し、採番された ID 付きで返す。
func (r *eventRepo) Create(ctx context.Context, ev *domain.Event) (*domain.Event, error) {
	doc, err := encodeEvent(ev)
	if err != nil {
		return nil, err
	}

	doc.ID = bson.NewObjectID()

	if _, insErr := r.coll.InsertOne(ctx, doc); insErr != nil {
		return nil, mapWriteError(insErr, "create event")
	}

	return decodeEvent(doc)
}

// Get は ID でイベントを取得する。
func (r *eventRepo) Get(ctx context.Context, id string) (*domain.Event, error) {
	oid, err := objectID(id)
	if err != nil {
		return nil, err
	}

	return r.findOne(ctx, bson.M{"_id": oid}, "get event")
}

// GetByChannelID はイベントチャンネル ID でイベントを取得する。
func (r *eventRepo) GetByChannelID(ctx context.Context, channelID string) (*domain.Event, error) {
	return r.findOne(ctx, bson.M{"channel.channelId": channelID}, "get event by channel")
}

// List は startsAt の降順でイベントを返す。NextCursor が空でなければ続きがある。
func (r *eventRepo) List(ctx context.Context, filter usecase.EventFilter) (usecase.EventPage, error) {
	query, err := buildEventQuery(filter)
	if err != nil {
		return usecase.EventPage{}, err
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = defaultEventPageSize
	}

	limit = min(limit, maxEventPageSize)

	opts := options.Find().
		SetSort(bson.D{{Key: "startsAt", Value: -1}, {Key: "_id", Value: -1}}).
		SetLimit(int64(limit) + 1)

	cur, err := r.coll.Find(ctx, query, opts)
	if err != nil {
		return usecase.EventPage{}, fmt.Errorf("list events: %w", err)
	}

	var docs []eventDoc
	if allErr := cur.All(ctx, &docs); allErr != nil {
		return usecase.EventPage{}, fmt.Errorf("list events: %w", allErr)
	}

	page := usecase.EventPage{}

	if len(docs) > limit {
		last := docs[limit-1]
		page.NextCursor = encodeCursor(last.StartsAt, last.ID)
		docs = docs[:limit]
	}

	page.Events = make([]*domain.Event, 0, len(docs))

	for _, doc := range docs {
		ev, decErr := decodeEvent(doc)
		if decErr != nil {
			return usecase.EventPage{}, decErr
		}

		page.Events = append(page.Events, ev)
	}

	return page, nil
}

// Update は編集・終了で変わるフィールドを保存する。チャンネルとメッセージ参照は対象外。
func (r *eventRepo) Update(ctx context.Context, ev *domain.Event) error {
	oid, err := objectID(ev.ID)
	if err != nil {
		return err
	}

	set := bson.M{
		"title":          ev.Title,
		"description":    ev.Description,
		"location":       ev.Location,
		"startsAt":       utc(ev.StartsAt),
		"endsAt":         utcPtr(ev.EndsAt),
		"status":         string(ev.Status),
		"reminderPolicy": encodeReminderPolicy(ev.ReminderPolicy),
		"endedAt":        utcPtr(ev.EndedAt),
		"endReason":      string(ev.EndReason),
		"updatedAt":      utc(ev.UpdatedAt),
	}

	return r.updateOne(ctx, oid, bson.M{"$set": set}, "update event")
}

// SetChannel は Provider 側で作成したチャンネル参照を保存する。
func (r *eventRepo) SetChannel(ctx context.Context, id string, channel domain.ChannelRef, now time.Time) error {
	oid, err := objectID(id)
	if err != nil {
		return err
	}

	set := bson.M{
		"channel": channelRefDoc{
			ChannelID: channel.ChannelID,
			Name:      channel.Name,
			Archived:  channel.Archived,
		},
		"updatedAt": utc(now),
	}

	return r.updateOne(ctx, oid, bson.M{"$set": set}, "set event channel")
}

// SetMessages は投稿済みメッセージの参照を保存する。
func (r *eventRepo) SetMessages(ctx context.Context, id string, messages domain.MessageRefs, now time.Time) error {
	oid, err := objectID(id)
	if err != nil {
		return err
	}

	set := bson.M{"messages": encodeMessageRefs(messages), "updatedAt": utc(now)}

	return r.updateOne(ctx, oid, bson.M{"$set": set}, "set event messages")
}

// AddParticipantCount は表示用の参加者数を増減する。
func (r *eventRepo) AddParticipantCount(ctx context.Context, id string, delta int, now time.Time) error {
	oid, err := objectID(id)
	if err != nil {
		return err
	}

	update := bson.M{
		"$inc": bson.M{"participantCount": delta},
		"$set": bson.M{"updatedAt": utc(now)},
	}

	return r.updateOne(ctx, oid, update, "add participant count")
}

func (r *eventRepo) findOne(ctx context.Context, filter bson.M, op string) (*domain.Event, error) {
	var doc eventDoc
	if err := r.coll.FindOne(ctx, filter).Decode(&doc); err != nil {
		return nil, mapNotFound(err, op)
	}

	return decodeEvent(doc)
}

func (r *eventRepo) updateOne(ctx context.Context, oid bson.ObjectID, update bson.M, op string) error {
	res, err := r.coll.UpdateOne(ctx, bson.M{"_id": oid}, update)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	if res.MatchedCount == 0 {
		return domain.ErrNotFound
	}

	return nil
}

func buildEventQuery(filter usecase.EventFilter) (bson.M, error) {
	query := bson.M{}

	if filter.WorkspaceID != "" {
		oid, err := objectID(filter.WorkspaceID)
		if err != nil {
			return nil, err
		}

		query["workspaceId"] = oid
	}

	if len(filter.Statuses) > 0 {
		statuses := make([]string, 0, len(filter.Statuses))
		for _, s := range filter.Statuses {
			statuses = append(statuses, string(s))
		}

		query["status"] = bson.M{"$in": statuses}
	}

	if len(filter.IDs) > 0 {
		oids, err := objectIDs(filter.IDs)
		if err != nil {
			return nil, err
		}

		query["_id"] = bson.M{"$in": oids}
	}

	if filter.StartsAfter != nil {
		query["startsAt"] = bson.M{"$gte": filter.StartsAfter.UTC()}
	}

	if filter.Cursor != "" {
		startsAt, id, err := decodeCursor(filter.Cursor)
		if err != nil {
			return nil, err
		}

		query["$or"] = []bson.M{
			{"startsAt": bson.M{"$lt": startsAt}},
			{"startsAt": startsAt, "_id": bson.M{"$lt": id}},
		}
	}

	return query, nil
}

func encodeCursor(startsAt time.Time, id bson.ObjectID) string {
	raw := strconv.FormatInt(startsAt.UTC().UnixMilli(), 10) + "|" + id.Hex()

	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(cursor string) (time.Time, bson.ObjectID, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, bson.NilObjectID, fmt.Errorf("decode cursor: %w", err)
	}

	parts := strings.SplitN(string(raw), "|", cursorParts)
	if len(parts) != cursorParts {
		return time.Time{}, bson.NilObjectID, fmt.Errorf("decode cursor: malformed %q", cursor)
	}

	millis, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, bson.NilObjectID, fmt.Errorf("decode cursor: %w", err)
	}

	id, err := bson.ObjectIDFromHex(parts[1])
	if err != nil {
		return time.Time{}, bson.NilObjectID, fmt.Errorf("decode cursor: %w", err)
	}

	return time.UnixMilli(millis).UTC(), id, nil
}
