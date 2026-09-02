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

func roomSnapshotIndexes() []driverMongo.IndexModel {
	return []driverMongo.IndexModel{
		{Keys: bson.D{{Key: "room_id", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "players.player_id", Value: 1}}},
		{Keys: bson.D{{Key: "updated_at", Value: -1}}},
	}
}
