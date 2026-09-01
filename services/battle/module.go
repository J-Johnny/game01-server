package battle

import (
	"github.com/gin-gonic/gin"
	"server/common/app"
	internalpb "server/proto/gen/internalpb"
	servicecommon "server/services/common"
)

type Module struct{ *servicecommon.Module }

func NewModule(deps app.Dependencies) *Module {
	return &Module{Module: servicecommon.NewModule("battle", internalpb.ServiceType_SERVICE_TYPE_BATTLE, deps)}
}

func (m *Module) RegisterRoutes(gin.IRouter) {}
