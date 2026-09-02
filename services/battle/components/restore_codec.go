package components

import (
	"sort"

	"google.golang.org/protobuf/proto"
	statepb "server/proto/gen/client/state"
	"server/services/battle/domain"
)

const StateSchemaVersion uint32 = 1

func MarshalRoomSnapshot(room *domain.Room) ([]byte, error) {
	snapshot := &statepb.BattleRoomSnapshot{
		RoomId:             room.ID,
		Tick:               room.Tick,
		StateVersion:       room.StateVersion,
		Status:             string(room.Status),
		UpdatedAtUnixMilli: room.UpdatedAt.UnixMilli(),
		Players:            make([]*statepb.BattlePlayerState, 0, len(room.Players)),
	}
	playerIDs := make([]string, 0, len(room.Players))
	for playerID := range room.Players {
		playerIDs = append(playerIDs, playerID)
	}
	sort.Strings(playerIDs)
	for _, playerID := range playerIDs {
		snapshot.Players = append(snapshot.Players, battlePlayerStateProto(room.Players[playerID]))
	}
	return proto.Marshal(snapshot)
}

func MarshalRoomDelta(delta *domain.RoomDelta) ([]byte, error) {
	if delta == nil {
		return nil, domain.ErrInvalidRoom
	}
	result := &statepb.BattleRoomDelta{
		RoomId:           delta.RoomID,
		FromStateVersion: delta.FromStateVersion,
		ToStateVersion:   delta.ToStateVersion,
		Tick:             delta.Tick,
		Status:           string(delta.Status),
		RemovedPlayerIds: append([]string(nil), delta.RemovedPlayerIDs...),
	}
	for _, player := range delta.UpsertPlayers {
		result.UpsertPlayers = append(result.UpsertPlayers, battlePlayerStateProto(player))
	}
	return proto.Marshal(result)
}

func battlePlayerStateProto(player domain.PlayerState) *statepb.BattlePlayerState {
	return &statepb.BattlePlayerState{
		PlayerId:  player.PlayerID,
		Hp:        player.HP,
		PositionX: player.PositionX,
		PositionY: player.PositionY,
	}
}
