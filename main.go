package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"server/common/app"
	"server/common/config"
	"server/common/discovery"
	discoverybootstrap "server/common/discovery/bootstrap"
	"server/common/idgen"
	"server/common/logging"
	commonmongo "server/common/mongodb"
	"server/common/observability"
	commonredis "server/common/redis"
	"server/common/streaming"
	"server/services/battle"
	"server/services/gateway"
	"server/services/lobby"
	"server/services/usercenter"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
)

func main() {
	cfgPath := config.PathFromFlags()
	cfg, e := config.Load(cfgPath)
	if e != nil {
		slog.Error("load config", "error", e)
		os.Exit(1)
	}
	logger := logging.New(cfg.App.Name, cfg.App.InstanceID, cfg.App.Environment)
	slog.SetDefault(logger)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	discoveryResources, e := discoverybootstrap.Open(ctx, cfg.Discovery)
	if e != nil {
		slog.Error("open discovery", "error", e)
		os.Exit(1)
	}
	defer discoveryResources.Close()

	redisClient, e := commonredis.New(context.Background(), cfg.Redis)
	if e != nil {
		slog.Error("connect redis", "error", e)
		os.Exit(1)
	}
	defer redisClient.Close()

	mongoResources := commonmongo.NewMongoModule(&cfg)
	e = mongoResources.Init()
	if e != nil {
		slog.Error("connect mongo", "error", e)
		os.Exit(1)
	}
	defer func() {
		if err := mongoResources.Shutdown(); err != nil {
			slog.Error("close mongo", "error", err)
		}
	}()
	idGenerator, e := idgen.New(cfg.IDGenerator.NodeID)
	if e != nil {
		slog.Error("create id generator", "error", e)
		os.Exit(1)
	}

	deps := app.Dependencies{
		Config:      cfg,
		Logger:      logger,
		Redis:       redisClient,
		Mongo:       mongoResources,
		Registry:    discoveryResources.Registry,
		IDGenerator: idGenerator,
		Metrics:     observability.NewMetrics()}
	r := gin.New()
	r.Use(gin.Recovery(), logging.GinAccess(logger))
	r.GET("/metrics", gin.WrapH(deps.Metrics.Handler()))
	r.GET("/healthz", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	mods := enabledModules(deps)

	grpcListener, e := net.Listen("tcp", cfg.GRPC.ListenAddress)
	if e != nil {
		slog.Error("listen grpc", "error", e)
		os.Exit(1)
	}
	defer grpcListener.Close()
	grpcServer := grpc.NewServer()
	internalRouter := streaming.NewRouter()
	for _, module := range mods {
		if !cfg.Services[module.Name()].Enabled {
			continue
		}

		if registrar, ok := module.(internalModule); ok {
			registrar.RegisterInternal(internalRouter)
		}
	}
	streaming.Register(grpcServer, internalRouter)
	go func() {
		if e := grpcServer.Serve(grpcListener); e != nil {
			slog.Error("grpc server", "error", e)
			cancel()
		}
	}()

	started, e := app.StartEnabled(ctx, r, deps, mods)
	if e != nil {
		slog.Error("start module", "error", e)
		os.Exit(1)
	}
	logger.Info("enabled modules started", "count", len(started))
	registrations, e := registerModules(ctx, discoveryResources.Registry, cfg, started)
	if e != nil {
		slog.Error("register service", "error", e)
		os.Exit(1)
	}
	logger.Info("services registered", "count", len(registrations))

	srv := &http.Server{
		Addr:         cfg.HTTP.Address,
		Handler:      r,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
	}
	go func() {
		logger.Info("http server listening", "address", cfg.HTTP.Address)
		if e := srv.ListenAndServe(); e != nil && e != http.ErrServerClosed {
			slog.Error("http server", "error", e)
			cancel()
		}
	}()
	<-ctx.Done()

	stop, c := context.WithTimeout(context.Background(), cfg.App.ShutdownTimeout)
	defer c()
	for _, module := range started {
		if draining, ok := module.(app.DrainingModule); ok {
			draining.BeginDrain()
		}
	}
	for i := len(registrations) - 1; i >= 0; i-- {
		_ = registrations[i]()
	}
	for _, module := range started {
		if draining, ok := module.(app.DrainingModule); ok {
			if err := draining.Drain(stop); err != nil {
				logger.Warn("drain module", "service", module.Name(), "error", err)
			}
		}
	}
	stopGRPC(stop, grpcServer)
	_ = srv.Shutdown(stop)
	_ = app.StopReverse(stop, started)
}

type internalModule interface {
	RegisterInternal(*streaming.Router)
}

func enabledModules(deps app.Dependencies) []app.Module {
	modules := make([]app.Module, 0, 4)
	if deps.Config.Services["usercenter"].Enabled {
		modules = append(modules, usercenter.NewModule(deps))
	}
	if deps.Config.Services["lobby"].Enabled {
		modules = append(modules, lobby.NewModule(deps))
	}
	if deps.Config.Services["battle"].Enabled {
		modules = append(modules, battle.NewModule(deps))
	}
	if deps.Config.Services["gateway"].Enabled {
		modules = append(modules, gateway.NewModule(deps))
	}
	return modules
}

func registerModules(ctx context.Context, registry discovery.Registry, cfg config.Config, modules []app.Module) ([]discovery.CloseFunc, error) {
	registrations := make([]discovery.CloseFunc, 0, len(modules))
	for _, module := range modules {
		slog.Info("registering service", "service", module.Name(), "instance_id", cfg.App.InstanceID, "address", cfg.GRPC.AdvertiseAddress)
		registerCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		closeRegistration, err := registry.Register(registerCtx, discovery.Registration{Service: module.Name(), Instance: cfg.App.InstanceID, Address: cfg.GRPC.AdvertiseAddress})
		cancel()
		if err != nil {
			for i := len(registrations) - 1; i >= 0; i-- {
				_ = registrations[i]()
			}
			return nil, err
		}
		registrations = append(registrations, closeRegistration)
	}
	return registrations, nil
}

func stopGRPC(ctx context.Context, server *grpc.Server) {
	done := make(chan struct{})
	go func() { server.GracefulStop(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		server.Stop()
	}
}
