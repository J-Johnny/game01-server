package integration

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"
	gatewaypb "server/proto/gen/client"
	"server/services/gateway"
)

func TestGatewayDrainNotifiesClientsAndStopsAccepting(t *testing.T) {
	if os.Getenv("GAME_E2E_GATEWAY_DRAIN") != "1" {
		t.Skip("set GAME_E2E_GATEWAY_DRAIN=1 to run against the local Compose environment")
	}
	if err := waitForHTTPStatus(faultGatewayHealthURL, http.StatusNoContent, 30*time.Second); err != nil {
		t.Fatalf("Gateway is not ready: %v", err)
	}
	connection := openAuthenticatedGatewayConnection(t)
	defer connection.Close()

	serverRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve server root: %v", err)
	}
	command := exec.Command("docker", "compose", "-f", filepath.Join(serverRoot, "docker-compose.yml"), "stop", "-t", "15", "gateway")
	command.Dir = serverRoot
	if err := command.Start(); err != nil {
		t.Fatalf("start Gateway stop: %v", err)
	}
	t.Cleanup(func() {
		if err := compose(serverRoot, 90*time.Second, "up", "-d", "gateway"); err != nil {
			t.Errorf("restore Gateway: %v", err)
		}
	})

	if err := waitForHTTPStatus("http://127.0.0.1:18081/readyz", http.StatusServiceUnavailable, 5*time.Second); err != nil {
		t.Fatalf("Gateway did not reject new connections while draining: %v", err)
	}
	connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		_, payload, err := connection.ReadMessage()
		if err != nil {
			t.Fatalf("read Gateway drain event: %v", err)
		}
		envelope := &gatewaypb.Envelope{}
		if err := proto.Unmarshal(payload, envelope); err != nil {
			t.Fatalf("unmarshal Gateway drain envelope: %v", err)
		}
		if envelope.MessageId != gatewaypb.ClientMessageId_CLIENT_MESSAGE_ID_GATEWAY_DRAINING_EVENT {
			continue
		}
		event := &gatewaypb.GatewayDrainingEvent{}
		if err := proto.Unmarshal(envelope.Payload, event); err != nil {
			t.Fatalf("unmarshal Gateway drain event: %v", err)
		}
		if event.ReconnectAfterMillis == 0 {
			t.Fatal("Gateway drain event did not include reconnect delay")
		}
		break
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("stop Gateway: %v", err)
	}
}

func openAuthenticatedGatewayConnection(t *testing.T) *websocket.Conn {
	t.Helper()
	connection, _, err := websocket.DefaultDialer.Dial(faultGatewayURL, nil)
	if err != nil {
		t.Fatalf("dial Gateway: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	payload, err := proto.Marshal(&gatewaypb.LoginRequest{
		Provider:       gatewaypb.AuthProvider_AUTH_PROVIDER_GUEST,
		InstallId:      "drain-test-" + uuid.NewString(),
		IdempotencyKey: uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("marshal login request: %v", err)
	}
	request, err := proto.Marshal(&gatewaypb.Envelope{
		MessageId: gateway.MessageLoginRequest,
		RequestId: 1,
		Payload:   payload,
	})
	if err != nil {
		t.Fatalf("marshal login envelope: %v", err)
	}
	if err := connection.WriteMessage(websocket.BinaryMessage, request); err != nil {
		t.Fatalf("write login request: %v", err)
	}
	connection.SetReadDeadline(time.Now().Add(10 * time.Second))
	for {
		_, responseBytes, err := connection.ReadMessage()
		if err != nil {
			t.Fatalf("read login response: %v", err)
		}
		response := &gatewaypb.Envelope{}
		if err := proto.Unmarshal(responseBytes, response); err != nil {
			t.Fatalf("unmarshal login envelope: %v", err)
		}
		if response.RequestId != 1 {
			continue
		}
		if response.MessageId != gateway.MessageLoginResponse {
			t.Fatalf("unexpected login response: %s", response.MessageId)
		}
		login := &gatewaypb.LoginResponse{}
		if err := proto.Unmarshal(response.Payload, login); err != nil || login.SessionId == "" {
			t.Fatalf("invalid login response: %v", err)
		}
		return connection
	}
}
