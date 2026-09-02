package gateway

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"server/common/app"
	"server/common/streaming"
	internalpb "server/proto/gen/internalpb"
	servicecommon "server/services/common"
	"server/services/gateway/session"
)

type Module struct {
	*servicecommon.Module
	handler *Handler
}

func NewModule(deps app.Dependencies) *Module {
	base := servicecommon.NewModule("gateway", internalpb.ServiceType_SERVICE_TYPE_GATEWAY, deps)
	store := session.NewRedisStore(deps.Redis, "game01:gateway:session:")
	manager := session.NewManager(store, deps.Config.App.InstanceID, deps.Config.Gateway.SessionTTL, deps.Config.Gateway.ReconnectGrace)
	authenticator := NewUserCenterAuthenticator(func() (*streaming.Client, bool) {
		return base.Client("usercenter")
	})
	dispatcher := NewDispatcher(authenticator, manager)
	return &Module{
		Module:  base,
		handler: NewHandler(dispatcher),
	}
}

func (m *Module) RegisterRoutes(router gin.IRouter) {
	router.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "pong"})
	})
	m.handler.RegisterRoutes(router)
}

func (m *Module) Start(ctx context.Context) error {
	return m.Module.Start(ctx)
}
