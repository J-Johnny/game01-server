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
)

type PlayerRepository struct{ collection *driverMongo.Collection }

func NewPlayerRepository(collection *driverMongo.Collection) *PlayerRepository {
	return &PlayerRepository{collection: collection}
}
func (r *PlayerRepository) EnsureIndexes(ctx context.Context) error {
	return ensureIndexes(ctx, r.collection, playerIndexes())
}
func (r *PlayerRepository) Create(ctx context.Context, player domain.Player) error {
	document, err := playerDocumentFromDomain(player)
	if err != nil {
		return err
	}
	_, err = r.collection.InsertOne(ctx, document)
	if driverMongo.IsDuplicateKeyError(err) {
		return fmt.Errorf("create player: duplicate key: %w", err)
	}
	return err
}
func (r *PlayerRepository) FindDefaultByAccountID(ctx context.Context, accountID string) (*domain.Player, error) {
	return r.find(ctx, bson.M{"account_id": accountID, "is_default": true})
}
func (r *PlayerRepository) FindByID(ctx context.Context, playerID string) (*domain.Player, error) {
	return r.find(ctx, bson.M{"player_id": playerID})
}
func (r *PlayerRepository) find(ctx context.Context, filter interface{}, findOptions ...*options.FindOneOptions) (*domain.Player, error) {
	var document playerDocument
	if err := r.collection.FindOne(ctx, filter, findOptions...).Decode(&document); err != nil {
		if errors.Is(err, driverMongo.ErrNoDocuments) {
			return nil, domain.ErrPlayerNotFound
		}
		return nil, err
	}
	player := playerDomainFromDocument(document)
	return &player, nil
}

var _ repository.PlayerRepository = (*PlayerRepository)(nil)
