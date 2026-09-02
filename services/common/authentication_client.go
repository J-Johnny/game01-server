package common

import (
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"
	"server/common/reliability"
	"server/common/streaming"
	internalpb "server/proto/gen/internalpb"
)

var ErrAuthenticationServiceUnavailable = errors.New("authentication service is unavailable")

type AuthenticationService interface {
	AuthenticateLogin(context.Context, []byte) (AuthenticationGrant, error)
	RefreshLogin(context.Context, []byte) (AuthenticationGrant, error)
}

type AuthenticationGrant struct {
	AccountID            string
	RefreshToken         string
	RefreshTokenExpireAt time.Time
}

type StreamingAuthenticationClient struct {
	client  func() (*streaming.Client, bool)
	retry   reliability.RetryPolicy
	breaker *reliability.CircuitBreaker
}

func NewStreamingAuthenticationClient(client func() (*streaming.Client, bool)) *StreamingAuthenticationClient {
	return &StreamingAuthenticationClient{
		client: client,
		retry:  reliability.RetryPolicy{MaxAttempts: 1},
	}
}

func (a *StreamingAuthenticationClient) SetReliability(retry reliability.RetryPolicy, breaker *reliability.CircuitBreaker) {
	a.retry = retry
	a.breaker = breaker
}

func (a *StreamingAuthenticationClient) AuthenticateLogin(ctx context.Context, loginRequest []byte) (AuthenticationGrant, error) {
	payload, err := proto.Marshal(&internalpb.ClientLoginAuthenticateRequest{LoginRequest: loginRequest})
	if err != nil {
		return AuthenticationGrant{}, fmt.Errorf("marshal client login authentication request: %w", err)
	}
	response, err := a.request(ctx, uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_CLIENT_LOGIN_AUTHENTICATE_REQUEST), payload)
	if err != nil {
		return AuthenticationGrant{}, err
	}
	message := &internalpb.ClientLoginAuthenticateResponse{}
	if err := proto.Unmarshal(response.Payload, message); err != nil {
		return AuthenticationGrant{}, fmt.Errorf("unmarshal client login authentication response: %w", err)
	}
	return authenticationGrantFromProto(message.Grant)
}

func (a *StreamingAuthenticationClient) RefreshLogin(ctx context.Context, refreshLoginRequest []byte) (AuthenticationGrant, error) {
	payload, err := proto.Marshal(&internalpb.ClientRefreshAuthenticateRequest{RefreshLoginRequest: refreshLoginRequest})
	if err != nil {
		return AuthenticationGrant{}, fmt.Errorf("marshal client refresh authentication request: %w", err)
	}
	response, err := a.request(ctx, uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_CLIENT_REFRESH_AUTHENTICATE_REQUEST), payload)
	if err != nil {
		return AuthenticationGrant{}, err
	}
	message := &internalpb.ClientRefreshAuthenticateResponse{}
	if err := proto.Unmarshal(response.Payload, message); err != nil {
		return AuthenticationGrant{}, fmt.Errorf("unmarshal client refresh authentication response: %w", err)
	}
	return authenticationGrantFromProto(message.Grant)
}

func authenticationGrantFromProto(grant *internalpb.AuthenticationGrant) (AuthenticationGrant, error) {
	if grant == nil || grant.AccountId == "" || grant.RefreshToken == "" {
		return AuthenticationGrant{}, errors.New("authentication service returned incomplete grant")
	}
	return AuthenticationGrant{
		AccountID:            grant.AccountId,
		RefreshToken:         grant.RefreshToken,
		RefreshTokenExpireAt: time.Unix(grant.RefreshTokenExpireAtUnix, 0),
	}, nil
}

func (a *StreamingAuthenticationClient) request(ctx context.Context, messageID uint32, payload []byte) (*internalpb.InternalEnvelope, error) {
	if a == nil || a.client == nil {
		return nil, errors.New("user center client is not configured")
	}
	var response *internalpb.InternalEnvelope
	request := func(requestContext context.Context) error {
		client, ok := a.client()
		if !ok || client == nil {
			return ErrAuthenticationServiceUnavailable
		}
		var requestErr error
		response, requestErr = client.Request(requestContext, &internalpb.InternalEnvelope{TargetService: internalpb.ServiceType_SERVICE_TYPE_USERCENTER, MessageId: messageID, Payload: payload})
		if requestErr != nil {
			return fmt.Errorf("user center request: %w", requestErr)
		}
		return nil
	}
	operation := func() error { return a.retry.Do(ctx, request) }
	var err error
	if a.breaker != nil {
		err = a.breaker.ExecuteClassified(operation, func(candidate error) bool {
			return errors.Is(candidate, ErrAuthenticationServiceUnavailable) || errors.Is(candidate, streaming.ErrConnectionClosed) || errors.Is(candidate, streaming.ErrRequestTimeout)
		})
	} else {
		err = operation()
	}
	if err != nil {
		return nil, err
	}
	if response.Kind == internalpb.EnvelopeKind_ENVELOPE_KIND_ERROR {
		return nil, fmt.Errorf("user center error %d: %s", response.ErrorCode, response.ErrorMessage)
	}
	if response.Kind != internalpb.EnvelopeKind_ENVELOPE_KIND_RESPONSE {
		return nil, errors.New("user center returned invalid response envelope")
	}
	return response, nil
}
