package bootstrap

import (
	"context"
	"fmt"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"server/common/config"
	"server/common/discovery"
	etcdregistry "server/common/discovery/etcd"
	"server/common/discovery/static"
)

type Resources struct {
	Registry discovery.Registry
	Close    discovery.CloseFunc
}

func Open(ctx context.Context, cfg config.DiscoveryConfig) (Resources, error) {
	if cfg.Provider == "static" {
		return Resources{
			Registry: static.New(nil),
			Close:    func() error { return nil },
		}, nil
	}
	client, err := clientv3.New(clientv3.Config{Endpoints: cfg.Endpoints, DialTimeout: 5 * time.Second})
	if err != nil {
		return Resources{}, fmt.Errorf("create etcd client: %w", err)
	}
	syncCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Sync(syncCtx); err != nil {
		_ = client.Close()
		return Resources{}, fmt.Errorf("sync etcd endpoints: %w", err)
	}
	return Resources{
		Registry: etcdregistry.New(client, cfg.Namespace, cfg.LeaseTTL),
		Close:    client.Close,
	}, nil
}
