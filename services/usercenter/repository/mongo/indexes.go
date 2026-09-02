package mongo

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	driverMongo "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func accountIndexes() []driverMongo.IndexModel {
	return []driverMongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "account_id", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
	}
}

func identityIndexes() []driverMongo.IndexModel {
	return []driverMongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "identity_id", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "provider", Value: 1},
				{Key: "subject", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "account_id", Value: 1},
			},
		},
	}
}

func refreshTokenIndexes() []driverMongo.IndexModel {
	return []driverMongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "token_id", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "token_hash", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "account_id", Value: 1},
				{Key: "install_id", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "expires_at", Value: 1},
			},
			Options: options.Index().SetExpireAfterSeconds(0),
		},
	}
}

func idempotencyIndexes() []driverMongo.IndexModel {
	return []driverMongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "key", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "expires_at", Value: 1},
			},
			Options: options.Index().SetExpireAfterSeconds(0),
		},
		{
			Keys: bson.D{
				{Key: "operation", Value: 1},
				{Key: "created_at", Value: -1},
			},
		},
	}
}

func ensureIndexes(ctx context.Context, collection *driverMongo.Collection, indexes []driverMongo.IndexModel) error {
	if collection == nil {
		return ErrCollectionRequired
	}
	_, err := collection.Indexes().CreateMany(ctx, indexes)
	return err
}
