package usercenter

import (
	"context"
	"fmt"
	"time"

	"server/common/app"
	commonmongo "server/common/mongodb"
	"server/common/streaming"
	internalpb "server/proto/gen/internalpb"
	servicecommon "server/services/common"
	"server/services/usercenter/components"
	"server/services/usercenter/repository"
	mongorepository "server/services/usercenter/repository/mongo"

	"github.com/gin-gonic/gin"
)

const (
	AccountsCollection      = "accounts"
	IdentitiesCollection    = "account_identities"
	RefreshTokensCollection = "refresh_tokens"
	IdempotencyCollection   = "idempotency_records"
)

type Module struct {
	*servicecommon.Module
	domainAuth       *components.DomainAuthComponent
	clientAuth       *components.ClientAuthComponent
	domainAccounts   repository.AccountRepository
	domainIdentities repository.IdentityRepository
	domainTokens     repository.RefreshTokenRepository
	idempotency      repository.IdempotencyRepository
	initErr          error
}

func NewModule(deps app.Dependencies) *Module {
	module := &Module{
		Module: servicecommon.NewModule("usercenter", internalpb.ServiceType_SERVICE_TYPE_USERCENTER, deps),
	}
	if driverResources, ok := deps.Mongo.(commonmongo.DriverResources); ok && driverResources.DriverClient() != nil && driverResources.DriverDatabase() != nil {
		unitOfWork := commonmongo.NewMongoUnitOfWork(driverResources.DriverClient())
		module.domainAccounts = mongorepository.NewAccountRepository(driverResources.DriverCollection(AccountsCollection))
		module.domainIdentities = mongorepository.NewIdentityRepository(driverResources.DriverCollection(IdentitiesCollection))
		module.domainTokens = mongorepository.NewRefreshTokenRepository(driverResources.DriverCollection(RefreshTokensCollection), unitOfWork)
		module.idempotency = mongorepository.NewIdempotencyRepository(driverResources.DriverCollection(IdempotencyCollection))
		module.domainAuth = components.NewDomainAuthComponent(module.domainAccounts, module.domainIdentities, module.domainTokens, unitOfWork, deps.Config.UserCenter.RefreshTokenTTL, module.idempotency).WithIdempotencyTTL(deps.Config.UserCenter.IdempotencyTTL)
		module.clientAuth = components.NewClientAuthComponent(module.domainAuth)
	} else {
		module.initErr = fmt.Errorf("official MongoDB Driver resources are required")
	}
	return module
}

func (m *Module) Start(ctx context.Context) error {
	if m.initErr != nil {
		return m.initErr
	}
	if err := m.domainAccounts.EnsureIndexes(ctx); err != nil {
		return fmt.Errorf("ensure accounts indexes: %w", err)
	}
	if err := m.domainIdentities.EnsureIndexes(ctx); err != nil {
		return fmt.Errorf("ensure account identities indexes: %w", err)
	}
	if err := m.domainTokens.EnsureIndexes(ctx); err != nil {
		return fmt.Errorf("ensure refresh token indexes: %w", err)
	}
	if err := m.idempotency.EnsureIndexes(ctx); err != nil {
		return fmt.Errorf("ensure idempotency indexes: %w", err)
	}
	if coordinator, ok := m.idempotency.(repository.IdempotencyCoordinator); ok {
		if _, err := coordinator.RecoverExpired(ctx, time.Now().UTC()); err != nil {
			return fmt.Errorf("recover expired idempotency records: %w", err)
		}
	}

	return m.Module.Start(ctx)
}

func (m *Module) RegisterRoutes(gin.IRouter) {}

func (m *Module) RegisterInternal(router *streaming.Router) {
	m.Module.RegisterInternal(router)
	if m.domainAuth == nil {
		return
	}
	m.domainAuth.RegisterInternal(router)
	if m.clientAuth != nil {
		m.clientAuth.RegisterInternal(router)
	}
}
