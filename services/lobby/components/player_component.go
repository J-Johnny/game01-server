package components

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"
	"server/common/idgen"
	"server/common/mongodb"
	"server/common/streaming"
	internalpb "server/proto/gen/internalpb"
	"server/services/lobby/domain"
	"server/services/lobby/repository"
)

type PlayerComponent struct {
	players    repository.PlayerRepository
	assets     repository.AssetRepository
	ledger     repository.LedgerRepository
	snapshot   repository.SnapshotRepository
	unitOfWork mongodb.UnitOfWork
	linker     PlayerLinker
	now        func() time.Time
}

func NewPlayerComponent(players repository.PlayerRepository, assets repository.AssetRepository, ledger repository.LedgerRepository, snapshot repository.SnapshotRepository, unitOfWork mongodb.UnitOfWork, linker PlayerLinker) *PlayerComponent {
	return &PlayerComponent{players: players, assets: assets, ledger: ledger, snapshot: snapshot, unitOfWork: unitOfWork, linker: linker, now: time.Now}
}

func (c *PlayerComponent) RegisterInternal(router *streaming.Router) {
	router.Register(internalpb.ServiceType_SERVICE_TYPE_LOBBY, uint32(internalpb.LobbyMessageId_LOBBY_MESSAGE_ID_ENSURE_PLAYER_REQUEST), streaming.MessageHandlerFunc(c.ensurePlayer))
	router.Register(internalpb.ServiceType_SERVICE_TYPE_LOBBY, uint32(internalpb.LobbyMessageId_LOBBY_MESSAGE_ID_SETTLEMENT_REQUEST), streaming.MessageHandlerFunc(c.settlement))
	router.Register(internalpb.ServiceType_SERVICE_TYPE_LOBBY, uint32(internalpb.InternalMessageId_INTERNAL_MESSAGE_ID_RESTORE_PLAYER_STATE_REQUEST), streaming.MessageHandlerFunc(c.restore))
}

func (c *PlayerComponent) ensurePlayer(ctx context.Context, _ streaming.Peer, envelope *internalpb.InternalEnvelope) (*streaming.MessageResult, error) {
	request := &internalpb.EnsurePlayerRequest{}
	if err := proto.Unmarshal(envelope.Payload, request); err != nil || strings.TrimSpace(request.AccountId) == "" {
		return nil, errors.New("account_id is required")
	}
	accountID := strings.TrimSpace(request.AccountId)
	player, err := c.players.FindDefaultByAccountID(ctx, accountID)
	created := false
	if errors.Is(err, domain.ErrPlayerNotFound) {
		playerID, idErr := idgen.NewUUID()
		if idErr != nil {
			return nil, idErr
		}
		now := c.now().UTC()
		candidate := domain.Player{ID: playerID, AccountID: accountID, Nickname: "Player", Region: "global", IsDefault: true, ProfileVersion: 1, CreatedAt: now, UpdatedAt: now}
		initialAssets := domain.Assets{PlayerID: playerID, Currency: map[string]int64{"gold": 0, "diamond": 0}, AssetVersion: 1, UpdatedAt: now}
		create := func(transactionContext context.Context) error {
			if err := c.players.Create(transactionContext, candidate); err != nil {
				return err
			}
			if err := c.assets.Create(transactionContext, initialAssets); err != nil {
				return err
			}
			return c.snapshot.Save(transactionContext, domain.Snapshot{Player: candidate, Assets: initialAssets, SchemaVersion: 1, CreatedAt: now})
		}
		if c.unitOfWork != nil {
			err = c.unitOfWork.Execute(ctx, create)
		} else {
			err = create(ctx)
		}
		if err != nil {
			player, err = c.players.FindDefaultByAccountID(ctx, accountID)
		} else {
			player = &candidate
			created = true
		}
	}
	if err != nil {
		return nil, fmt.Errorf("ensure player: %w", err)
	}
	if c.linker == nil {
		return nil, errors.New("usercenter client is not configured")
	}
	if err := c.linker.LinkPlayer(ctx, accountID, player.ID); err != nil {
		return nil, fmt.Errorf("link player: %w", err)
	}
	return &streaming.MessageResult{MessageID: uint32(internalpb.LobbyMessageId_LOBBY_MESSAGE_ID_ENSURE_PLAYER_RESPONSE), Message: &internalpb.EnsurePlayerResponse{AccountId: accountID, PlayerId: player.ID, Created: created}}, nil
}

func (c *PlayerComponent) settlement(ctx context.Context, _ streaming.Peer, envelope *internalpb.InternalEnvelope) (*streaming.MessageResult, error) {
	request := &internalpb.SettlementRequest{}
	if err := proto.Unmarshal(envelope.Payload, request); err != nil || request.SettlementId == "" || request.PlayerId == "" || request.AssetType == "" || request.Delta == 0 || request.Reason == "" || request.Source == "" {
		return nil, domain.ErrInvalidSettlement
	}
	settlement := domain.Settlement{ID: request.SettlementId, PlayerID: request.PlayerId, AssetType: request.AssetType, Delta: request.Delta, Reason: request.Reason, Source: request.Source, CreatedAt: c.now().UTC()}
	if existing, err := c.ledger.FindBySettlementID(ctx, settlement.ID); err == nil {
		if existing.PlayerID != settlement.PlayerID || existing.AssetType != settlement.AssetType || existing.Delta != settlement.Delta || existing.Reason != settlement.Reason || existing.Source != settlement.Source {
			return nil, domain.ErrSettlementConflict
		}
		return c.settlementResult(ctx, existing, true)
	} else if !errors.Is(err, domain.ErrSettlementNotFound) {
		return nil, err
	}
	var assets *domain.Assets
	execute := func(transactionContext context.Context) error {
		if err := c.ledger.Create(transactionContext, settlement); err != nil {
			return err
		}
		var err error
		assets, err = c.assets.ApplyDelta(transactionContext, settlement.PlayerID, settlement.AssetType, settlement.Delta)
		if err != nil {
			return err
		}
		player, err := c.players.FindByID(transactionContext, settlement.PlayerID)
		if err != nil {
			return err
		}
		return c.snapshot.Save(transactionContext, domain.Snapshot{Player: *player, Assets: *assets, SchemaVersion: 1, CreatedAt: c.now().UTC()})
	}
	if c.unitOfWork != nil {
		if err := c.unitOfWork.Execute(ctx, execute); err != nil {
			if errors.Is(err, domain.ErrSettlementApplied) {
				return c.replaySettlement(ctx, settlement.ID)
			}
			return nil, err
		}
	} else if err := execute(ctx); err != nil {
		if errors.Is(err, domain.ErrSettlementApplied) {
			return c.replaySettlement(ctx, settlement.ID)
		}
		return nil, err
	}
	return c.settlementResult(ctx, &settlement, false)
}

func (c *PlayerComponent) replaySettlement(ctx context.Context, settlementID string) (*streaming.MessageResult, error) {
	settlement, err := c.ledger.FindBySettlementID(ctx, settlementID)
	if err != nil {
		return nil, fmt.Errorf("load replayed settlement: %w", err)
	}
	return c.settlementResult(ctx, settlement, true)
}

func (c *PlayerComponent) settlementResult(ctx context.Context, settlement *domain.Settlement, replayed bool) (*streaming.MessageResult, error) {
	assets, err := c.assets.FindByPlayerID(ctx, settlement.PlayerID)
	if err != nil {
		return nil, err
	}
	return &streaming.MessageResult{
		MessageID: uint32(internalpb.LobbyMessageId_LOBBY_MESSAGE_ID_SETTLEMENT_RESPONSE),
		Message: &internalpb.SettlementResponse{
			SettlementId: settlement.ID,
			PlayerId:     settlement.PlayerID,
			AssetType:    settlement.AssetType,
			Balance:      assets.Balance(settlement.AssetType),
			AssetVersion: assets.AssetVersion,
			Replayed:     replayed,
		},
	}, nil
}

func (c *PlayerComponent) restore(ctx context.Context, _ streaming.Peer, envelope *internalpb.InternalEnvelope) (*streaming.MessageResult, error) {
	request := &internalpb.RestorePlayerStateRequest{}
	if err := proto.Unmarshal(envelope.Payload, request); err != nil || request.PlayerId == "" {
		return nil, errors.New("player_id is required")
	}
	snapshot, err := c.snapshot.FindByPlayerID(ctx, request.PlayerId)
	if errors.Is(err, domain.ErrPlayerNotFound) {
		player, playerErr := c.players.FindByID(ctx, request.PlayerId)
		if playerErr != nil {
			return nil, playerErr
		}
		assets, assetErr := c.assets.FindByPlayerID(ctx, request.PlayerId)
		if assetErr != nil {
			return nil, assetErr
		}
		snapshot = &domain.Snapshot{Player: *player, Assets: *assets, SchemaVersion: 1, CreatedAt: c.now().UTC()}
		_ = c.snapshot.Save(ctx, *snapshot)
	} else if err != nil {
		return nil, err
	}
	stateVersion := snapshot.StateVersion()
	mode := internalpb.RestoreMode_RESTORE_MODE_FULL
	baseStateVersion := uint64(0)
	var payload []byte
	if request.LastStateVersion == stateVersion {
		mode = internalpb.RestoreMode_RESTORE_MODE_NOOP
		baseStateVersion = request.LastStateVersion
	} else {
		payload, err = MarshalStateSnapshot(*snapshot)
	}
	if err != nil {
		return nil, err
	}
	return &streaming.MessageResult{
		MessageID: uint32(internalpb.InternalMessageId_INTERNAL_MESSAGE_ID_RESTORE_PLAYER_STATE_RESPONSE),
		Message: &internalpb.RestorePlayerStateResponse{
			ServiceType:      internalpb.ServiceType_SERVICE_TYPE_LOBBY,
			PlayerId:         request.PlayerId,
			StateVersion:     stateVersion,
			Snapshot:         payload,
			Available:        true,
			Mode:             mode,
			BaseStateVersion: baseStateVersion,
			PayloadType:      lobbyStatePayloadType(mode),
			SchemaVersion:    StateSchemaVersion,
		},
	}, nil
}

func lobbyStatePayloadType(mode internalpb.RestoreMode) internalpb.StatePayloadType {
	if mode == internalpb.RestoreMode_RESTORE_MODE_NOOP {
		return internalpb.StatePayloadType_STATE_PAYLOAD_TYPE_UNSPECIFIED
	}
	return internalpb.StatePayloadType_STATE_PAYLOAD_TYPE_LOBBY_SNAPSHOT
}
