package lobby

import (
	"github.com/gin-gonic/gin"
	"server/common/app"
	internalpb "server/proto/gen/internalpb"
	servicecommon "server/services/common"
)

type Module struct{ *servicecommon.Module }

func NewModule(deps app.Dependencies) *Module {
	return &Module{Module: servicecommon.NewModule("lobby", internalpb.ServiceType_SERVICE_TYPE_LOBBY, deps)}
}

func (m *Module) RegisterRoutes(gin.IRouter) {}
