package gateway

import (
	"testing"

	"google.golang.org/protobuf/proto"
	statepb "server/proto/gen/client/state"
	internalpb "server/proto/gen/internalpb"
)

func TestToClientStateConvertsBattleSnapshot(t *testing.T) {
	payload, err := proto.Marshal(&internalpb.BattleRoomSnapshot{
		RoomId:  7,
		Tick:    3,
		Status:  "running",
		Players: []*internalpb.BattlePlayerState{{PlayerId: "player-1", Hp: 80}},
	})
	if err != nil {
		t.Fatal(err)
	}
	converted, err := (&Module{}).toClientState("battle", internalpb.RestoreMode_RESTORE_MODE_FULL, payload)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := &statepb.BattleRoomSnapshot{}
	if err := proto.Unmarshal(converted, snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.RoomId != 7 || len(snapshot.Players) != 1 || snapshot.Players[0].PlayerId != "player-1" {
		t.Fatalf("unexpected client snapshot: %s", snapshot)
	}
}
