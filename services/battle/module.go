package battle

import (
	"context"
	"github.com/gin-gonic/gin"
	"google.golang.org/protobuf/proto"
	"server/common/app"
	"server/common/streaming"
	internalpb "server/proto/gen/internalpb"
	servicecommon "server/services/common"
)

type Module struct {
	*servicecommon.Module
	settlements *LobbySettlementClient
}

func NewModule(deps app.Dependencies) *Module {
	module := &Module{Module: servicecommon.NewModule("battle", internalpb.ServiceType_SERVICE_TYPE_BATTLE, deps)}
	module.settlements = NewLobbySettlementClient(func() (*streaming.Client, bool) { return module.Client("lobby") })
	return module
}

func (m *Module) Settlements() *LobbySettlementClient { return m.settlements }

func (m *Module) RegisterInternal(router *streaming.Router) {
	m.Module.RegisterInternal(router)
	router.Register(internalpb.ServiceType_SERVICE_TYPE_BATTLE, uint32(internalpb.InternalMessageId_INTERNAL_MESSAGE_ID_RESTORE_PLAYER_STATE_REQUEST), streaming.MessageHandlerFunc(m.restorePlayerState))
}

func (m *Module) restorePlayerState(_ context.Context, _ streaming.Peer, envelope *internalpb.InternalEnvelope) (*streaming.MessageResult, error) {
	request := &internalpb.RestorePlayerStateRequest{}
	if err := proto.Unmarshal(envelope.Payload, request); err != nil {
		return nil, err
	}
	return &streaming.MessageResult{MessageID: uint32(internalpb.InternalMessageId_INTERNAL_MESSAGE_ID_RESTORE_PLAYER_STATE_RESPONSE), Message: &internalpb.RestorePlayerStateResponse{ServiceType: internalpb.ServiceType_SERVICE_TYPE_BATTLE, PlayerId: request.PlayerId, Available: false}}, nil
}

func (m *Module) RegisterRoutes(gin.IRouter) {}
