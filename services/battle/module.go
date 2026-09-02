package battle

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/gin-gonic/gin"
	"google.golang.org/protobuf/proto"
	"server/common/app"
	commonmongo "server/common/mongodb"
	"server/common/streaming"
	internalpb "server/proto/gen/internalpb"
	"server/services/battle/components"
	"server/services/battle/domain"
	mongorepository "server/services/battle/repository/mongo"
	servicecommon "server/services/common"
)

const RoomSnapshotsCollection = "battle_room_snapshots"

type Module struct {
	*servicecommon.Module
	settlements *LobbySettlementClient
	rooms       *components.RoomManager
	initErr     error
}

func NewModule(deps app.Dependencies) *Module {
	module := &Module{Module: servicecommon.NewModule("battle", internalpb.ServiceType_SERVICE_TYPE_BATTLE, deps)}
	module.settlements = NewLobbySettlementClient(func() (*streaming.Client, bool) { return module.Client("lobby") })
	driverResources, ok := deps.Mongo.(commonmongo.DriverResources)
	if !ok || driverResources.DriverDatabase() == nil {
		module.initErr = fmt.Errorf("official MongoDB Driver resources are required")
		return module
	}
	module.rooms = components.NewRoomManager(mongorepository.NewRoomSnapshotRepository(driverResources.DriverCollection(RoomSnapshotsCollection)))
	return module
}

func (m *Module) Settlements() *LobbySettlementClient { return m.settlements }

func (m *Module) Rooms() *components.RoomManager { return m.rooms }

func (m *Module) Start(ctx context.Context) error {
	if m.initErr != nil {
		return m.initErr
	}
	if err := m.rooms.EnsureIndexes(ctx); err != nil {
		return fmt.Errorf("ensure battle room snapshot indexes: %w", err)
	}
	return m.Module.Start(ctx)
}

func (m *Module) RegisterInternal(router *streaming.Router) {
	m.Module.RegisterInternal(router)
	router.Register(internalpb.ServiceType_SERVICE_TYPE_BATTLE, uint32(internalpb.InternalMessageId_INTERNAL_MESSAGE_ID_RESTORE_PLAYER_STATE_REQUEST), streaming.MessageHandlerFunc(m.restorePlayerState))
}

func (m *Module) restorePlayerState(ctx context.Context, _ streaming.Peer, envelope *internalpb.InternalEnvelope) (*streaming.MessageResult, error) {
	request := &internalpb.RestorePlayerStateRequest{}
	if err := proto.Unmarshal(envelope.Payload, request); err != nil {
		return nil, err
	}
	if request.PlayerId == "" {
		return nil, fmt.Errorf("player_id is required")
	}
	if m.rooms == nil {
		return &streaming.MessageResult{MessageID: uint32(internalpb.InternalMessageId_INTERNAL_MESSAGE_ID_RESTORE_PLAYER_STATE_RESPONSE), Message: &internalpb.RestorePlayerStateResponse{ServiceType: internalpb.ServiceType_SERVICE_TYPE_BATTLE, PlayerId: request.PlayerId, Available: false}}, nil
	}
	result, err := m.rooms.RestoreByPlayerIDSince(ctx, request.PlayerId, request.LastStateVersion, request.AllowIncremental)
	if err != nil {
		if errors.Is(err, domain.ErrRoomNotFound) || errors.Is(err, domain.ErrPlayerNotInRoom) {
			return &streaming.MessageResult{MessageID: uint32(internalpb.InternalMessageId_INTERNAL_MESSAGE_ID_RESTORE_PLAYER_STATE_RESPONSE), Message: &internalpb.RestorePlayerStateResponse{ServiceType: internalpb.ServiceType_SERVICE_TYPE_BATTLE, PlayerId: request.PlayerId, Available: false}}, nil
		}
		return nil, err
	}
	response := &internalpb.RestorePlayerStateResponse{
		ServiceType:      internalpb.ServiceType_SERVICE_TYPE_BATTLE,
		PlayerId:         request.PlayerId,
		StateVersion:     result.Room.StateVersion,
		Available:        true,
		Mode:             restoreModeProto(result.Mode),
		BaseStateVersion: result.BaseStateVersion,
	}
	var payload proto.Message
	switch result.Mode {
	case components.RestoreModeDelta:
		payload = roomDeltaProto(result.Delta)
	case components.RestoreModeNoop:
		payload = nil
	default:
		payload = roomSnapshotProto(result.Room)
	}
	var payloadBytes []byte
	if payload != nil {
		payloadBytes, err = proto.Marshal(payload)
	}
	if err != nil {
		return nil, err
	}
	response.Snapshot = payloadBytes
	return &streaming.MessageResult{MessageID: uint32(internalpb.InternalMessageId_INTERNAL_MESSAGE_ID_RESTORE_PLAYER_STATE_RESPONSE), Message: response}, nil
}

func restoreModeProto(mode components.RestoreMode) internalpb.RestoreMode {
	switch mode {
	case components.RestoreModeDelta:
		return internalpb.RestoreMode_RESTORE_MODE_DELTA
	case components.RestoreModeNoop:
		return internalpb.RestoreMode_RESTORE_MODE_NOOP
	default:
		return internalpb.RestoreMode_RESTORE_MODE_FULL
	}
}

func roomDeltaProto(delta *domain.RoomDelta) *internalpb.BattleRoomDelta {
	result := &internalpb.BattleRoomDelta{}
	if delta == nil {
		return result
	}
	result.RoomId = delta.RoomID
	result.FromStateVersion = delta.FromStateVersion
	result.ToStateVersion = delta.ToStateVersion
	result.Tick = delta.Tick
	result.Status = string(delta.Status)
	result.RemovedPlayerIds = append(result.RemovedPlayerIds, delta.RemovedPlayerIDs...)
	for _, player := range delta.UpsertPlayers {
		result.UpsertPlayers = append(result.UpsertPlayers, &internalpb.BattlePlayerState{
			PlayerId:  player.PlayerID,
			Hp:        player.HP,
			PositionX: player.PositionX,
			PositionY: player.PositionY,
		})
	}
	return result
}

func roomSnapshotProto(room *domain.Room) *internalpb.BattleRoomSnapshot {
	snapshot := &internalpb.BattleRoomSnapshot{
		RoomId:             room.ID,
		Tick:               room.Tick,
		StateVersion:       room.StateVersion,
		Status:             string(room.Status),
		UpdatedAtUnixMilli: room.UpdatedAt.UnixMilli(),
		Players:            make([]*internalpb.BattlePlayerState, 0, len(room.Players)),
	}
	playerIDs := make([]string, 0, len(room.Players))
	for playerID := range room.Players {
		playerIDs = append(playerIDs, playerID)
	}
	sort.Strings(playerIDs)
	for _, playerID := range playerIDs {
		player := room.Players[playerID]
		snapshot.Players = append(snapshot.Players, &internalpb.BattlePlayerState{
			PlayerId:  player.PlayerID,
			Hp:        player.HP,
			PositionX: player.PositionX,
			PositionY: player.PositionY,
		})
	}
	return snapshot
}

func (m *Module) RegisterRoutes(gin.IRouter) {}
