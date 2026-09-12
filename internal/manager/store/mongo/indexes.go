package mongo

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// 完了ジョブは 30 日で自動削除する(docs/05-database.md §3.5)。
const jobRetentionSeconds = 30 * 24 * 60 * 60

// EnsureIndexes は全コレクションのインデックスを作成する。冪等なので起動時とテストの両方で呼べる。
func EnsureIndexes(ctx context.Context, s *Store) error {
	specs := map[string][]mongo.IndexModel{
		collWorkspaces: {
			{
				Keys:    bson.D{{Key: "provider", Value: 1}, {Key: "externalId", Value: 1}},
				Options: options.Index().SetUnique(true),
			},
		},
		collEvents: {
			{Keys: bson.D{{Key: "workspaceId", Value: 1}, {Key: "status", Value: 1}, {Key: "startsAt", Value: 1}}},
			{Keys: bson.D{{Key: "startsAt", Value: -1}, {Key: "_id", Value: -1}}},
			{Keys: bson.D{{Key: "channel.channelId", Value: 1}}},
			{Keys: bson.D{{Key: "messages.announcement.messageId", Value: 1}}},
		},
		collParticipations: {
			{
				Keys:    bson.D{{Key: "eventId", Value: 1}, {Key: "chatUserId", Value: 1}},
				Options: options.Index().SetUnique(true),
			},
			{Keys: bson.D{{Key: "eventId", Value: 1}, {Key: "status", Value: 1}}},
		},
		collJobs: {
			{Keys: bson.D{{Key: "status", Value: 1}, {Key: "runAt", Value: 1}}},
			{Keys: bson.D{{Key: "dedupeKey", Value: 1}}, Options: options.Index().SetUnique(true).SetSparse(true)},
			{Keys: bson.D{{Key: "eventId", Value: 1}, {Key: "status", Value: 1}}},
			{
				Keys:    bson.D{{Key: "finishedAt", Value: 1}},
				Options: options.Index().SetExpireAfterSeconds(jobRetentionSeconds),
			},
		},
		collReminderLogs: {
			{Keys: bson.D{{Key: "eventId", Value: 1}, {Key: "sequence", Value: 1}}},
			{Keys: bson.D{{Key: "eventId", Value: 1}, {Key: "scheduledFor", Value: 1}}},
		},
		collConsoleUsers: {
			{Keys: bson.D{{Key: "googleSub", Value: 1}}, Options: options.Index().SetUnique(true)},
			{Keys: bson.D{{Key: "email", Value: 1}}},
		},
		collChatIdentities: {
			{
				Keys:    bson.D{{Key: "workspaceId", Value: 1}, {Key: "chatUserId", Value: 1}},
				Options: options.Index().SetUnique(true),
			},
			{Keys: bson.D{{Key: "consoleUserId", Value: 1}}},
		},
		collLinkTokens: {
			{Keys: bson.D{{Key: "expiresAt", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0)},
		},
		collSessions: {
			{Keys: bson.D{{Key: "expiresAt", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0)},
			{Keys: bson.D{{Key: "userId", Value: 1}}},
		},
		collOAuthStates: {
			{Keys: bson.D{{Key: "expiresAt", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0)},
		},
	}

	for name, models := range specs {
		if _, err := s.db.Collection(name).Indexes().CreateMany(ctx, models); err != nil {
			return fmt.Errorf("ensure indexes on %s: %w", name, err)
		}
	}

	return nil
}
