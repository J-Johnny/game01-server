package mongo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"server/services/usercenter/domain"
	"server/services/usercenter/repository"

	"go.mongodb.org/mongo-driver/bson"
	driverMongo "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type IdempotencyRepository struct {
	collection *driverMongo.Collection
}

func NewIdempotencyRepository(collection *driverMongo.Collection) *IdempotencyRepository {
	return &IdempotencyRepository{
		collection: collection,
	}
}

func (r *IdempotencyRepository) EnsureIndexes(ctx context.Context) error {
	if r == nil {
		return ErrCollectionRequired
	}
	return ensureIndexes(ctx, r.collection, idempotencyIndexes())
}

func (r *IdempotencyRepository) Find(ctx context.Context, key string, now time.Time) (*domain.IdempotencyRecord, error) {
	if r == nil || r.collection == nil {
		return nil, ErrCollectionRequired
	}
	if key == "" {
		return nil, domain.ErrInvalidIdempotency
	}
	var document idempotencyDocument
	err := r.collection.FindOne(ctx, bson.M{"key": key, "expires_at": bson.M{"$gt": now}}).Decode(&document)
	if errors.Is(err, driverMongo.ErrNoDocuments) {
		return nil, ErrIdempotencyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find idempotency record: %w", err)
	}
	record := idempotencyDomainFromDocument(document)
	return &record, nil
}

func (r *IdempotencyRepository) Create(ctx context.Context, record *domain.IdempotencyRecord) error {
	if r == nil || r.collection == nil {
		return ErrCollectionRequired
	}
	document, err := idempotencyDocumentFromDomain(record)
	if err != nil {
		return err
	}
	if _, err := r.collection.InsertOne(ctx, document); err != nil {
		if driverMongo.IsDuplicateKeyError(err) {
			return domain.ErrIdempotencyConflict
		}
		return fmt.Errorf("create idempotency record: %w", err)
	}
	return nil
}

func (r *IdempotencyRepository) Reserve(ctx context.Context, record *domain.IdempotencyRecord, now time.Time) (*domain.IdempotencyRecord, bool, error) {
	if r == nil || r.collection == nil {
		return nil, false, ErrCollectionRequired
	}
	if record == nil {
		return nil, false, domain.ErrInvalidIdempotency
	}
	if record.State == "" {
		record.State = domain.IdempotencyStatePending
	}
	if record.ReservationID == "" || record.LeaseUntil.IsZero() {
		return nil, false, domain.ErrInvalidIdempotency
	}
	if err := record.Validate(now); err != nil {
		return nil, false, err
	}
	document, err := idempotencyDocumentFromDomain(record)
	if err != nil {
		return nil, false, err
	}
	if _, err := r.collection.InsertOne(ctx, document); err == nil {
		return record, true, nil
	} else if !driverMongo.IsDuplicateKeyError(err) {
		return nil, false, fmt.Errorf("reserve idempotency record: %w", err)
	}
	current, err := r.Find(ctx, record.Key, now)
	if errors.Is(err, ErrIdempotencyNotFound) {
		staleResult, deleteErr := r.collection.DeleteOne(ctx, bson.M{
			"key": record.Key,
			"$or": []bson.M{
				{"expires_at": bson.M{"$lte": now}},
				{"state": string(domain.IdempotencyStatePending), "lease_until": bson.M{"$lte": now}},
			},
		})
		if deleteErr != nil {
			return nil, false, fmt.Errorf("remove stale idempotency record: %w", deleteErr)
		}
		if staleResult.DeletedCount == 1 {
			if _, insertErr := r.collection.InsertOne(ctx, document); insertErr == nil {
				return record, true, nil
			} else if !driverMongo.IsDuplicateKeyError(insertErr) {
				return nil, false, fmt.Errorf("reserve idempotency record after stale cleanup: %w", insertErr)
			}
			current, err = r.Find(ctx, record.Key, now)
		}
	}
	if err != nil {
		return nil, false, err
	}
	if current.Operation != record.Operation || current.RequestHash != record.RequestHash {
		return current, false, domain.ErrIdempotencyConflict
	}
	if current.IsCompleted() {
		return current, false, nil
	}
	if current.LeaseUntil.After(now) {
		return current, false, nil
	}
	filter := bson.M{"key": record.Key, "state": string(domain.IdempotencyStatePending), "reservation_id": current.ReservationID, "lease_until": bson.M{"$lte": now}}
	update := bson.M{"$set": bson.M{"reservation_id": record.ReservationID, "lease_until": record.LeaseUntil}}
	result := r.collection.FindOneAndUpdate(ctx, filter, update, options.FindOneAndUpdate().SetReturnDocument(options.After))
	var claimed idempotencyDocument
	if err := result.Decode(&claimed); err != nil {
		if errors.Is(err, driverMongo.ErrNoDocuments) {
			latest, findErr := r.Find(ctx, record.Key, now)
			if findErr != nil {
				return nil, false, findErr
			}
			return latest, false, nil
		}
		return nil, false, fmt.Errorf("claim idempotency record: %w", err)
	}
	claimedDomain := idempotencyDomainFromDocument(claimed)
	return &claimedDomain, true, nil
}

func (r *IdempotencyRepository) Complete(ctx context.Context, key, reservationID string, response []byte, now time.Time) error {
	if r == nil || r.collection == nil {
		return ErrCollectionRequired
	}
	result, err := r.collection.UpdateOne(ctx, bson.M{"key": key, "state": string(domain.IdempotencyStatePending), "reservation_id": reservationID, "expires_at": bson.M{"$gt": now}}, bson.M{"$set": bson.M{"state": string(domain.IdempotencyStateCompleted), "response": append([]byte(nil), response...)}, "$unset": bson.M{"lease_until": ""}})
	if err != nil {
		return fmt.Errorf("complete idempotency record: %w", err)
	}
	if result.MatchedCount == 0 {
		return domain.ErrIdempotencyConflict
	}
	return nil
}

func (r *IdempotencyRepository) Release(ctx context.Context, key, reservationID string) error {
	if r == nil || r.collection == nil {
		return ErrCollectionRequired
	}
	if _, err := r.collection.DeleteOne(ctx, bson.M{"key": key, "state": string(domain.IdempotencyStatePending), "reservation_id": reservationID}); err != nil {
		return fmt.Errorf("release idempotency record: %w", err)
	}
	return nil
}

func (r *IdempotencyRepository) Renew(ctx context.Context, key, reservationID string, leaseUntil, now time.Time) error {
	if r == nil || r.collection == nil {
		return ErrCollectionRequired
	}
	if key == "" || reservationID == "" || !leaseUntil.After(now) {
		return domain.ErrInvalidIdempotency
	}
	result, err := r.collection.UpdateOne(ctx, bson.M{
		"key":            key,
		"state":          string(domain.IdempotencyStatePending),
		"reservation_id": reservationID,
		"expires_at":     bson.M{"$gt": now},
	}, bson.M{"$set": bson.M{"lease_until": leaseUntil}})
	if err != nil {
		return fmt.Errorf("renew idempotency lease: %w", err)
	}
	if result.MatchedCount == 0 {
		return domain.ErrIdempotencyLeaseLost
	}
	return nil
}

func (r *IdempotencyRepository) RecoverExpired(ctx context.Context, now time.Time) (int64, error) {
	if r == nil || r.collection == nil {
		return 0, ErrCollectionRequired
	}
	result, err := r.collection.DeleteMany(ctx, bson.M{
		"state": string(domain.IdempotencyStatePending),
		"$or": []bson.M{
			{"lease_until": bson.M{"$lte": now}},
			{"expires_at": bson.M{"$lte": now}},
		},
	})
	if err != nil {
		return 0, fmt.Errorf("recover expired idempotency records: %w", err)
	}
	return result.DeletedCount, nil
}

var _ repository.IdempotencyRepository = (*IdempotencyRepository)(nil)
var _ repository.IdempotencyCoordinator = (*IdempotencyRepository)(nil)
