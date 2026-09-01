package streaming

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	internalpb "server/proto/gen/internalpb"
)

const bufferSize = 1024 * 1024

func TestClientRequestCompletesThroughBidirectionalStream(t *testing.T) {
	listener := bufconn.Listen(bufferSize)
	server := grpc.NewServer()
	Register(server, HandlerFunc(func(_ context.Context, peer Peer, request *internalpb.InternalEnvelope) error {
		return peer.Connection.Send(&internalpb.InternalEnvelope{
			Kind:      internalpb.EnvelopeKind_ENVELOPE_KIND_RESPONSE,
			RequestId: request.RequestId,
			Payload:   request.Payload,
		})
	}))
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	connection, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(dialer(listener)), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer connection.Close()
	client, err := NewClient(ctx, connection, internalpb.ServiceType_SERVICE_TYPE_GATEWAY, "gateway-test", nil)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()

	response, err := client.Request(ctx, &internalpb.InternalEnvelope{TargetService: internalpb.ServiceType_SERVICE_TYPE_LOBBY, MessageId: 10, Payload: []byte("payload")})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if response.RequestId == 0 {
		t.Fatal("response request id is missing")
	}
	if string(response.Payload) != "payload" {
		t.Fatalf("response payload = %q", response.Payload)
	}
}

func dialer(listener *bufconn.Listener) func(context.Context, string) (net.Conn, error) {
	return func(context.Context, string) (net.Conn, error) { return listener.Dial() }
}
