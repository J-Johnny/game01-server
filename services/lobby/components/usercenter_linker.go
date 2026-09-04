package components

import (
	"context"
	"errors"
	"fmt"

	"server/common/streaming"
	internalpb "server/proto/gen/internalpb"

	"google.golang.org/protobuf/proto"
)

type PlayerLinker interface {
	LinkPlayer(context.Context, string, string) error
}

type UserCenterPlayerLinker struct {
	client func() (*streaming.Client, bool)
}

func NewUserCenterPlayerLinker(client func() (*streaming.Client, bool)) *UserCenterPlayerLinker {
	return &UserCenterPlayerLinker{client: client}
}

func (l *UserCenterPlayerLinker) LinkPlayer(ctx context.Context, accountID, playerID string) error {
	if l == nil || l.client == nil || accountID == "" || playerID == "" {
		return errors.New("usercenter player linker is not configured")
	}

	client, ok := l.client()
	if !ok || client == nil {
		return errors.New("usercenter service is unavailable")
	}

	payload, err := proto.Marshal(&internalpb.LinkPlayerRequest{
		AccountId: accountID,
		PlayerId:  playerID,
	})
	if err != nil {
		return err
	}

	response, err := client.Request(ctx, &internalpb.InternalEnvelope{
		TargetService: internalpb.ServiceType_SERVICE_TYPE_USERCENTER,
		MessageId:     uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_LINK_PLAYER_REQUEST),
		Payload:       payload,
	})
	if err != nil {
		return err
	}

	if response.Kind == internalpb.EnvelopeKind_ENVELOPE_KIND_ERROR {
		return fmt.Errorf("usercenter link player error %d: %s", response.ErrorCode, response.ErrorMessage)
	}

	return nil
}
