package components

import (
	"context"
	"errors"
	"testing"
	"time"

	"server/common/mongodb"
	"server/common/streaming"
	statepb "server/proto/gen/client/state"
	internalpb "server/proto/gen/internalpb"
	"server/services/lobby/domain"

	"google.golang.org/protobuf/proto"
)

func TestPlayerComponentCreatesSettlesAndRestoresSnapshot(t *testing.T) {
	players := &memoryPlayers{byID: map[string]domain.Player{}, byAccount: map[string]string{}}
	assets := &memoryAssets{records: map[string]domain.Assets{}}
	ledger := &memoryLedger{records: map[string]domain.Settlement{}}
	snapshots := &memorySnapshots{records: map[string]domain.Snapshot{}}
	component := NewPlayerComponent(players, assets, ledger, snapshots, immediateUnitOfWork{}, recordingLinker{})
	component.now = func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }

	ensurePayload, _ := proto.Marshal(&internalpb.EnsurePlayerRequest{AccountId: "account-1"})
	ensure, err := component.ensurePlayer(context.Background(), streaming.Peer{}, &internalpb.InternalEnvelope{Payload: ensurePayload})
	if err != nil {
		t.Fatalf("ensure player: %v", err)
	}

	response := ensure.Message.(*internalpb.EnsurePlayerResponse)
	if response.PlayerId == "" || !response.Created {
		t.Fatalf("unexpected ensure response: %s", response)
	}

	second, err := component.ensurePlayer(context.Background(), streaming.Peer{}, &internalpb.InternalEnvelope{Payload: ensurePayload})
	if err != nil {
		t.Fatalf("ensure existing player: %v", err)
	}

	if second.Message.(*internalpb.EnsurePlayerResponse).PlayerId != response.PlayerId || second.Message.(*internalpb.EnsurePlayerResponse).Created {
		t.Fatalf("existing player was not reused: %s", second.Message)
	}

	settlementPayload, _ := proto.Marshal(&internalpb.SettlementRequest{
		SettlementId: "settlement-1",
		PlayerId:     response.PlayerId,
		AssetType:    "gold",
		Delta:        125,
		Reason:       "battle_settlement",
		Source:       "battle"})
	settlement, err := component.settlement(context.Background(), streaming.Peer{}, &internalpb.InternalEnvelope{Payload: settlementPayload})
	if err != nil {
		t.Fatalf("settle: %v", err)
	}

	settlementResponse := settlement.Message.(*internalpb.SettlementResponse)
	if settlementResponse.Balance != 125 || settlementResponse.AssetVersion != 2 || settlementResponse.Replayed {
		t.Fatalf("unexpected settlement: %s", settlementResponse)
	}

	replayed, err := component.settlement(context.Background(), streaming.Peer{}, &internalpb.InternalEnvelope{Payload: settlementPayload})
	if err != nil {
		t.Fatalf("replay settlement: %v", err)
	}

	if !replayed.Message.(*internalpb.SettlementResponse).Replayed {
		t.Fatal("duplicate settlement was not replayed")
	}

	conflictingPayload, _ := proto.Marshal(&internalpb.SettlementRequest{
		SettlementId: "settlement-1",
		PlayerId:     response.PlayerId,
		AssetType:    "gold",
		Delta:        126,
		Reason:       "battle_settlement",
		Source:       "battle"})
	if _, err := component.settlement(context.Background(), streaming.Peer{}, &internalpb.InternalEnvelope{Payload: conflictingPayload}); !errors.Is(err, domain.ErrSettlementConflict) {
		t.Fatalf("conflicting settlement error = %v, want %v", err, domain.ErrSettlementConflict)
	}

	restorePayload, _ := proto.Marshal(&internalpb.RestorePlayerStateRequest{PlayerId: response.PlayerId})
	restored, err := component.restore(context.Background(), streaming.Peer{}, &internalpb.InternalEnvelope{Payload: restorePayload})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}

	state := restored.Message.(*internalpb.RestorePlayerStateResponse)
	if !state.Available {
		t.Fatal("snapshot was unavailable")
	}

	if state.PayloadType != internalpb.StatePayloadType_STATE_PAYLOAD_TYPE_LOBBY_SNAPSHOT || state.SchemaVersion != StateSchemaVersion {
		t.Fatalf("unexpected state metadata: %s", state)
	}

	snapshot := &statepb.LobbyStateSnapshot{}
	if err := proto.Unmarshal(state.Snapshot, snapshot); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}

	if snapshot.Currency["gold"] != 125 || snapshot.AssetVersion != 2 {
		t.Fatalf("unexpected snapshot: %s", snapshot)
	}
}

type immediateUnitOfWork struct{}

func (immediateUnitOfWork) Execute(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

var _ mongodb.UnitOfWork = immediateUnitOfWork{}

type recordingLinker struct{}

func (recordingLinker) LinkPlayer(context.Context, string, string) error { return nil }

type memoryPlayers struct {
	byID      map[string]domain.Player
	byAccount map[string]string
}

func (*memoryPlayers) EnsureIndexes(context.Context) error { return nil }

func (r *memoryPlayers) Create(_ context.Context, player domain.Player) error {
	if _, ok := r.byAccount[player.AccountID]; ok {
		return domain.ErrInvalidPlayer
	}

	r.byID[player.ID] = player
	r.byAccount[player.AccountID] = player.ID
	return nil
}

func (r *memoryPlayers) FindDefaultByAccountID(_ context.Context, accountID string) (*domain.Player, error) {
	id, ok := r.byAccount[accountID]
	if !ok {
		return nil, domain.ErrPlayerNotFound
	}

	player := r.byID[id]
	return &player, nil
}

func (r *memoryPlayers) FindByID(_ context.Context, playerID string) (*domain.Player, error) {
	player, ok := r.byID[playerID]
	if !ok {
		return nil, domain.ErrPlayerNotFound
	}

	return &player, nil
}

type memoryAssets struct{ records map[string]domain.Assets }

func (*memoryAssets) EnsureIndexes(context.Context) error { return nil }

func (r *memoryAssets) Create(_ context.Context, assets domain.Assets) error {
	r.records[assets.PlayerID] = assets
	return nil
}

func (r *memoryAssets) FindByPlayerID(_ context.Context, playerID string) (*domain.Assets, error) {
	assets, ok := r.records[playerID]
	if !ok {
		return nil, domain.ErrAssetNotFound
	}

	assets.Currency = cloneCurrency(assets.Currency)
	return &assets, nil
}

func (r *memoryAssets) ApplyDelta(_ context.Context, playerID, assetType string, delta int64) (*domain.Assets, error) {
	assets, ok := r.records[playerID]
	if !ok {
		return nil, domain.ErrAssetNotFound
	}

	if assets.Currency[assetType]+delta < 0 {
		return nil, domain.ErrInsufficientCurrency
	}

	assets.Currency[assetType] += delta
	assets.AssetVersion++
	r.records[playerID] = assets
	result := assets
	result.Currency = cloneCurrency(assets.Currency)
	return &result, nil
}

type memoryLedger struct{ records map[string]domain.Settlement }

func (*memoryLedger) EnsureIndexes(context.Context) error { return nil }

func (r *memoryLedger) Create(_ context.Context, settlement domain.Settlement) error {
	if _, ok := r.records[settlement.ID]; ok {
		return domain.ErrSettlementApplied
	}

	r.records[settlement.ID] = settlement
	return nil
}

func (r *memoryLedger) FindBySettlementID(_ context.Context, id string) (*domain.Settlement, error) {
	settlement, ok := r.records[id]
	if !ok {
		return nil, domain.ErrSettlementNotFound
	}

	return &settlement, nil
}

type memorySnapshots struct{ records map[string]domain.Snapshot }

func (*memorySnapshots) EnsureIndexes(context.Context) error { return nil }

func (r *memorySnapshots) FindByPlayerID(_ context.Context, playerID string) (*domain.Snapshot, error) {
	snapshot, ok := r.records[playerID]
	if !ok {
		return nil, domain.ErrPlayerNotFound
	}

	return &snapshot, nil
}

func (r *memorySnapshots) Save(_ context.Context, snapshot domain.Snapshot) error {
	r.records[snapshot.Player.ID] = snapshot
	return nil
}

func cloneCurrency(values map[string]int64) map[string]int64 {
	cloned := make(map[string]int64, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
