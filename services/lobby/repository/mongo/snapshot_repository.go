package mongo

import (
	"context"
	"errors"
	"server/services/lobby/domain"
	"server/services/lobby/repository"

	"go.mongodb.org/mongo-driver/bson"
	driverMongo "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type SnapshotRepository struct{ collection *driverMongo.Collection }

func NewSnapshotRepository(collection *driverMongo.Collection) *SnapshotRepository {
	return &SnapshotRepository{collection: collection}
}

func (r *SnapshotRepository) EnsureIndexes(ctx context.Context) error {
	return ensureIndexes(ctx, r.collection, snapshotIndexes())
}

func (r *SnapshotRepository) FindByPlayerID(ctx context.Context, playerID string) (*domain.Snapshot, error) {
	var document snapshotDocument
	err := r.collection.FindOne(ctx, bson.M{"player_id": playerID}).Decode(&document)
	if errors.Is(err, driverMongo.ErrNoDocuments) {
		return nil, domain.ErrPlayerNotFound
	}

	if err != nil {
		return nil, err
	}

	snapshot := snapshotDomainFromDocument(document)
	return &snapshot, nil
}

func (r *SnapshotRepository) Save(ctx context.Context, snapshot domain.Snapshot) error {
	_, err := r.collection.ReplaceOne(ctx, bson.M{"player_id": snapshot.Player.ID},
		snapshotDocumentFromDomain(snapshot), options.Replace().SetUpsert(true))

	return err
}

var _ repository.SnapshotRepository = (*SnapshotRepository)(nil)
