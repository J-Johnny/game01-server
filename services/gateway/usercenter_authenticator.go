package gateway

import (
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"
	"server/common/streaming"
	gatewaypb "server/proto/gen/client"
	internalpb "server/proto/gen/internalpb"
)

type UserCenterAuthenticator struct {
	client func() (*streaming.Client, bool)
}

func NewUserCenterAuthenticator(client func() (*streaming.Client, bool)) *UserCenterAuthenticator {
	return &UserCenterAuthenticator{client: client}
}

func (a *UserCenterAuthenticator) Authenticate(ctx context.Context, provider gatewaypb.AuthProvider, _, installID string) (Authentication, error) {
	if provider != gatewaypb.AuthProvider_AUTH_PROVIDER_GUEST {
		return Authentication{}, errors.New("authentication provider is not enabled")
	}

	payload, err := proto.Marshal(&internalpb.GuestAuthenticateRequest{InstallId: installID})
	if err != nil {
		return Authentication{}, fmt.Errorf("marshal guest authentication request: %w", err)
	}
	response, err := a.request(ctx, uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_GUEST_AUTHENTICATE_REQUEST), payload)
	if err != nil {
		return Authentication{}, err
	}
	message := &internalpb.GuestAuthenticateResponse{}
	if err := proto.Unmarshal(response.Payload, message); err != nil {
		return Authentication{}, fmt.Errorf("unmarshal guest authentication response: %w", err)
	}
	if message.AccountId == "" || message.RefreshToken == "" {
		return Authentication{}, errors.New("user center returned incomplete guest authentication")
	}
	return Authentication{AccountID: message.AccountId, RefreshToken: message.RefreshToken, RefreshTokenExpireAt: time.Unix(message.RefreshTokenExpireAtUnix, 0)}, nil
}

func (a *UserCenterAuthenticator) AuthenticatePassword(ctx context.Context, username, password, installID string) (Authentication, error) {
	payload, err := proto.Marshal(&internalpb.PasswordAuthenticateRequest{Username: username, Password: password, InstallId: installID})
	if err != nil {
		return Authentication{}, fmt.Errorf("marshal password authentication request: %w", err)
	}
	response, err := a.request(ctx, uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_PASSWORD_AUTHENTICATE_REQUEST), payload)
	if err != nil {
		return Authentication{}, err
	}
	message := &internalpb.PasswordAuthenticateResponse{}
	if err := proto.Unmarshal(response.Payload, message); err != nil {
		return Authentication{}, fmt.Errorf("unmarshal password authentication response: %w", err)
	}
	if message.AccountId == "" || message.RefreshToken == "" {
		return Authentication{}, errors.New("user center returned incomplete password authentication")
	}
	return Authentication{AccountID: message.AccountId, RefreshToken: message.RefreshToken, RefreshTokenExpireAt: time.Unix(message.RefreshTokenExpireAtUnix, 0)}, nil
}

func (a *UserCenterAuthenticator) Refresh(ctx context.Context, refreshToken, installID string) (Authentication, error) {
	payload, err := proto.Marshal(&internalpb.RefreshAuthenticateRequest{RefreshToken: refreshToken, InstallId: installID})
	if err != nil {
		return Authentication{}, fmt.Errorf("marshal refresh authentication request: %w", err)
	}
	response, err := a.request(ctx, uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_REFRESH_AUTHENTICATE_REQUEST), payload)
	if err != nil {
		return Authentication{}, err
	}
	message := &internalpb.RefreshAuthenticateResponse{}
	if err := proto.Unmarshal(response.Payload, message); err != nil {
		return Authentication{}, fmt.Errorf("unmarshal refresh authentication response: %w", err)
	}
	if message.AccountId == "" || message.RefreshToken == "" {
		return Authentication{}, errors.New("user center returned incomplete refresh authentication")
	}
	return Authentication{AccountID: message.AccountId, RefreshToken: message.RefreshToken, RefreshTokenExpireAt: time.Unix(message.RefreshTokenExpireAtUnix, 0)}, nil
}

func (a *UserCenterAuthenticator) request(ctx context.Context, messageID uint32, payload []byte) (*internalpb.InternalEnvelope, error) {
	if a == nil || a.client == nil {
		return nil, errors.New("user center client is not configured")
	}
	client, ok := a.client()
	if !ok || client == nil {
		return nil, errors.New("user center service is unavailable")
	}
	response, err := client.Request(ctx, &internalpb.InternalEnvelope{TargetService: internalpb.ServiceType_SERVICE_TYPE_USERCENTER, MessageId: messageID, Payload: payload})
	if err != nil {
		return nil, fmt.Errorf("user center request: %w", err)
	}
	if response.Kind == internalpb.EnvelopeKind_ENVELOPE_KIND_ERROR {
		return nil, fmt.Errorf("user center error %d: %s", response.ErrorCode, response.ErrorMessage)
	}
	if response.Kind != internalpb.EnvelopeKind_ENVELOPE_KIND_RESPONSE {
		return nil, errors.New("user center returned invalid response envelope")
	}
	return response, nil
}
