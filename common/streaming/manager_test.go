package streaming

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"server/common/discovery"
	"server/common/discovery/static"
	internalpb "server/proto/gen/internalpb"
)

func TestClientManagerConnectsFromRegistryWatch(t *testing.T) {
	listener := bufconn.Listen(bufferSize)
	server := grpc.NewServer()
	Register(server, HandlerFunc(func(_ context.Context, peer Peer, request *internalpb.InternalEnvelope) error {
		return peer.Connection.Send(&internalpb.InternalEnvelope{Kind: internalpb.EnvelopeKind_ENVELOPE_KIND_RESPONSE, RequestId: request.RequestId})
	}))
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()

	registry := static.New(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager := NewClientManager(registry, internalpb.ServiceType_SERVICE_TYPE_GATEWAY, "gateway", "gateway-1", nil)
	manager.dial = func(ctx context.Context, _ string) (*grpc.ClientConn, error) {
		return grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(dialer(listener)), grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	manager.Start(ctx, map[string]internalpb.ServiceType{"lobby": internalpb.ServiceType_SERVICE_TYPE_LOBBY})
	defer manager.Close()
	closeRegistration, err := registry.Register(ctx, discovery.Registration{Service: "lobby", Instance: "lobby-1", Address: "bufnet"})
	if err != nil {
		t.Fatalf("register lobby: %v", err)
	}
	defer closeRegistration()

	client := waitForClient(t, manager, "lobby", "lobby-1")
	requestCtx, requestCancel := context.WithTimeout(ctx, time.Second)
	defer requestCancel()
	if _, err := client.Request(requestCtx, &internalpb.InternalEnvelope{TargetService: internalpb.ServiceType_SERVICE_TYPE_LOBBY}); err != nil {
		t.Fatalf("request through discovered client: %v", err)
	}
	client.Close()
	if replacement := waitForReplacement(t, manager, "lobby", "lobby-1", client); replacement == client {
		t.Fatal("closed stream was not replaced")
	}
}

func waitForClient(t *testing.T, manager *ClientManager, service, instanceID string) *Client {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if client, ok := manager.Client(service, instanceID); ok {
			return client
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("client for %s/%s was not created", service, instanceID)
	return nil
}

func waitForReplacement(t *testing.T, manager *ClientManager, service, instanceID string, previous *Client) *Client {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if client, ok := manager.Client(service, instanceID); ok && client != previous {
			return client
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("client for %s/%s was not reconnected", service, instanceID)
	return nil
}

func _managerDialer(listener *bufconn.Listener) func(context.Context, string) (net.Conn, error) {
	return func(context.Context, string) (net.Conn, error) { return listener.Dial() }
}
