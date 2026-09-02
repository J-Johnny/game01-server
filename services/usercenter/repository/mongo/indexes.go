package mongo

import (
	"context"
	driverMongo "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func accountIndexes() []driverMongo.IndexModel {
	return []driverMongo.IndexModel{
		{Keys: map[string]int{"account_id": 1}, Options: options.Index().SetUnique(true)},
	}
}

func identityIndexes() []driverMongo.IndexModel {
	return []driverMongo.IndexModel{
		{Keys: map[string]int{"identity_id": 1}, Options: options.Index().SetUnique(true)},
		{Keys: map[string]int{"provider": 1, "subject": 1}, Options: options.Index().SetUnique(true)},
		{Keys: map[string]int{"account_id": 1}},
	}
}

func refreshTokenIndexes() []driverMongo.IndexModel {
	return []driverMongo.IndexModel{
		{Keys: map[string]int{"token_id": 1}, Options: options.Index().SetUnique(true)},
		{Keys: map[string]int{"token_hash": 1}, Options: options.Index().SetUnique(true)},
		{Keys: map[string]int{"account_id": 1, "install_id": 1}},
		{Keys: map[string]int{"expires_at": 1}, Options: options.Index().SetExpireAfterSeconds(0)},
	}
}

func idempotencyIndexes() []driverMongo.IndexModel {
	return []driverMongo.IndexModel{
		{Keys: map[string]int{"key": 1}, Options: options.Index().SetUnique(true)},
		{Keys: map[string]int{"expires_at": 1}, Options: options.Index().SetExpireAfterSeconds(0)},
		{Keys: map[string]int{"operation": 1, "created_at": -1}},
	}
}

func ensureIndexes(ctx context.Context, collection *driverMongo.Collection, indexes []driverMongo.IndexModel) error {
	if collection == nil {
		return ErrCollectionRequired
	}
	_, err := collection.Indexes().CreateMany(ctx, indexes)
	return err
}
