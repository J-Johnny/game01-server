package etcd

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	clientv3 "go.etcd.io/etcd/client/v3"
	"server/common/discovery"
)

type Registry struct {
	client    *clientv3.Client
	namespace string
	leaseTTL  int64
}

func New(client *clientv3.Client, namespace string, leaseTTL int64) *Registry {
	return &Registry{client: client, namespace: strings.TrimRight(namespace, "/"), leaseTTL: leaseTTL}
}

func (r *Registry) Register(ctx context.Context, item discovery.Registration) (discovery.CloseFunc, error) {
	if item.Service == "" || item.Instance == "" || item.Address == "" {
		return nil, fmt.Errorf("service, instance and address are required")
	}
	data, err := json.Marshal(item)
	if err != nil {
		return nil, err
	}
	lease, err := r.client.Grant(ctx, r.leaseTTL)
	if err != nil {
		return nil, err
	}
	key := r.key(item.Service, item.Instance)
	if _, err = r.client.Put(ctx, key, string(data), clientv3.WithLease(lease.ID)); err != nil {
		_, _ = r.client.Revoke(context.Background(), lease.ID)
		return nil, err
	}
	keepAliveCtx, cancel := context.WithCancel(context.Background())
	keepAlive, err := r.client.KeepAlive(keepAliveCtx, lease.ID)
	if err != nil {
		cancel()
		return nil, err
	}
	go func() {
		for range keepAlive {
		}
	}()
	return func() error { cancel(); _, err := r.client.Revoke(context.Background(), lease.ID); return err }, nil
}

func (r *Registry) Discover(ctx context.Context, service string) ([]discovery.Registration, error) {
	response, err := r.client.Get(ctx, r.prefix(service), clientv3.WithPrefix())
	if err != nil {
		return nil, err
	}
	result := make([]discovery.Registration, 0, len(response.Kvs))
	for _, kv := range response.Kvs {
		var item discovery.Registration
		if err := json.Unmarshal(kv.Value, &item); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

func (r *Registry) Watch(ctx context.Context, service string) (<-chan discovery.Event, error) {
	out := make(chan discovery.Event, 16)
	go func() {
		defer close(out)
		watch := r.client.Watch(ctx, r.prefix(service), clientv3.WithPrefix())
		for response := range watch {
			for _, event := range response.Events {
				var item discovery.Registration
				if event.Type == clientv3.EventTypeDelete {
					parts := strings.Split(string(event.Kv.Key), "/")
					if len(parts) > 0 {
						item.Service = service
						item.Instance = parts[len(parts)-1]
					}
					select {
					case out <- discovery.Event{Type: discovery.EventDelete, Registration: item}:
					case <-ctx.Done():
						return
					}
					continue
				}
				if err := json.Unmarshal(event.Kv.Value, &item); err != nil {
					continue
				}
				select {
				case out <- discovery.Event{Type: discovery.EventPut, Registration: item}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

func (r *Registry) prefix(service string) string        { return path.Join(r.namespace, service) + "/" }
func (r *Registry) key(service, instance string) string { return r.prefix(service) + instance }
