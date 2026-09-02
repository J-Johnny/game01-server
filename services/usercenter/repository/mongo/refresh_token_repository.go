package mongo

import (
	"context"
	"errors"
	"fmt"
	"time"

	commonmongo "server/common/mongodb"
	"server/services/usercenter/domain"
	"server/services/usercenter/repository"

	"go.mongodb.org/mongo-driver/bson"
	driverMongo "go.mongodb.org/mongo-driver/mongo"
)

type RefreshTokenRepository struct {
	collection *driverMongo.Collection
	unitOfWork commonmongo.UnitOfWork
}

func NewRefreshTokenRepository(collection *driverMongo.Collection, unitOfWork commonmongo.UnitOfWork) *RefreshTokenRepository {
	return &RefreshTokenRepository{
		collection: collection,
		unitOfWork: unitOfWork,
	}
}

func (r *RefreshTokenRepository) EnsureIndexes(ctx context.Context) error {
	if r == nil {
		return ErrCollectionRequired
	}
	return ensureIndexes(ctx, r.collection, refreshTokenIndexes())
}

func (r *RefreshTokenRepository) Create(ctx context.Context, token *domain.RefreshToken) error {
	if r == nil || r.collection == nil {
		return ErrCollectionRequired
	}
	document, err := refreshTokenDocumentFromDomain(token)
	if err != nil {
		return err
	}
	if _, err := r.collection.InsertOne(ctx, document); err != nil {
		if driverMongo.IsDuplicateKeyError(err) {
			return fmt.Errorf("create refresh token: duplicate key: %w", err)
		}
		return fmt.Errorf("create refresh token: %w", err)
	}
	return nil
}

func (r *RefreshTokenRepository) FindValid(ctx context.Context, tokenHash string, now time.Time) (*domain.RefreshToken, error) {
	if r == nil || r.collection == nil {
		return nil, ErrCollectionRequired
	}
	if tokenHash == "" {
		return nil, domain.ErrInvalidToken
	}
	var document refreshTokenDocument
	err := r.collection.FindOne(ctx, bson.M{"token_hash": tokenHash, "revoked_at": nil, "expires_at": bson.M{"$gt": now}}).Decode(&document)
	if errors.Is(err, driverMongo.ErrNoDocuments) {
		return nil, domain.ErrInvalidToken
	}
	if err != nil {
		return nil, fmt.Errorf("find refresh token: %w", err)
	}
	token := refreshTokenDomainFromDocument(document)
	return &token, nil
}

func (r *RefreshTokenRepository) Revoke(ctx context.Context, tokenID string, now time.Time) error {
	if r == nil || r.collection == nil {
		return ErrCollectionRequired
	}
	result, err := r.collection.UpdateOne(ctx, bson.M{"token_id": tokenID, "revoked_at": nil}, bson.M{"$set": bson.M{"revoked_at": now}})
	if err != nil {
		return fmt.Errorf("revoke refresh token: %w", err)
	}
	if result.MatchedCount == 0 {
		return domain.ErrInvalidToken
	}
	return nil
}

func (r *RefreshTokenRepository) Rotate(ctx context.Context, tokenID string, now time.Time, replacement *domain.RefreshToken) error {
	if r == nil || r.collection == nil {
		return ErrCollectionRequired
	}
	if r.unitOfWork == nil {
		return ErrTransactionRequired
	}
	if tokenID == "" || replacement == nil {
		return domain.ErrInvalidToken
	}
	replacementDocument, err := refreshTokenDocumentFromDomain(replacement)
	if err != nil {
		return err
	}
	return r.unitOfWork.Execute(ctx, func(transactionContext context.Context) error {
		result, updateErr := r.collection.UpdateOne(transactionContext, bson.M{"token_id": tokenID, "revoked_at": nil, "expires_at": bson.M{"$gt": now}}, bson.M{"$set": bson.M{"revoked_at": now}})
		if updateErr != nil {
			return fmt.Errorf("rotate refresh token revoke: %w", updateErr)
		}
		if result.MatchedCount != 1 {
			return domain.ErrInvalidToken
		}
		if _, insertErr := r.collection.InsertOne(transactionContext, replacementDocument); insertErr != nil {
			return fmt.Errorf("rotate refresh token insert: %w", insertErr)
		}
		return nil
	})
}

var _ repository.RefreshTokenRepository = (*RefreshTokenRepository)(nil)
