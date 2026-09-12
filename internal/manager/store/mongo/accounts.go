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
)

type consoleUserRepo struct {
	coll *mongo.Collection
}

type chatIdentityRepo struct {
	coll *mongo.Collection
}

type linkTokenRepo struct {
	coll *mongo.Collection
}

type sessionRepo struct {
	coll *mongo.Collection
}

type oauthStateRepo struct {
	coll *mongo.Collection
}

// Get は ID で WebConsole 利用者を取得する。
func (r *consoleUserRepo) Get(ctx context.Context, id string) (*domain.ConsoleUser, error) {
	oid, err := objectID(id)
	if err != nil {
		return nil, err
	}

	var doc consoleUserDoc
	if findErr := r.coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&doc); findErr != nil {
		return nil, mapNotFound(findErr, "get console user")
	}

	return decodeConsoleUser(doc), nil
}

// GetByGoogleSub は Google の sub クレームで利用者を取得する。
func (r *consoleUserRepo) GetByGoogleSub(ctx context.Context, sub string) (*domain.ConsoleUser, error) {
	var doc consoleUserDoc
	if err := r.coll.FindOne(ctx, bson.M{"googleSub": sub}).Decode(&doc); err != nil {
		return nil, mapNotFound(err, "get console user by google sub")
	}

	return decodeConsoleUser(doc), nil
}

// Create は利用者を作る。googleSub が重複した場合は domain.ErrAlreadyExists を返す。
func (r *consoleUserRepo) Create(ctx context.Context, user *domain.ConsoleUser) (*domain.ConsoleUser, error) {
	doc := consoleUserDoc{
		ID:          bson.NewObjectID(),
		GoogleSub:   user.GoogleSub,
		Email:       user.Email,
		Name:        user.Name,
		Picture:     user.Picture,
		CreatedVia:  string(user.CreatedVia),
		CreatedAt:   utc(user.CreatedAt),
		LastLoginAt: utc(user.LastLoginAt),
	}

	if _, err := r.coll.InsertOne(ctx, doc); err != nil {
		return nil, mapWriteError(err, "create console user")
	}

	return decodeConsoleUser(doc), nil
}

// TouchLogin は最終ログイン時刻を更新する。
func (r *consoleUserRepo) TouchLogin(ctx context.Context, id string, now time.Time) error {
	oid, err := objectID(id)
	if err != nil {
		return err
	}

	res, err := r.coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": bson.M{"lastLoginAt": utc(now)}})
	if err != nil {
		return fmt.Errorf("touch console user login: %w", err)
	}

	if res.MatchedCount == 0 {
		return domain.ErrNotFound
	}

	return nil
}

// Link はチャットユーザーと利用者を紐づける。既に別の利用者へ紐づいていれば
// domain.ErrIdentityLinkedElsewhere を返す。
func (r *chatIdentityRepo) Link(ctx context.Context, identity *domain.ChatIdentity) (*domain.ChatIdentity, error) {
	consoleUserID, err := objectID(identity.ConsoleUserID)
	if err != nil {
		return nil, err
	}

	workspaceID, err := objectID(identity.WorkspaceID)
	if err != nil {
		return nil, err
	}

	doc := chatIdentityDoc{
		ID:                  bson.NewObjectID(),
		ConsoleUserID:       consoleUserID,
		WorkspaceID:         workspaceID,
		Provider:            string(identity.Provider),
		WorkspaceExternalID: identity.WorkspaceExternalID,
		ChatUserID:          identity.ChatUserID,
		DisplayName:         identity.DisplayName,
		LinkedAt:            utc(identity.LinkedAt),
	}

	if _, insErr := r.coll.InsertOne(ctx, doc); insErr != nil {
		if mongo.IsDuplicateKeyError(insErr) {
			return nil, domain.ErrIdentityLinkedElsewhere
		}

		return nil, fmt.Errorf("link chat identity: %w", insErr)
	}

	return decodeChatIdentity(doc), nil
}

// Get はワークスペースとチャットユーザーの組で連携を取得する。
func (r *chatIdentityRepo) Get(ctx context.Context, workspaceID, chatUserID string) (*domain.ChatIdentity, error) {
	oid, err := objectID(workspaceID)
	if err != nil {
		return nil, err
	}

	filter := bson.M{"workspaceId": oid, "chatUserId": chatUserID}

	var doc chatIdentityDoc
	if findErr := r.coll.FindOne(ctx, filter).Decode(&doc); findErr != nil {
		return nil, mapNotFound(findErr, "get chat identity")
	}

	return decodeChatIdentity(doc), nil
}

// ListByUser は利用者に紐づく連携を連携日時の昇順で返す。
func (r *chatIdentityRepo) ListByUser(ctx context.Context, consoleUserID string) ([]*domain.ChatIdentity, error) {
	oid, err := objectID(consoleUserID)
	if err != nil {
		return nil, err
	}

	opts := options.Find().SetSort(bson.D{{Key: "linkedAt", Value: 1}})

	cur, err := r.coll.Find(ctx, bson.M{"consoleUserId": oid}, opts)
	if err != nil {
		return nil, fmt.Errorf("list chat identities: %w", err)
	}

	var docs []chatIdentityDoc
	if allErr := cur.All(ctx, &docs); allErr != nil {
		return nil, fmt.Errorf("list chat identities: %w", allErr)
	}

	out := make([]*domain.ChatIdentity, 0, len(docs))
	for _, doc := range docs {
		out = append(out, decodeChatIdentity(doc))
	}

	return out, nil
}

// Unlink は連携を削除する。
func (r *chatIdentityRepo) Unlink(ctx context.Context, id string) error {
	oid, err := objectID(id)
	if err != nil {
		return err
	}

	res, err := r.coll.DeleteOne(ctx, bson.M{"_id": oid})
	if err != nil {
		return fmt.Errorf("unlink chat identity: %w", err)
	}

	if res.DeletedCount == 0 {
		return domain.ErrNotFound
	}

	return nil
}

// Create は連携トークンを保存する。ID はトークンのハッシュで、平文は保存しない。
func (r *linkTokenRepo) Create(ctx context.Context, token domain.LinkToken) error {
	workspaceID, err := objectID(token.WorkspaceID)
	if err != nil {
		return err
	}

	doc := linkTokenDoc{
		ID:          token.ID,
		WorkspaceID: workspaceID,
		ChatUserID:  token.ChatUserID,
		DisplayName: token.DisplayName,
		ExpiresAt:   utc(token.ExpiresAt),
	}

	if _, insErr := r.coll.InsertOne(ctx, doc); insErr != nil {
		return mapWriteError(insErr, "create link token")
	}

	return nil
}

// Get はトークンを消費せずに取得する。連携ページの表示に使う。
func (r *linkTokenRepo) Get(ctx context.Context, id string) (*domain.LinkToken, error) {
	var doc linkTokenDoc
	if err := r.coll.FindOne(ctx, bson.M{"_id": id}).Decode(&doc); err != nil {
		return nil, mapNotFound(err, "get link token")
	}

	return decodeLinkToken(doc), nil
}

// Consume は未消費かつ有効期限内のトークンを原子的に消費する。
// 対象が無い場合は、消費済みか期限切れかを読み直して区別する。
func (r *linkTokenRepo) Consume(ctx context.Context, id string, now time.Time) (*domain.LinkToken, error) {
	at := utc(now)
	filter := bson.M{"_id": id, "consumedAt": nil, "expiresAt": bson.M{"$gt": at}}
	update := bson.M{"$set": bson.M{"consumedAt": at}}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var doc linkTokenDoc

	err := r.coll.FindOneAndUpdate(ctx, filter, update, opts).Decode(&doc)
	if err == nil {
		return decodeLinkToken(doc), nil
	}

	if !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, fmt.Errorf("consume link token: %w", err)
	}

	existing, getErr := r.Get(ctx, id)
	if getErr != nil {
		return nil, getErr
	}

	if usableErr := existing.Usable(at); usableErr != nil {
		return nil, usableErr
	}

	return nil, domain.ErrNotFound
}

// Create はセッションを保存する。
func (r *sessionRepo) Create(ctx context.Context, session domain.Session) error {
	userID, err := objectID(session.UserID)
	if err != nil {
		return err
	}

	doc := sessionDoc{
		ID:         session.ID,
		UserID:     userID,
		CSRFToken:  session.CSRFToken,
		CreatedAt:  utc(session.CreatedAt),
		ExpiresAt:  utc(session.ExpiresAt),
		LastSeenAt: utc(session.LastSeenAt),
	}

	if _, insErr := r.coll.InsertOne(ctx, doc); insErr != nil {
		return mapWriteError(insErr, "create session")
	}

	return nil
}

// Get はセッションを取得する。
func (r *sessionRepo) Get(ctx context.Context, id string) (*domain.Session, error) {
	var doc sessionDoc
	if err := r.coll.FindOne(ctx, bson.M{"_id": id}).Decode(&doc); err != nil {
		return nil, mapNotFound(err, "get session")
	}

	return &domain.Session{
		ID:         doc.ID,
		UserID:     doc.UserID.Hex(),
		CSRFToken:  doc.CSRFToken,
		CreatedAt:  doc.CreatedAt.UTC(),
		ExpiresAt:  doc.ExpiresAt.UTC(),
		LastSeenAt: doc.LastSeenAt.UTC(),
	}, nil
}

// Touch は最終アクセス時刻を更新する。
func (r *sessionRepo) Touch(ctx context.Context, id string, now time.Time) error {
	res, err := r.coll.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"lastSeenAt": utc(now)}})
	if err != nil {
		return fmt.Errorf("touch session: %w", err)
	}

	if res.MatchedCount == 0 {
		return domain.ErrNotFound
	}

	return nil
}

// Delete はセッションを削除する。
func (r *sessionRepo) Delete(ctx context.Context, id string) error {
	if _, err := r.coll.DeleteOne(ctx, bson.M{"_id": id}); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}

	return nil
}

// DeleteByUser は利用者のセッションをすべて削除する。
func (r *sessionRepo) DeleteByUser(ctx context.Context, userID string) error {
	oid, err := objectID(userID)
	if err != nil {
		return err
	}

	if _, delErr := r.coll.DeleteMany(ctx, bson.M{"userId": oid}); delErr != nil {
		return fmt.Errorf("delete sessions by user: %w", delErr)
	}

	return nil
}

// Create は Google ログインの state を保存する。
func (r *oauthStateRepo) Create(ctx context.Context, state domain.OAuthState) error {
	doc := oauthStateDoc{
		ID:           state.State,
		CodeVerifier: state.CodeVerifier,
		RedirectTo:   state.RedirectTo,
		ExpiresAt:    utc(state.ExpiresAt),
	}

	if _, err := r.coll.InsertOne(ctx, doc); err != nil {
		return mapWriteError(err, "create oauth state")
	}

	return nil
}

// Consume は state を 1 度だけ取り出す。期限切れ・使用済みは domain.ErrNotFound。
func (r *oauthStateRepo) Consume(ctx context.Context, state string, now time.Time) (*domain.OAuthState, error) {
	filter := bson.M{"_id": state, "expiresAt": bson.M{"$gt": utc(now)}}

	var doc oauthStateDoc
	if err := r.coll.FindOneAndDelete(ctx, filter).Decode(&doc); err != nil {
		return nil, mapNotFound(err, "consume oauth state")
	}

	return &domain.OAuthState{
		State:        doc.ID,
		CodeVerifier: doc.CodeVerifier,
		RedirectTo:   doc.RedirectTo,
		ExpiresAt:    doc.ExpiresAt.UTC(),
	}, nil
}
