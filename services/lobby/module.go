package lobby

import (
	"context"
	"fmt"

	"server/common/app"
	commonmongo "server/common/mongodb"
	"server/common/streaming"
	internalpb "server/proto/gen/internalpb"
	servicecommon "server/services/common"
	"server/services/lobby/components"
	"server/services/lobby/repository"
	mongorepository "server/services/lobby/repository/mongo"

	"github.com/gin-gonic/gin"
)

const (
	PlayersCollection   = "players"
	AssetsCollection    = "player_assets"
	LedgerCollection    = "asset_ledger"
	SnapshotsCollection = "player_snapshots"
)

type Module struct {
	*servicecommon.Module
	component *components.PlayerComponent
	players   repository.PlayerRepository
	assets    repository.AssetRepository
	ledger    repository.LedgerRepository
	snapshots repository.SnapshotRepository
	lifecycle *servicecommon.SessionLifecycleConsumer
	initErr   error
}

func NewModule(deps app.Dependencies) *Module {
	module := &Module{
		Module:    servicecommon.NewModule("lobby", internalpb.ServiceType_SERVICE_TYPE_LOBBY, deps),
		lifecycle: servicecommon.NewSessionLifecycleConsumer(),
	}
	driverResources, ok := deps.Mongo.(commonmongo.DriverResources)
	if !ok || driverResources.DriverClient() == nil || driverResources.DriverDatabase() == nil {
		module.initErr = fmt.Errorf("official MongoDB Driver resources are required")
		return module
	}
	module.players = mongorepository.NewPlayerRepository(driverResources.DriverCollection(PlayersCollection))
	module.assets = mongorepository.NewAssetRepository(driverResources.DriverCollection(AssetsCollection))
	module.ledger = mongorepository.NewLedgerRepository(driverResources.DriverCollection(LedgerCollection))
	module.snapshots = mongorepository.NewSnapshotRepository(driverResources.DriverCollection(SnapshotsCollection))
	unitOfWork := commonmongo.NewMongoUnitOfWork(driverResources.DriverClient())
	linker := components.NewUserCenterPlayerLinker(func() (*streaming.Client, bool) { return module.Client("usercenter") })
	module.component = components.NewPlayerComponent(module.players, module.assets, module.ledger, module.snapshots, unitOfWork, linker)
	return module
}

func (m *Module) Start(ctx context.Context) error {
	if m.initErr != nil {
		return m.initErr
	}

	for _, item := range []struct {
		name   string
		ensure func(context.Context) error
	}{
		{"players", m.players.EnsureIndexes},
		{"assets", m.assets.EnsureIndexes},
		{"ledger", m.ledger.EnsureIndexes},
		{"snapshots", m.snapshots.EnsureIndexes},
	} {
		if err := item.ensure(ctx); err != nil {
			return fmt.Errorf("ensure %s indexes: %w", item.name, err)
		}
	}
	return m.Module.Start(ctx)
}

func (m *Module) RegisterRoutes(gin.IRouter) {}

func (m *Module) RegisterInternal(router *streaming.Router) {
	m.Module.RegisterInternal(router)
	if m.component != nil {
		m.component.RegisterInternal(router)
	}

	if m.lifecycle != nil {
		m.lifecycle.Register(router, internalpb.ServiceType_SERVICE_TYPE_LOBBY)
	}
}
