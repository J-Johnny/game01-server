package gateway

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/protobuf/proto"
	"server/common/streaming"
	internalpb "server/proto/gen/internalpb"
)

type PlayerResolver interface {
	EnsurePlayer(context.Context, string) (string, error)
}

type LobbyPlayerResolver struct {
	client func() (*streaming.Client, bool)
}

func NewLobbyPlayerResolver(client func() (*streaming.Client, bool)) *LobbyPlayerResolver {
	return &LobbyPlayerResolver{client: client}
}

func (r *LobbyPlayerResolver) EnsurePlayer(ctx context.Context, accountID string) (string, error) {
	if r == nil || r.client == nil || accountID == "" {
		return "", errors.New("lobby player resolver is not configured")
	}
	client, ok := r.client()
	if !ok || client == nil {
		return "", errors.New("lobby service is unavailable")
	}
	payload, err := proto.Marshal(&internalpb.EnsurePlayerRequest{AccountId: accountID})
	if err != nil {
		return "", err
	}
	response, err := client.Request(ctx, &internalpb.InternalEnvelope{TargetService: internalpb.ServiceType_SERVICE_TYPE_LOBBY, MessageId: uint32(internalpb.LobbyMessageId_LOBBY_MESSAGE_ID_ENSURE_PLAYER_REQUEST), Payload: payload})
	if err != nil {
		return "", fmt.Errorf("ensure lobby player: %w", err)
	}
	if response.Kind == internalpb.EnvelopeKind_ENVELOPE_KIND_ERROR {
		return "", fmt.Errorf("lobby player error %d: %s", response.ErrorCode, response.ErrorMessage)
	}
	result := &internalpb.EnsurePlayerResponse{}
	if err := proto.Unmarshal(response.Payload, result); err != nil {
		return "", err
	}
	if result.AccountId != accountID || result.PlayerId == "" {
		return "", errors.New("lobby returned invalid player")
	}
	return result.PlayerId, nil
}
