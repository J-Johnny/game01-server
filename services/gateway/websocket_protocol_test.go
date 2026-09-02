package gateway

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"
	gatewaypb "server/proto/gen/client"
	"server/services/gateway/session"
)

func TestWebSocketBinaryProtocolValidation(t *testing.T) {
	tests := []struct {
		name          string
		payload       []byte
		requestID     uint64
		wantCode      uint32
		wantRequestID uint64
		wantMessage   string
	}{
		{
			name:          "malformed protobuf",
			payload:       []byte{0xff},
			wantCode:      ErrorInvalidEnvelope,
			wantRequestID: 0,
			wantMessage:   "invalid envelope",
		},
		{
			name:          "missing message id",
			payload:       marshalProtocolEnvelope(t, &gatewaypb.Envelope{RequestId: 11}),
			wantCode:      ErrorInvalidEnvelope,
			wantRequestID: 0,
			wantMessage:   "message id is required",
		},
		{
			name:          "unsupported message id",
			payload:       marshalProtocolEnvelope(t, &gatewaypb.Envelope{MessageId: gatewaypb.ClientMessageId(999), RequestId: 12}),
			wantCode:      ErrorUnsupported,
			wantRequestID: 12,
			wantMessage:   "unsupported message",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newWebSocketProtocolTestServer(t)
			defer server.Close()
			ws := dialWebSocketTestServer(t, server)
			defer ws.Close()

			if err := ws.WriteMessage(websocket.BinaryMessage, test.payload); err != nil {
				t.Fatalf("write binary frame: %v", err)
			}
			_, responseBytes, err := ws.ReadMessage()
			if err != nil {
				t.Fatalf("read binary response: %v", err)
			}
			responseEnvelope := &gatewaypb.Envelope{}
			if err := proto.Unmarshal(responseBytes, responseEnvelope); err != nil {
				t.Fatalf("unmarshal response envelope: %v", err)
			}
			if responseEnvelope.MessageId != MessageErrorResponse || responseEnvelope.RequestId != test.wantRequestID {
				t.Fatalf("unexpected response envelope: %s", responseEnvelope)
			}
			errorResponse := &gatewaypb.ErrorResponse{}
			if err := proto.Unmarshal(responseEnvelope.Payload, errorResponse); err != nil {
				t.Fatalf("unmarshal error response: %v", err)
			}
			if errorResponse.Code != test.wantCode || errorResponse.Message != test.wantMessage {
				t.Fatalf("unexpected protocol error: %s", errorResponse)
			}
		})
	}
}

func TestWebSocketRejectsTextFrames(t *testing.T) {
	server := newWebSocketProtocolTestServer(t)
	defer server.Close()
	ws := dialWebSocketTestServer(t, server)
	defer ws.Close()

	if err := ws.WriteMessage(websocket.TextMessage, []byte("not a binary protobuf frame")); err != nil {
		t.Fatalf("write text frame: %v", err)
	}
	if err := ws.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	if _, _, err := ws.ReadMessage(); err == nil {
		t.Fatal("text frame unexpectedly received a response")
	}
}

func TestWebSocketDisconnectMarksBoundSessionReconnecting(t *testing.T) {
	store := session.NewMemoryStore()
	manager := session.NewManager(store, "gateway-test", time.Hour, time.Minute)
	var sessionID string

	dispatch := DispatcherFunc(func(_ context.Context, connection *Connection, _ []byte) error {
		created, createErr := manager.Create(context.Background(), "account-test", connection.ID, time.Now().UTC())
		if createErr != nil {
			return createErr
		}
		sessionID = created.Record.SessionID
		connection.BindSession(sessionID)
		return nil
	})
	router := gin.New()
	NewHandler(dispatch, manager).RegisterRoutes(router)
	server := httptest.NewServer(router)
	defer server.Close()
	ws := dialWebSocketTestServer(t, server)

	if err := ws.WriteMessage(websocket.BinaryMessage, []byte{1}); err != nil {
		t.Fatalf("write message: %v", err)
	}
	if err := ws.Close(); err != nil {
		t.Fatalf("close websocket: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		record, getErr := store.Get(context.Background(), sessionID)
		if getErr == nil && record.State == session.StateReconnecting && record.ConnectionID == "" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	record, err := store.Get(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("get disconnected session: %v", err)
	}
	t.Fatalf("session was not marked reconnecting: %+v", record)
}

func newWebSocketProtocolTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	router := gin.New()
	NewHandler(NewDispatcher(RejectingAuthenticator{}, nil)).RegisterRoutes(router)
	return httptest.NewServer(router)
}

func dialWebSocketTestServer(t *testing.T, server *httptest.Server) *websocket.Conn {
	t.Helper()
	url := "ws" + server.URL[len("http"):]
	ws, _, err := websocket.DefaultDialer.Dial(url+"/ws", nil)
	if err != nil {
		t.Fatalf("dial websocket test server: %v", err)
	}
	return ws
}

func marshalProtocolEnvelope(t *testing.T, envelope *gatewaypb.Envelope) []byte {
	t.Helper()
	payload, err := proto.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal protocol envelope: %v", err)
	}
	return payload
}
