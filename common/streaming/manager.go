package streaming

import (
	"context"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"server/common/discovery"
	internalpb "server/proto/gen/internalpb"
)

type DialFunc func(context.Context, string) (*grpc.ClientConn, error)

type ClientManager struct {
	registry    discovery.Registry
	service     internalpb.ServiceType
	serviceName string
	instanceID  string
	onEvent     Handler
	dial        DialFunc
	mu          sync.RWMutex
	known       map[string]discovery.Registration
	clients     map[string]*managedClient
	cancel      context.CancelFunc
}

type managedClient struct {
	registration discovery.Registration
	connection   *grpc.ClientConn
	client       *Client
}

func NewClientManager(registry discovery.Registry, service internalpb.ServiceType, serviceName, instanceID string, onEvent Handler) *ClientManager {
	return &ClientManager{
		registry:    registry,
		service:     service,
		serviceName: serviceName,
		instanceID:  instanceID,
		onEvent:     onEvent,
		dial:        defaultDial,
		known:       make(map[string]discovery.Registration),
		clients:     make(map[string]*managedClient),
	}
}

func (m *ClientManager) Start(ctx context.Context, targets map[string]internalpb.ServiceType) {
	child, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	for name, service := range targets {
		go m.watchService(child, name, service)
	}
}

func (m *ClientManager) Client(service string, instanceID string) (*Client, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	client, ok := m.clients[managerKey(service, instanceID)]
	if !ok {
		return nil, false
	}
	return client.client, true
}

func (m *ClientManager) AnyClient(service string) (*Client, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for key, client := range m.clients {
		if strings.HasPrefix(key, service+"/") {
			return client.client, true
		}
	}
	return nil, false
}

func (m *ClientManager) Close() {
	if m.cancel != nil {
		m.cancel()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, client := range m.clients {
		client.client.Close()
		_ = client.connection.Close()
		delete(m.clients, key)
	}
}

func (m *ClientManager) watchService(ctx context.Context, serviceName string, targetType internalpb.ServiceType) {
	for ctx.Err() == nil {
		registrations, err := m.registry.Discover(ctx, serviceName)
		if err == nil {
			for _, registration := range registrations {
				m.upsert(ctx, serviceName, targetType, registration)
			}
		}
		events, err := m.registry.Watch(ctx, serviceName)
		if err != nil {
			if !wait(ctx, time.Second) {
				return
			}
			continue
		}
		for event := range events {
			if event.Type == discovery.EventPut {
				m.upsert(ctx, serviceName, targetType, event.Registration)
			} else {
				m.remove(serviceName, event.Registration.Instance)
			}
		}
		if !wait(ctx, time.Second) {
			return
		}
	}
}

func (m *ClientManager) upsert(ctx context.Context, serviceName string, targetType internalpb.ServiceType, registration discovery.Registration) {
	key := managerKey(serviceName, registration.Instance)
	m.mu.Lock()
	m.known[key] = registration
	if _, exists := m.clients[key]; exists {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()
	m.connect(ctx, key, targetType, registration)
}

func (m *ClientManager) connect(ctx context.Context, key string, targetType internalpb.ServiceType, registration discovery.Registration) {
	if registration.Instance == m.instanceID && registration.Service == m.serviceName {
		return
	}
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	connection, err := m.dial(dialCtx, registration.Address)
	cancel()
	if err != nil {
		return
	}
	client, err := NewClient(ctx, connection, m.service, m.instanceID, m.onEvent)
	if err != nil {
		_ = connection.Close()
		return
	}
	m.mu.Lock()
	if _, exists := m.clients[key]; exists {
		m.mu.Unlock()
		client.Close()
		_ = connection.Close()
		return
	}
	m.clients[key] = &managedClient{
		registration: registration,
		connection:   connection,
		client:       client,
	}
	m.mu.Unlock()
	go m.monitor(ctx, key, targetType, registration, client, connection)
}

func (m *ClientManager) monitor(ctx context.Context, key string, targetType internalpb.ServiceType, registration discovery.Registration, client *Client, connection *grpc.ClientConn) {
	select {
	case <-client.Done():
	case <-ctx.Done():
		return
	}
	m.mu.Lock()
	current := m.clients[key]
	if current != nil && current.client == client {
		delete(m.clients, key)
	}
	known, exists := m.known[key]
	m.mu.Unlock()
	_ = connection.Close()
	if exists && ctx.Err() == nil && wait(ctx, time.Second) {
		m.connect(ctx, key, targetType, known)
	}
}

func (m *ClientManager) remove(serviceName, instanceID string) {
	key := managerKey(serviceName, instanceID)
	m.mu.Lock()
	delete(m.known, key)
	client := m.clients[key]
	delete(m.clients, key)
	m.mu.Unlock()
	if client != nil {
		client.client.Close()
		_ = client.connection.Close()
	}
}

func defaultDial(ctx context.Context, address string) (*grpc.ClientConn, error) {
	return grpc.DialContext(ctx, address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
}

func managerKey(service, instanceID string) string { return service + "/" + instanceID }
func wait(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
