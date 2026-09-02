package mongo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"server/services/usercenter/domain"
	"server/services/usercenter/repository"

	"go.mongodb.org/mongo-driver/bson"
	driverMongo "go.mongodb.org/mongo-driver/mongo"
)

type AccountRepository struct {
	collection *driverMongo.Collection
}

func NewAccountRepository(collection *driverMongo.Collection) *AccountRepository {
	return &AccountRepository{
		collection: collection,
	}
}

func (r *AccountRepository) EnsureIndexes(ctx context.Context) error {
	if r == nil {
		return ErrCollectionRequired
	}
	return ensureIndexes(ctx, r.collection, accountIndexes())
}

func (r *AccountRepository) Create(ctx context.Context, account *domain.Account) error {
	if r == nil || r.collection == nil {
		return ErrCollectionRequired
	}
	document, err := accountDocumentFromDomain(account)
	if err != nil {
		return err
	}
	if _, err := r.collection.InsertOne(ctx, document); err != nil {
		if driverMongo.IsDuplicateKeyError(err) {
			return fmt.Errorf("create account: duplicate key: %w", err)
		}
		return fmt.Errorf("create account: %w", err)
	}
	return nil
}

func (r *AccountRepository) FindByID(ctx context.Context, accountID string) (*domain.Account, error) {
	if r == nil || r.collection == nil {
		return nil, ErrCollectionRequired
	}
	if accountID == "" {
		return nil, domain.ErrInvalidAccount
	}
	var document accountDocument
	if err := r.collection.FindOne(ctx, bson.M{"account_id": accountID}).Decode(&document); err != nil {
		if errors.Is(err, driverMongo.ErrNoDocuments) {
			return nil, domain.ErrAccountNotFound
		}
		return nil, fmt.Errorf("find account: %w", err)
	}
	account, err := accountDomainFromDocument(document, nil)
	if err != nil {
		return nil, err
	}
	return &account, nil
}

func (r *AccountRepository) LinkPlayer(ctx context.Context, accountID, playerID string, now time.Time) error {
	if r == nil || r.collection == nil {
		return ErrCollectionRequired
	}
	if accountID == "" || playerID == "" {
		return domain.ErrInvalidAccount
	}
	// Normalize documents created before PlayerIDs was encoded as an array.
	if _, err := r.collection.UpdateOne(ctx, bson.M{"account_id": accountID, "$or": bson.A{
		bson.M{"player_ids": bson.M{"$exists": false}},
		bson.M{"player_ids": nil},
	}}, bson.M{"$set": bson.M{"player_ids": []string{}}}); err != nil {
		return fmt.Errorf("normalize player links: %w", err)
	}
	result, err := r.collection.UpdateOne(ctx, bson.M{"account_id": accountID}, bson.M{"$addToSet": bson.M{"player_ids": playerID}, "$set": bson.M{"updated_at": now}})
	if err != nil {
		return fmt.Errorf("link player: %w", err)
	}
	if result.MatchedCount == 0 {
		return domain.ErrAccountNotFound
	}
	return nil
}

var _ repository.AccountRepository = (*AccountRepository)(nil)
