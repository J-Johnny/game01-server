package static

import (
	"context"
	"sync"

	"server/common/discovery"
)

type Registry struct {
	mu      sync.RWMutex
	items   map[string]discovery.Registration
	watches map[string][]chan discovery.Event
}

func New(items []discovery.Registration) *Registry {
	r := &Registry{
		items:   make(map[string]discovery.Registration),
		watches: make(map[string][]chan discovery.Event),
	}
	for _, item := range items {
		r.items[key(item.Service, item.Instance)] = item
	}
	return r
}

func (r *Registry) Register(_ context.Context, item discovery.Registration) (discovery.CloseFunc, error) {
	r.mu.Lock()
	r.items[key(item.Service, item.Instance)] = item
	r.publishLocked(item.Service, discovery.Event{Type: discovery.EventPut, Registration: item})
	r.mu.Unlock()
	return func() error {
		r.mu.Lock()
		defer r.mu.Unlock()
		delete(r.items, key(item.Service, item.Instance))
		r.publishLocked(item.Service, discovery.Event{Type: discovery.EventDelete, Registration: item})
		return nil
	}, nil
}

func (r *Registry) Discover(_ context.Context, service string) ([]discovery.Registration, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]discovery.Registration, 0)
	for _, item := range r.items {
		if item.Service == service {
			result = append(result, item)
		}
	}
	return result, nil
}

func (r *Registry) Watch(ctx context.Context, service string) (<-chan discovery.Event, error) {
	ch := make(chan discovery.Event, 16)
	r.mu.Lock()
	r.watches[service] = append(r.watches[service], ch)
	r.mu.Unlock()
	go func() {
		<-ctx.Done()
		r.mu.Lock()
		defer r.mu.Unlock()
		for i, candidate := range r.watches[service] {
			if candidate == ch {
				r.watches[service] = append(r.watches[service][:i], r.watches[service][i+1:]...)
				break
			}
		}
		close(ch)
	}()
	return ch, nil
}

func (r *Registry) publishLocked(service string, event discovery.Event) {
	for _, ch := range r.watches[service] {
		select {
		case ch <- event:
		default:
		}
	}
}
func key(service, instance string) string { return service + "/" + instance }
