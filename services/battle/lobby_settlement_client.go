package battle

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/protobuf/proto"
	"server/common/streaming"
	internalpb "server/proto/gen/internalpb"
)

type Settlement struct {
	ID        string
	PlayerID  string
	AssetType string
	Delta     int64
	Reason    string
}

type SettlementResult struct {
	Balance      int64
	AssetVersion uint64
	Replayed     bool
}

type LobbySettlementClient struct {
	client func() (*streaming.Client, bool)
}

func NewLobbySettlementClient(client func() (*streaming.Client, bool)) *LobbySettlementClient {
	return &LobbySettlementClient{client: client}
}

func (c *LobbySettlementClient) Submit(ctx context.Context, settlement Settlement) (SettlementResult, error) {
	if c == nil || c.client == nil || settlement.ID == "" || settlement.PlayerID == "" || settlement.AssetType == "" || settlement.Delta == 0 || settlement.Reason == "" {
		return SettlementResult{}, errors.New("invalid battle settlement")
	}
	client, ok := c.client()
	if !ok || client == nil {
		return SettlementResult{}, errors.New("lobby service is unavailable")
	}
	payload, err := proto.Marshal(&internalpb.SettlementRequest{SettlementId: settlement.ID, PlayerId: settlement.PlayerID, AssetType: settlement.AssetType, Delta: settlement.Delta, Reason: settlement.Reason, Source: "battle"})
	if err != nil {
		return SettlementResult{}, err
	}
	response, err := client.Request(ctx, &internalpb.InternalEnvelope{TargetService: internalpb.ServiceType_SERVICE_TYPE_LOBBY, MessageId: uint32(internalpb.LobbyMessageId_LOBBY_MESSAGE_ID_SETTLEMENT_REQUEST), PlayerId: settlement.PlayerID, Payload: payload})
	if err != nil {
		return SettlementResult{}, fmt.Errorf("submit lobby settlement: %w", err)
	}
	if response.Kind == internalpb.EnvelopeKind_ENVELOPE_KIND_ERROR {
		return SettlementResult{}, fmt.Errorf("lobby settlement error %d: %s", response.ErrorCode, response.ErrorMessage)
	}
	result := &internalpb.SettlementResponse{}
	if err := proto.Unmarshal(response.Payload, result); err != nil {
		return SettlementResult{}, err
	}
	return SettlementResult{Balance: result.Balance, AssetVersion: result.AssetVersion, Replayed: result.Replayed}, nil
}
