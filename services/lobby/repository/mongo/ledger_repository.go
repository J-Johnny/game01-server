package mongo

import (
	"context"
	"errors"
	"fmt"
	"server/services/lobby/domain"
	"server/services/lobby/repository"

	"go.mongodb.org/mongo-driver/bson"
	driverMongo "go.mongodb.org/mongo-driver/mongo"
)

type LedgerRepository struct{ collection *driverMongo.Collection }

func NewLedgerRepository(collection *driverMongo.Collection) *LedgerRepository {
	return &LedgerRepository{collection: collection}
}

func (r *LedgerRepository) EnsureIndexes(ctx context.Context) error {
	return ensureIndexes(ctx, r.collection, ledgerIndexes())
}

func (r *LedgerRepository) Create(ctx context.Context, settlement domain.Settlement) error {
	document, err := ledgerDocumentFromDomain(settlement)
	if err != nil {
		return err
	}

	_, err = r.collection.InsertOne(ctx, document)
	if driverMongo.IsDuplicateKeyError(err) {
		return fmt.Errorf("create settlement: %w", domain.ErrSettlementApplied)
	}

	return err
}

func (r *LedgerRepository) FindBySettlementID(ctx context.Context, id string) (*domain.Settlement, error) {
	var document ledgerDocument
	err := r.collection.FindOne(ctx, bson.M{"settlement_id": id}).Decode(&document)
	if errors.Is(err, driverMongo.ErrNoDocuments) {
		return nil, domain.ErrSettlementNotFound
	}

	if err != nil {
		return nil, err
	}

	settlement := settlementDomainFromDocument(document)
	return &settlement, nil
}

var _ repository.LedgerRepository = (*LedgerRepository)(nil)
