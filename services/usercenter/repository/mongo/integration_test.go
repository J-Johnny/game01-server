package mongo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	commonmongo "server/common/mongodb"
	"server/services/usercenter/domain"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	driverMongo "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func TestMongoReplicaSetPersistenceAndRefreshRotation(t *testing.T) {
	uri := os.Getenv("GAME_MONGO_REPLICA_URI")
	if uri == "" {
		t.Skip("GAME_MONGO_REPLICA_URI is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := driverMongo.Connect(ctx, options.Client().ApplyURI(uri).SetServerSelectionTimeout(10*time.Second))
	if err != nil {
		t.Fatalf("connect replica set: %v", err)
	}
	defer client.Disconnect(context.Background())
	if err := client.Ping(ctx, nil); err != nil {
		t.Fatalf("ping replica set: %v", err)
	}

	var hello bson.M
	if err := client.Database("admin").RunCommand(ctx, bson.D{{Key: "hello", Value: 1}}).Decode(&hello); err != nil {
		t.Fatalf("hello replica set: %v", err)
	}
	if hello["setName"] == nil {
		t.Fatal("MongoDB integration URI is not connected to a replica set")
	}

	databaseName := fmt.Sprintf("game01_usercenter_test_%s", uuid.NewString())
	database := client.Database(databaseName)
	defer database.Drop(context.Background())

	unitOfWork := commonmongo.NewMongoUnitOfWork(client)
	accounts := NewAccountRepository(database.Collection("accounts"))
	identities := NewIdentityRepository(database.Collection("account_identities"))
	tokens := NewRefreshTokenRepository(database.Collection("refresh_tokens"), unitOfWork)
	idempotency := NewIdempotencyRepository(database.Collection("idempotency_records"))
	for name, repository := range map[string]interface {
		EnsureIndexes(context.Context) error
	}{
		"accounts":    accounts,
		"identities":  identities,
		"tokens":      tokens,
		"idempotency": idempotency,
	} {
		if err := repository.EnsureIndexes(ctx); err != nil {
			t.Fatalf("ensure %s indexes: %v", name, err)
		}
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	account := &domain.Account{ID: uuid.NewString(), CreatedAt: now, UpdatedAt: now}
	if err := accounts.Create(ctx, account); err != nil {
		t.Fatalf("create account: %v", err)
	}
	identity := &domain.Identity{ID: uuid.NewString(), AccountID: account.ID, Provider: domain.AuthProviderPassword, Subject: "replica-user", PasswordHash: "bcrypt-hash", LinkedAt: now}
	if err := identities.Create(ctx, identity); err != nil {
		t.Fatalf("create identity: %v", err)
	}
	if err := accounts.LinkPlayer(ctx, account.ID, uuid.NewString(), now); err != nil {
		t.Fatalf("link player: %v", err)
	}
	loadedAccount, err := accounts.FindByID(ctx, account.ID)
	if err != nil || loadedAccount.ID != account.ID || len(loadedAccount.PlayerIDs) != 1 {
		t.Fatalf("load account = %+v, err = %v", loadedAccount, err)
	}
	loadedIdentity, err := identities.Find(ctx, domain.AuthProviderPassword, "replica-user")
	if err != nil || loadedIdentity.AccountID != account.ID {
		t.Fatalf("load identity = %+v, err = %v", loadedIdentity, err)
	}

	oldToken := &domain.RefreshToken{ID: uuid.NewString(), AccountID: account.ID, TokenHash: uuid.NewString(), InstallID: "replica-install", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := tokens.Create(ctx, oldToken); err != nil {
		t.Fatalf("create refresh token: %v", err)
	}
	replacement := &domain.RefreshToken{ID: uuid.NewString(), AccountID: account.ID, TokenHash: uuid.NewString(), InstallID: oldToken.InstallID, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := tokens.Rotate(ctx, oldToken.ID, now.Add(time.Second), replacement); err != nil {
		t.Fatalf("rotate refresh token: %v", err)
	}
	if _, err := tokens.FindValid(ctx, oldToken.TokenHash, now.Add(2*time.Second)); !errors.Is(err, domain.ErrInvalidToken) {
		t.Fatalf("old token error = %v, want invalid token", err)
	}
	if loaded, err := tokens.FindValid(ctx, replacement.TokenHash, now.Add(2*time.Second)); err != nil || loaded.ID != replacement.ID {
		t.Fatalf("replacement token = %+v, err = %v", loaded, err)
	}

	pending := &domain.IdempotencyRecord{Key: uuid.NewString(), Operation: "integration", RequestHash: uuid.NewString(), State: domain.IdempotencyStatePending, ReservationID: uuid.NewString(), LeaseUntil: now.Add(time.Second), CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := idempotency.Create(ctx, pending); err != nil {
		t.Fatalf("create pending idempotency record: %v", err)
	}
	if err := idempotency.Renew(ctx, pending.Key, pending.ReservationID, now.Add(2*time.Minute), now); err != nil {
		t.Fatalf("renew idempotency lease: %v", err)
	}
	recovered, err := idempotency.RecoverExpired(ctx, now.Add(3*time.Minute))
	if err != nil || recovered != 1 {
		t.Fatalf("recover pending idempotency record: deleted=%d err=%v", recovered, err)
	}
}

func TestMongoReplicaSetRotationRollsBackOnInsertFailure(t *testing.T) {
	uri := os.Getenv("GAME_MONGO_REPLICA_URI")
	if uri == "" {
		t.Skip("GAME_MONGO_REPLICA_URI is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := driverMongo.Connect(ctx, options.Client().ApplyURI(uri).SetServerSelectionTimeout(10*time.Second))
	if err != nil {
		t.Fatalf("connect replica set: %v", err)
	}
	defer client.Disconnect(context.Background())
	database := client.Database(fmt.Sprintf("game01_usercenter_rollback_%s", uuid.NewString()))
	defer database.Drop(context.Background())

	tokens := NewRefreshTokenRepository(database.Collection("refresh_tokens"), commonmongo.NewMongoUnitOfWork(client))
	if err := tokens.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensure token indexes: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	oldToken := &domain.RefreshToken{ID: uuid.NewString(), AccountID: uuid.NewString(), TokenHash: uuid.NewString(), InstallID: "rollback-install", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := tokens.Create(ctx, oldToken); err != nil {
		t.Fatalf("create old token: %v", err)
	}
	duplicateID := &domain.RefreshToken{ID: oldToken.ID, AccountID: oldToken.AccountID, TokenHash: uuid.NewString(), InstallID: oldToken.InstallID, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := tokens.Rotate(ctx, oldToken.ID, now.Add(time.Second), duplicateID); err == nil {
		t.Fatal("rotation unexpectedly succeeded with duplicate token id")
	}
	if loaded, err := tokens.FindValid(ctx, oldToken.TokenHash, now.Add(2*time.Second)); err != nil || loaded.ID != oldToken.ID {
		t.Fatalf("rollback did not preserve old token: %+v, err = %v", loaded, err)
	}
}
