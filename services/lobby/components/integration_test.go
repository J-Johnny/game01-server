package components

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	commonmongo "server/common/mongodb"
	"server/common/streaming"
	internalpb "server/proto/gen/internalpb"
	"server/services/lobby/domain"
	mongorepository "server/services/lobby/repository/mongo"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	driverMongo "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"google.golang.org/protobuf/proto"
)

func TestMongoReplicaSetPlayerAssetsAndSettlement(t *testing.T) {
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

	database := client.Database(fmt.Sprintf("game01_lobby_test_%s", uuid.NewString()))
	defer database.Drop(context.Background())

	players := mongorepository.NewPlayerRepository(database.Collection("players"))
	assets := mongorepository.NewAssetRepository(database.Collection("player_assets"))
	ledger := mongorepository.NewLedgerRepository(database.Collection("asset_ledger"))
	snapshots := mongorepository.NewSnapshotRepository(database.Collection("player_snapshots"))
	for name, repository := range map[string]interface {
		EnsureIndexes(context.Context) error
	}{
		"players":   players,
		"assets":    assets,
		"ledger":    ledger,
		"snapshots": snapshots,
	} {
		if err := repository.EnsureIndexes(ctx); err != nil {
			t.Fatalf("ensure %s indexes: %v", name, err)
		}
	}

	component := NewPlayerComponent(players, assets, ledger, snapshots, commonmongo.NewMongoUnitOfWork(client), recordingLinker{})
	component.now = func() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }
	accountID := uuid.NewString()
	ensurePayload, err := proto.Marshal(&internalpb.EnsurePlayerRequest{AccountId: accountID})
	if err != nil {
		t.Fatalf("marshal ensure request: %v", err)
	}

	ensure, err := component.ensurePlayer(ctx, streaming.Peer{}, &internalpb.InternalEnvelope{Payload: ensurePayload})
	if err != nil {
		t.Fatalf("ensure player: %v", err)
	}

	playerID := ensure.Message.(*internalpb.EnsurePlayerResponse).PlayerId
	if playerID == "" {
		t.Fatal("ensure player returned an empty player_id")
	}

	for collection, want := range map[string]int64{
		"players":          1,
		"player_assets":    1,
		"player_snapshots": 1,
	} {
		got, countErr := database.Collection(collection).CountDocuments(ctx, bson.M{})
		if countErr != nil || got != want {
			t.Fatalf("%s count = %d, err = %v, want %d", collection, got, countErr, want)
		}
	}

	duplicateDefault := domain.Player{
		ID:             uuid.NewString(),
		AccountID:      accountID,
		Nickname:       "Another Player",
		Region:         "global",
		IsDefault:      true,
		ProfileVersion: 1,
		CreatedAt:      component.now(),
		UpdatedAt:      component.now(),
	}
	if err := players.Create(ctx, duplicateDefault); err == nil {
		t.Fatal("creating a second default player unexpectedly succeeded")
	}

	nonDefault := duplicateDefault
	nonDefault.ID = uuid.NewString()
	nonDefault.IsDefault = false
	if err := players.Create(ctx, nonDefault); err != nil {
		t.Fatalf("create non-default player for same account: %v", err)
	}

	settlementPayload, err := proto.Marshal(&internalpb.SettlementRequest{
		SettlementId: "settlement-1",
		PlayerId:     playerID,
		AssetType:    "gold",
		Delta:        50,
		Reason:       "battle_settlement",
		Source:       "battle",
	})
	if err != nil {
		t.Fatalf("marshal settlement request: %v", err)
	}

	settled, err := component.settlement(ctx, streaming.Peer{}, &internalpb.InternalEnvelope{Payload: settlementPayload})
	if err != nil {
		t.Fatalf("settle player: %v", err)
	}

	settlement := settled.Message.(*internalpb.SettlementResponse)
	if settlement.Balance != 50 || settlement.AssetVersion != 2 || settlement.Replayed {
		t.Fatalf("unexpected settlement response: %s", settlement)
	}

	replayed, err := component.settlement(ctx, streaming.Peer{}, &internalpb.InternalEnvelope{Payload: settlementPayload})
	if err != nil {
		t.Fatalf("replay settlement: %v", err)
	}

	if !replayed.Message.(*internalpb.SettlementResponse).Replayed {
		t.Fatal("duplicate settlement was not replayed")
	}

	insufficientPayload, err := proto.Marshal(&internalpb.SettlementRequest{
		SettlementId: "settlement-insufficient",
		PlayerId:     playerID,
		AssetType:    "gold",
		Delta:        -51,
		Reason:       "battle_settlement",
		Source:       "battle",
	})
	if err != nil {
		t.Fatalf("marshal insufficient settlement request: %v", err)
	}
	_, err = component.settlement(ctx, streaming.Peer{}, &internalpb.InternalEnvelope{Payload: insufficientPayload})
	if !errors.Is(err, domain.ErrInsufficientCurrency) {
		t.Fatalf("insufficient settlement error = %v, want %v", err, domain.ErrInsufficientCurrency)
	}

	if _, err := ledger.FindBySettlementID(ctx, "settlement-insufficient"); !errors.Is(err, domain.ErrSettlementNotFound) {
		t.Fatalf("failed settlement ledger = %v, want not found", err)
	}

	storedAssets, err := assets.FindByPlayerID(ctx, playerID)
	if err != nil || storedAssets.Balance("gold") != 50 || storedAssets.AssetVersion != 2 {
		t.Fatalf("assets after rollback = %+v, err = %v", storedAssets, err)
	}
}
