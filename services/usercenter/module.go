package usercenter

import (
	"context"
	"fmt"

	"server/common/app"
	"server/common/streaming"
	internalpb "server/proto/gen/internalpb"
	servicecommon "server/services/common"
	"server/services/usercenter/components"
	"server/services/usercenter/repository"

	"github.com/gin-gonic/gin"
)

var ROLE_COLLECTION = "user_center"

type Module struct {
	*servicecommon.Module
	accounts repository.IAccountRepository
	auth     *components.AuthComponent
}

func NewModule(deps app.Dependencies) *Module {
	accounts := repository.NewAccountRepository(deps.Mongo.Collection(ROLE_COLLECTION))
	return &Module{
		Module:   servicecommon.NewModule("usercenter", internalpb.ServiceType_SERVICE_TYPE_USERCENTER, deps),
		accounts: accounts,
		auth:     components.NewAuthComponent(accounts, deps.Config.UserCenter.RefreshTokenTTL),
	}
}

func (m *Module) Start(ctx context.Context) error {
	if err := m.accounts.EnsureIndexes(ctx); err != nil {
		return fmt.Errorf("ensure user center indexes: %w", err)
	}

	return m.Module.Start(ctx)
}

func (m *Module) Accounts() repository.IAccountRepository {
	return m.accounts
}

func (m *Module) RegisterRoutes(gin.IRouter) {}

func (m *Module) RegisterInternal(router *streaming.Router) {
	m.Module.RegisterInternal(router)
	m.auth.RegisterInternal(router)
}
