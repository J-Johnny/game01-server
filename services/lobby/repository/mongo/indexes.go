package mongo

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	driverMongo "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func ensureIndexes(ctx context.Context, collection *driverMongo.Collection, indexes []driverMongo.IndexModel) error {
	if collection == nil {
		return ErrCollectionRequired
	}
	_, err := collection.Indexes().CreateMany(ctx, indexes)
	return err
}

func playerIndexes() []driverMongo.IndexModel {
	return []driverMongo.IndexModel{{Keys: bson.D{{Key: "player_id", Value: 1}}, Options: options.Index().SetUnique(true)}, {Keys: bson.D{{Key: "account_id", Value: 1}, {Key: "updated_at", Value: -1}}}, {Keys: bson.D{{Key: "account_id", Value: 1}, {Key: "is_default", Value: 1}}, Options: options.Index().SetUnique(true).SetPartialFilterExpression(bson.M{"is_default": true})}}
}

func assetIndexes() []driverMongo.IndexModel {
	return []driverMongo.IndexModel{{Keys: bson.D{{Key: "player_id", Value: 1}}, Options: options.Index().SetUnique(true)}}
}

func ledgerIndexes() []driverMongo.IndexModel {
	return []driverMongo.IndexModel{{Keys: bson.D{{Key: "settlement_id", Value: 1}}, Options: options.Index().SetUnique(true)}, {Keys: bson.D{{Key: "player_id", Value: 1}, {Key: "created_at", Value: -1}}}}
}

func snapshotIndexes() []driverMongo.IndexModel {
	return []driverMongo.IndexModel{{Keys: bson.D{{Key: "player_id", Value: 1}}, Options: options.Index().SetUnique(true)}}
}
