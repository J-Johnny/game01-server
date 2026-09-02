package mongo

import (
	"context"
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/bson"
	driverMongo "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"server/services/battle/domain"
	"server/services/battle/repository"
)

type RoomSnapshotRepository struct {
	collection *driverMongo.Collection
}

func NewRoomSnapshotRepository(collection *driverMongo.Collection) *RoomSnapshotRepository {
	return &RoomSnapshotRepository{collection: collection}
}

func (r *RoomSnapshotRepository) EnsureIndexes(ctx context.Context) error {
	return ensureIndexes(ctx, r.collection, roomSnapshotIndexes())
}

func (r *RoomSnapshotRepository) Save(ctx context.Context, room *domain.Room) error {
	if r == nil || r.collection == nil {
		return ErrCollectionRequired
	}
	document, err := roomSnapshotDocumentFromDomain(room)
	if err != nil {
		return err
	}
	_, err = r.collection.ReplaceOne(ctx, bson.M{"room_id": room.ID}, document, options.Replace().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("save battle room snapshot: %w", err)
	}
	return nil
}

func (r *RoomSnapshotRepository) FindByPlayerID(ctx context.Context, playerID string) (*domain.Room, error) {
	if r == nil || r.collection == nil {
		return nil, ErrCollectionRequired
	}
	if playerID == "" {
		return nil, domain.ErrPlayerNotInRoom
	}
	var document roomSnapshotDocument
	err := r.collection.FindOne(ctx, bson.M{"players.player_id": playerID}, options.FindOne().SetSort(bson.D{{Key: "updated_at", Value: -1}})).Decode(&document)
	if errors.Is(err, driverMongo.ErrNoDocuments) {
		return nil, domain.ErrRoomNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find battle room snapshot: %w", err)
	}
	return roomSnapshotDomainFromDocument(document)
}

var _ repository.RoomSnapshotRepository = (*RoomSnapshotRepository)(nil)
