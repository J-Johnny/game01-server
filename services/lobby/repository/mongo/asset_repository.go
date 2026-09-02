package mongo

import (
	"context"
	"errors"
	"fmt"
	"go.mongodb.org/mongo-driver/bson"
	driverMongo "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"server/services/lobby/domain"
	"server/services/lobby/repository"
	"time"
)

type AssetRepository struct{ collection *driverMongo.Collection }

func NewAssetRepository(collection *driverMongo.Collection) *AssetRepository {
	return &AssetRepository{collection: collection}
}
func (r *AssetRepository) EnsureIndexes(ctx context.Context) error {
	return ensureIndexes(ctx, r.collection, assetIndexes())
}
func (r *AssetRepository) Create(ctx context.Context, assets domain.Assets) error {
	_, err := r.collection.InsertOne(ctx, assetsDocumentFromDomain(assets))
	return err
}
func (r *AssetRepository) FindByPlayerID(ctx context.Context, playerID string) (*domain.Assets, error) {
	var document assetsDocument
	if err := r.collection.FindOne(ctx, bson.M{"player_id": playerID}).Decode(&document); err != nil {
		if errors.Is(err, driverMongo.ErrNoDocuments) {
			return nil, domain.ErrAssetNotFound
		}
		return nil, err
	}
	assets := assetsDomainFromDocument(document)
	return &assets, nil
}
func (r *AssetRepository) ApplyDelta(ctx context.Context, playerID, assetType string, delta int64) (*domain.Assets, error) {
	if delta == 0 {
		return r.FindByPlayerID(ctx, playerID)
	}
	filter := bson.M{"player_id": playerID}
	if delta < 0 {
		filter["currency."+assetType] = bson.M{"$gte": -delta}
	}
	result := r.collection.FindOneAndUpdate(ctx, filter, bson.M{"$inc": bson.M{"currency." + assetType: delta, "asset_version": 1}, "$set": bson.M{"updated_at": time.Now().UTC()}}, options.FindOneAndUpdate().SetReturnDocument(options.After))
	var document assetsDocument
	if err := result.Decode(&document); err != nil {
		if errors.Is(err, driverMongo.ErrNoDocuments) {
			return nil, domain.ErrInsufficientCurrency
		}
		return nil, fmt.Errorf("apply asset delta: %w", err)
	}
	assets := assetsDomainFromDocument(document)
	return &assets, nil
}

var _ repository.AssetRepository = (*AssetRepository)(nil)
