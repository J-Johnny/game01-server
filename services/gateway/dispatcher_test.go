package gateway

import (
	"bytes"
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	gatewaypb "server/proto/gen/client"
	"server/services/gateway/session"
)

func TestDispatcherForwardsLoginPayloadWithoutAuthenticationParsing(t *testing.T) {
	authentication := &capturingAuthenticationService{
		grant: AuthenticationGrant{
			AccountID:            "account-1",
			RefreshToken:         "refresh-token",
			RefreshTokenExpireAt: time.Now().Add(time.Hour),
		},
	}
	dispatcher := NewDispatcher(authentication, session.NewManager(session.NewMemoryStore(), "gateway-test", time.Hour, time.Minute), testPlayerResolver{})
	connection := &Connection{
		ID:   "connection-1",
		send: make(chan []byte, 1),
		done: make(chan struct{}),
	}
	payload := []byte{0xff, 0x01}
	if err := dispatcher.login(context.Background(), connection, &gatewaypb.Envelope{RequestId: 7, Payload: payload}); err != nil {
		t.Fatalf("dispatch login: %v", err)
	}
	if !bytes.Equal(authentication.loginPayload, payload) {
		t.Fatalf("Gateway changed login payload: got=%x want=%x", authentication.loginPayload, payload)
	}
	responseBytes := <-connection.send
	envelope := &gatewaypb.Envelope{}
	if err := proto.Unmarshal(responseBytes, envelope); err != nil {
		t.Fatalf("unmarshal response envelope: %v", err)
	}
	if envelope.MessageId != MessageLoginResponse || envelope.RequestId != 7 {
		t.Fatalf("unexpected login response envelope: %s", envelope)
	}
}

type capturingAuthenticationService struct {
	grant        AuthenticationGrant
	loginPayload []byte
}

func (s *capturingAuthenticationService) AuthenticateLogin(_ context.Context, payload []byte) (AuthenticationGrant, error) {
	s.loginPayload = append([]byte(nil), payload...)
	return s.grant, nil
}

func (s *capturingAuthenticationService) RefreshLogin(context.Context, []byte) (AuthenticationGrant, error) {
	return s.grant, nil
}

type testPlayerResolver struct{}

func (testPlayerResolver) EnsurePlayer(context.Context, string) (string, error) {
	return "player-1", nil
}
