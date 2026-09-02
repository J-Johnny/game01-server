package components

import (
	"google.golang.org/protobuf/proto"
	statepb "server/proto/gen/client/state"
	"server/services/lobby/domain"
)

const StateSchemaVersion uint32 = 1

func MarshalStateSnapshot(snapshot domain.Snapshot) ([]byte, error) {
	return proto.Marshal(&statepb.LobbyStateSnapshot{
		PlayerId:       snapshot.Player.ID,
		AccountId:      snapshot.Player.AccountID,
		Nickname:       snapshot.Player.Nickname,
		Region:         snapshot.Player.Region,
		ProfileVersion: snapshot.Player.ProfileVersion,
		AssetVersion:   snapshot.Assets.AssetVersion,
		Currency:       snapshot.Assets.Currency,
	})
}
