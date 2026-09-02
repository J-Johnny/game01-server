package battle

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"server/common/streaming"
	statepb "server/proto/gen/client/state"
	internalpb "server/proto/gen/internalpb"
	"server/services/battle/components"
	"server/services/battle/domain"
)

func TestRestorePlayerStateReturnsBattleRoomSnapshot(t *testing.T) {
	repository := &moduleRoomSnapshotRepository{rooms: make(map[uint64]*domain.Room)}
	manager := components.NewRoomManager(repository)
	now := time.Now().UTC()
	room, err := domain.NewRoom(123, []domain.PlayerState{{PlayerID: "player-1", HP: 90}}, now)
	if err != nil {
		t.Fatalf("new room: %v", err)
	}
	if err := manager.Create(context.Background(), room); err != nil {
		t.Fatalf("create room: %v", err)
	}
	module := &Module{rooms: manager}
	payload, err := proto.Marshal(&internalpb.RestorePlayerStateRequest{PlayerId: "player-1", SessionId: "session-1"})
	if err != nil {
		t.Fatalf("marshal restore request: %v", err)
	}
	result, err := module.restorePlayerState(context.Background(), streaming.Peer{}, &internalpb.InternalEnvelope{Payload: payload})
	if err != nil {
		t.Fatalf("restore player state: %v", err)
	}
	response := result.Message.(*internalpb.RestorePlayerStateResponse)
	if !response.Available || response.StateVersion != 1 || response.PlayerId != "player-1" {
		t.Fatalf("unexpected restore response: %s", response)
	}
	if response.PayloadType != internalpb.StatePayloadType_STATE_PAYLOAD_TYPE_BATTLE_SNAPSHOT || response.SchemaVersion != components.StateSchemaVersion {
		t.Fatalf("unexpected state metadata: %s", response)
	}
	snapshot := &statepb.BattleRoomSnapshot{}
	if err := proto.Unmarshal(response.Snapshot, snapshot); err != nil {
		t.Fatalf("unmarshal battle snapshot: %v", err)
	}
	if snapshot.RoomId != 123 || len(snapshot.Players) != 1 || snapshot.Players[0].Hp != 90 {
		t.Fatalf("unexpected battle snapshot: %s", snapshot)
	}
}

func TestRestorePlayerStateReturnsClientBattleDelta(t *testing.T) {
	repository := &moduleRoomSnapshotRepository{rooms: make(map[uint64]*domain.Room)}
	manager := components.NewRoomManager(repository)
	now := time.Now().UTC()
	room, err := domain.NewRoom(124, []domain.PlayerState{{PlayerID: "player-1", HP: 90}}, now)
	if err != nil {
		t.Fatalf("new room: %v", err)
	}
	if err := manager.Create(context.Background(), room); err != nil {
		t.Fatalf("create room: %v", err)
	}
	if err := manager.Mutate(context.Background(), room.ID, func(room *domain.Room) error {
		return room.UpdatePlayer(domain.PlayerState{PlayerID: "player-1", HP: 70})
	}); err != nil {
		t.Fatalf("mutate room: %v", err)
	}
	module := &Module{rooms: manager}
	payload, err := proto.Marshal(&internalpb.RestorePlayerStateRequest{
		PlayerId:         "player-1",
		LastStateVersion: 1,
		AllowIncremental: true,
	})
	if err != nil {
		t.Fatalf("marshal restore request: %v", err)
	}
	result, err := module.restorePlayerState(context.Background(), streaming.Peer{}, &internalpb.InternalEnvelope{Payload: payload})
	if err != nil {
		t.Fatalf("restore player state: %v", err)
	}
	response := result.Message.(*internalpb.RestorePlayerStateResponse)
	if response.Mode != internalpb.RestoreMode_RESTORE_MODE_DELTA || response.PayloadType != internalpb.StatePayloadType_STATE_PAYLOAD_TYPE_BATTLE_DELTA {
		t.Fatalf("unexpected restore response: %s", response)
	}
	delta := &statepb.BattleRoomDelta{}
	if err := proto.Unmarshal(response.Snapshot, delta); err != nil {
		t.Fatalf("unmarshal battle delta: %v", err)
	}
	if delta.FromStateVersion != 1 || delta.ToStateVersion != 2 || len(delta.UpsertPlayers) != 1 || delta.UpsertPlayers[0].Hp != 70 {
		t.Fatalf("unexpected battle delta: %s", delta)
	}
}

type moduleRoomSnapshotRepository struct {
	rooms map[uint64]*domain.Room
}

func (r *moduleRoomSnapshotRepository) EnsureIndexes(context.Context) error { return nil }

func (r *moduleRoomSnapshotRepository) Save(_ context.Context, room *domain.Room) error {
	r.rooms[room.ID] = room.Clone()
	return nil
}

func (r *moduleRoomSnapshotRepository) FindByPlayerID(_ context.Context, playerID string) (*domain.Room, error) {
	for _, room := range r.rooms {
		if _, exists := room.Players[playerID]; exists {
			return room.Clone(), nil
		}
	}
	return nil, domain.ErrRoomNotFound
}
