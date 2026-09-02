package mongo

import (
	"context"
	"errors"
	"fmt"

	"server/services/usercenter/domain"
	"server/services/usercenter/repository"

	"go.mongodb.org/mongo-driver/bson"
	driverMongo "go.mongodb.org/mongo-driver/mongo"
)

type IdentityRepository struct {
	collection *driverMongo.Collection
}

func NewIdentityRepository(collection *driverMongo.Collection) *IdentityRepository {
	return &IdentityRepository{
		collection: collection,
	}
}

func (r *IdentityRepository) EnsureIndexes(ctx context.Context) error {
	if r == nil {
		return ErrCollectionRequired
	}
	return ensureIndexes(ctx, r.collection, identityIndexes())
}

func (r *IdentityRepository) Create(ctx context.Context, identity *domain.Identity) error {
	if r == nil || r.collection == nil {
		return ErrCollectionRequired
	}
	document, err := identityDocumentFromDomain(identity)
	if err != nil {
		return err
	}
	if _, err := r.collection.InsertOne(ctx, document); err != nil {
		if driverMongo.IsDuplicateKeyError(err) {
			return fmt.Errorf("create identity: duplicate key: %w", err)
		}
		return fmt.Errorf("create identity: %w", err)
	}
	return nil
}

func (r *IdentityRepository) Find(ctx context.Context, provider domain.AuthProvider, subject string) (*domain.Identity, error) {
	if r == nil || r.collection == nil {
		return nil, ErrCollectionRequired
	}
	if provider == "" || subject == "" {
		return nil, domain.ErrInvalidIdentity
	}
	var document identityDocument
	if err := r.collection.FindOne(ctx, bson.M{"provider": string(provider), "subject": subject}).Decode(&document); err != nil {
		if errors.Is(err, driverMongo.ErrNoDocuments) {
			return nil, domain.ErrIdentityNotFound
		}
		return nil, fmt.Errorf("find identity: %w", err)
	}
	identity := identityDomainFromDocument(document)
	return &identity, nil
}

var _ repository.IdentityRepository = (*IdentityRepository)(nil)
