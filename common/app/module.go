package app

import (
	"context"
	"log/slog"

	"server/common/config"
	"server/common/discovery"
	"server/common/idgen"
	"server/common/mongodb"
	"server/common/observability"

	"github.com/gin-gonic/gin"
	redis "github.com/redis/go-redis/v9"
)

type Dependencies struct {
	Config      config.Config
	Logger      *slog.Logger
	Redis       *redis.Client
	Mongo       mongodb.IMongoModule
	Registry    discovery.Registry
	IDGenerator *idgen.Generator
	Metrics     *observability.Metrics
}

type Module interface {
	Name() string
	RegisterRoutes(gin.IRouter)
	Start(context.Context) error
	Stop(context.Context) error
}

func StartEnabled(ctx context.Context, r gin.IRouter, d Dependencies, ms []Module) ([]Module, error) {
	started := make([]Module, 0, len(ms))
	for _, m := range ms {
		if !d.Config.Services[m.Name()].Enabled {
			continue
		}
		m.RegisterRoutes(r)
		if e := m.Start(ctx); e != nil {
			return started, e
		}
		started = append(started, m)
	}
	return started, nil
}

func StopReverse(ctx context.Context, ms []Module) error {
	var first error
	for i := len(ms) - 1; i >= 0; i-- {
		if e := ms[i].Stop(ctx); e != nil && first == nil {
			first = e
		}
	}
	return first
}
