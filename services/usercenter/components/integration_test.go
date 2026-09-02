package components

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
	"server/common/streaming"
	internalpb "server/proto/gen/internalpb"
)

func TestAuthenticationOverGRPCStreaming(t *testing.T) {
	grpcServer, address := startAuthenticationServer(t)
	defer grpcServer.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connection, err := grpc.DialContext(ctx, address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial user center: %v", err)
	}
	defer connection.Close()

	client, err := streaming.NewClient(ctx, connection, internalpb.ServiceType_SERVICE_TYPE_GATEWAY, "gateway-auth-integration", nil)
	if err != nil {
		t.Fatalf("create streaming client: %v", err)
	}
	defer client.Close()

	guestResponse := &internalpb.GuestAuthenticateResponse{}
	response, err := sendAuthenticationRequest(ctx, client, internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_GUEST_AUTHENTICATE_REQUEST, &internalpb.GuestAuthenticateRequest{InstallId: "integration-install"})
	if err != nil {
		t.Fatalf("guest authentication request: %v", err)
	}
	if response.Kind != internalpb.EnvelopeKind_ENVELOPE_KIND_RESPONSE || response.MessageId != uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_GUEST_AUTHENTICATE_RESPONSE) {
		t.Fatalf("unexpected guest envelope: %s", response)
	}
	if err := proto.Unmarshal(response.Payload, guestResponse); err != nil {
		t.Fatalf("unmarshal guest response: %v", err)
	}
	if guestResponse.AccountId == "" || guestResponse.RefreshToken == "" || !guestResponse.Created {
		t.Fatalf("incomplete guest response: %s", guestResponse)
	}

	secondResponse := &internalpb.GuestAuthenticateResponse{}
	response, err = sendAuthenticationRequest(ctx, client, internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_GUEST_AUTHENTICATE_REQUEST, &internalpb.GuestAuthenticateRequest{InstallId: "integration-install"})
	if err != nil {
		t.Fatalf("second guest authentication request: %v", err)
	}
	if err := proto.Unmarshal(response.Payload, secondResponse); err != nil {
		t.Fatalf("unmarshal second guest response: %v", err)
	}
	if secondResponse.Created || secondResponse.AccountId != guestResponse.AccountId {
		t.Fatalf("guest authentication is not idempotent: %s", secondResponse)
	}

	passwordResponse := &internalpb.PasswordAuthenticateResponse{}
	response, err = sendAuthenticationRequest(ctx, client, internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_PASSWORD_AUTHENTICATE_REQUEST, &internalpb.PasswordAuthenticateRequest{Username: "integration-user", Password: "correct-password", InstallId: "integration-install"})
	if err != nil {
		t.Fatalf("password authentication request: %v", err)
	}
	if err := proto.Unmarshal(response.Payload, passwordResponse); err != nil {
		t.Fatalf("unmarshal password response: %v", err)
	}
	if passwordResponse.AccountId == "" || passwordResponse.RefreshToken == "" {
		t.Fatalf("incomplete password response: %s", passwordResponse)
	}

	response, err = sendAuthenticationRequest(ctx, client, internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_PASSWORD_AUTHENTICATE_REQUEST, &internalpb.PasswordAuthenticateRequest{Username: "integration-user", Password: "wrong-password", InstallId: "integration-install"})
	if err != nil {
		t.Fatalf("invalid password request transport error: %v", err)
	}
	if response.Kind != internalpb.EnvelopeKind_ENVELOPE_KIND_ERROR || response.ErrorCode == 0 || response.ErrorMessage == "" {
		t.Fatalf("invalid password did not return streaming error: %s", response)
	}

	refreshResponse := &internalpb.RefreshAuthenticateResponse{}
	response, err = sendAuthenticationRequest(ctx, client, internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_REFRESH_AUTHENTICATE_REQUEST, &internalpb.RefreshAuthenticateRequest{RefreshToken: guestResponse.RefreshToken, InstallId: "integration-install"})
	if err != nil {
		t.Fatalf("refresh authentication request: %v", err)
	}
	if err := proto.Unmarshal(response.Payload, refreshResponse); err != nil {
		t.Fatalf("unmarshal refresh response: %v", err)
	}
	if refreshResponse.AccountId != guestResponse.AccountId || refreshResponse.RefreshToken == guestResponse.RefreshToken {
		t.Fatalf("refresh token was not rotated: %s", refreshResponse)
	}

	response, err = sendAuthenticationRequest(ctx, client, internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_REFRESH_AUTHENTICATE_REQUEST, &internalpb.RefreshAuthenticateRequest{RefreshToken: guestResponse.RefreshToken, InstallId: "integration-install"})
	if err != nil {
		t.Fatalf("old refresh token request transport error: %v", err)
	}
	if response.Kind != internalpb.EnvelopeKind_ENVELOPE_KIND_ERROR {
		t.Fatalf("old refresh token was accepted: %s", response)
	}

	response, err = sendAuthenticationRequest(ctx, client, internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_REVOKE_REFRESH_TOKEN_REQUEST, &internalpb.RevokeRefreshTokenRequest{AccountId: refreshResponse.AccountId, RefreshToken: refreshResponse.RefreshToken})
	if err != nil {
		t.Fatalf("revoke refresh token request: %v", err)
	}
	if response.Kind != internalpb.EnvelopeKind_ENVELOPE_KIND_RESPONSE || response.MessageId != uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_REVOKE_REFRESH_TOKEN_RESPONSE) {
		t.Fatalf("unexpected revoke response: %s", response)
	}
	response, err = sendAuthenticationRequest(ctx, client, internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_REFRESH_AUTHENTICATE_REQUEST, &internalpb.RefreshAuthenticateRequest{RefreshToken: refreshResponse.RefreshToken, InstallId: "integration-install"})
	if err != nil {
		t.Fatalf("revoked refresh token request transport error: %v", err)
	}
	if response.Kind != internalpb.EnvelopeKind_ENVELOPE_KIND_ERROR {
		t.Fatalf("revoked refresh token was accepted: %s", response)
	}
}

func startAuthenticationServer(t *testing.T) (*grpc.Server, string) {
	t.Helper()
	store := newDomainAuthMemory()
	auth := NewDomainAuthComponent(store.accounts, store.identities, store.tokens, domainAuthUnitOfWork{}, time.Hour)
	router := streaming.NewRouter()
	auth.RegisterInternal(router)
	grpcServer := grpc.NewServer()
	streaming.Register(grpcServer, router)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen user center: %v", err)
	}
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(func() { _ = listener.Close() })
	return grpcServer, listener.Addr().String()
}

func sendAuthenticationRequest(ctx context.Context, client *streaming.Client, messageID internalpb.UserCenterMessageId, request proto.Message) (*internalpb.InternalEnvelope, error) {
	payload, err := proto.Marshal(request)
	if err != nil {
		return nil, err
	}
	return client.Request(ctx, &internalpb.InternalEnvelope{TargetService: internalpb.ServiceType_SERVICE_TYPE_USERCENTER, MessageId: uint32(messageID), Payload: payload})
}
