package mongo

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	driverMongo "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"server/services/battle/domain"
)

func TestMongoReplicaSetRoomSnapshotPersistence(t *testing.T) {
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
	database := client.Database(fmt.Sprintf("game01_battle_test_%s", uuid.NewString()))
	defer database.Drop(context.Background())
	repository := NewRoomSnapshotRepository(database.Collection("battle_room_snapshots"))
	if err := repository.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensure indexes: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	room, err := domain.NewRoom(9001, []domain.PlayerState{{PlayerID: "player-1", HP: 100}, {PlayerID: "player-2", HP: 90}}, now)
	if err != nil {
		t.Fatalf("new room: %v", err)
	}
	if err := repository.Save(ctx, room); err != nil {
		t.Fatalf("save initial snapshot: %v", err)
	}
	if err := room.UpdatePlayer(domain.PlayerState{PlayerID: "player-1", HP: 75, PositionX: 12.5}); err != nil {
		t.Fatalf("update player: %v", err)
	}
	if err := room.AdvanceTick(now.Add(time.Second)); err != nil {
		t.Fatalf("advance tick: %v", err)
	}
	if err := repository.Save(ctx, room); err != nil {
		t.Fatalf("save updated snapshot: %v", err)
	}

	restored, err := repository.FindByPlayerID(ctx, "player-1")
	if err != nil {
		t.Fatalf("restore snapshot: %v", err)
	}
	if restored.ID != room.ID || restored.Tick != 1 || restored.StateVersion != 2 || restored.Players["player-1"].HP != 75 || restored.Players["player-1"].PositionX != 12.5 {
		t.Fatalf("unexpected restored room: %+v", restored)
	}
	if _, err := repository.FindByPlayerID(ctx, "missing-player"); err != domain.ErrRoomNotFound {
		t.Fatalf("missing player error = %v, want %v", err, domain.ErrRoomNotFound)
	}
}
