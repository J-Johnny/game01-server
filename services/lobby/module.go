package lobby

import (
	"context"
	"github.com/gin-gonic/gin"
	"google.golang.org/protobuf/proto"
	"server/common/app"
	"server/common/streaming"
	internalpb "server/proto/gen/internalpb"
	servicecommon "server/services/common"
)

type Module struct{ *servicecommon.Module }

func NewModule(deps app.Dependencies) *Module {
	return &Module{
		Module: servicecommon.NewModule("lobby", internalpb.ServiceType_SERVICE_TYPE_LOBBY, deps),
	}
}

func (m *Module) RegisterInternal(router *streaming.Router) {
	m.Module.RegisterInternal(router)
	router.Register(internalpb.ServiceType_SERVICE_TYPE_LOBBY, uint32(internalpb.InternalMessageId_INTERNAL_MESSAGE_ID_RESTORE_PLAYER_STATE_REQUEST), streaming.MessageHandlerFunc(m.restorePlayerState))
}

func (m *Module) restorePlayerState(_ context.Context, _ streaming.Peer, envelope *internalpb.InternalEnvelope) (*streaming.MessageResult, error) {
	request := &internalpb.RestorePlayerStateRequest{}
	if err := proto.Unmarshal(envelope.Payload, request); err != nil {
		return nil, err
	}
	return &streaming.MessageResult{MessageID: uint32(internalpb.InternalMessageId_INTERNAL_MESSAGE_ID_RESTORE_PLAYER_STATE_RESPONSE), Message: &internalpb.RestorePlayerStateResponse{ServiceType: internalpb.ServiceType_SERVICE_TYPE_LOBBY, PlayerId: request.PlayerId, Available: false}}, nil
}

func (m *Module) RegisterRoutes(gin.IRouter) {}
