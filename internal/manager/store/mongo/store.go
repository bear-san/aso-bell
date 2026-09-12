// Package mongo は usecase のリポジトリポートを MongoDB で実装する(docs/05-database.md)。
// 複数ドキュメントのトランザクションは使わず、単一ドキュメントの原子的更新で構成する。
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

// コレクション名(docs/05-database.md §2)。
const (
	collMeta           = "meta"
	collWorkspaces     = "workspaces"
	collEvents         = "events"
	collParticipations = "participations"
	collJobs           = "jobs"
	collReminderLogs   = "reminder_logs"
	collConsoleUsers   = "console_users"
	collChatIdentities = "chat_identities"
	collLinkTokens     = "link_tokens"
	collSessions       = "sessions"
	collOAuthStates    = "oauth_states"
)

// SchemaVersion は現在のスキーマバージョン。
const SchemaVersion = 1

// Store は MongoDB 上のリポジトリ実装をまとめたもの。
type Store struct {
	db *mongo.Database

	meta           *metaRepo
	workspaces     *workspaceRepo
	events         *eventRepo
	participations *participationRepo
	jobs           *jobRepo
	reminderLogs   *reminderLogRepo
	consoleUsers   *consoleUserRepo
	chatIdentities *chatIdentityRepo
	linkTokens     *linkTokenRepo
	sessions       *sessionRepo
	oauthStates    *oauthStateRepo
}

// New は Database に紐づく Store を作る。
func New(db *mongo.Database) *Store {
	return &Store{
		db:             db,
		meta:           &metaRepo{coll: db.Collection(collMeta)},
		workspaces:     &workspaceRepo{coll: db.Collection(collWorkspaces)},
		events:         &eventRepo{coll: db.Collection(collEvents)},
		participations: &participationRepo{coll: db.Collection(collParticipations)},
		jobs:           &jobRepo{coll: db.Collection(collJobs)},
		reminderLogs:   &reminderLogRepo{coll: db.Collection(collReminderLogs)},
		consoleUsers:   &consoleUserRepo{coll: db.Collection(collConsoleUsers)},
		chatIdentities: &chatIdentityRepo{coll: db.Collection(collChatIdentities)},
		linkTokens:     &linkTokenRepo{coll: db.Collection(collLinkTokens)},
		sessions:       &sessionRepo{coll: db.Collection(collSessions)},
		oauthStates:    &oauthStateRepo{coll: db.Collection(collOAuthStates)},
	}
}

// Meta はメタ情報リポジトリを返す。
func (s *Store) Meta() usecase.MetaRepository { return s.meta }

// Workspaces はワークスペースリポジトリを返す。
func (s *Store) Workspaces() usecase.WorkspaceRepository { return s.workspaces }

// Events はイベントリポジトリを返す。
func (s *Store) Events() usecase.EventRepository { return s.events }

// Participations は参加リポジトリを返す。
func (s *Store) Participations() usecase.ParticipationRepository { return s.participations }

// Jobs はジョブリポジトリを返す。
func (s *Store) Jobs() usecase.JobRepository { return s.jobs }

// ReminderLogs はリマインド履歴リポジトリを返す。
func (s *Store) ReminderLogs() usecase.ReminderLogRepository { return s.reminderLogs }

// ConsoleUsers は WebConsole 利用者リポジトリを返す。
func (s *Store) ConsoleUsers() usecase.ConsoleUserRepository { return s.consoleUsers }

// ChatIdentities は連携リポジトリを返す。
func (s *Store) ChatIdentities() usecase.ChatIdentityRepository { return s.chatIdentities }

// LinkTokens は連携トークンリポジトリを返す。
func (s *Store) LinkTokens() usecase.LinkTokenRepository { return s.linkTokens }

// Sessions はセッションリポジトリを返す。
func (s *Store) Sessions() usecase.SessionRepository { return s.sessions }

// OAuthStates は OAuth state リポジトリを返す。
func (s *Store) OAuthStates() usecase.OAuthStateRepository { return s.oauthStates }

func objectID(id string) (bson.ObjectID, error) {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return bson.NilObjectID, fmt.Errorf("%w: invalid id %q", domain.ErrNotFound, id)
	}

	return oid, nil
}

func mapNotFound(err error, op string) error {
	if errors.Is(err, mongo.ErrNoDocuments) {
		return domain.ErrNotFound
	}

	return fmt.Errorf("%s: %w", op, err)
}

func mapWriteError(err error, op string) error {
	if mongo.IsDuplicateKeyError(err) {
		return domain.ErrAlreadyExists
	}

	return fmt.Errorf("%s: %w", op, err)
}

func utc(t time.Time) time.Time { return t.UTC() }

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}

	v := t.UTC()

	return &v
}

// listByEvent は eventId とステータスで絞り込んだドキュメントを sortKey の昇順で取得し、ドメイン型へ変換する。
func listByEvent[S ~string, D, T any](
	ctx context.Context,
	coll *mongo.Collection,
	eventID string,
	statuses []S,
	sortKey string,
	decode func(D) T,
	op string,
) ([]T, error) {
	oid, err := objectID(eventID)
	if err != nil {
		return nil, err
	}

	filter := bson.M{"eventId": oid}
	if len(statuses) > 0 {
		values := make([]string, 0, len(statuses))
		for _, s := range statuses {
			values = append(values, string(s))
		}

		filter["status"] = bson.M{"$in": values}
	}

	cur, err := coll.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: sortKey, Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	var docs []D
	if allErr := cur.All(ctx, &docs); allErr != nil {
		return nil, fmt.Errorf("%s: %w", op, allErr)
	}

	out := make([]T, 0, len(docs))
	for _, doc := range docs {
		out = append(out, decode(doc))
	}

	return out, nil
}

var _ usecase.Repository = (*Store)(nil)
