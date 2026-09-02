package gateway

import (
	"context"
	"errors"
	"log/slog"
	"time"

	gatewaypb "server/proto/gen/client"
	"server/services/gateway/session"

	"google.golang.org/protobuf/proto"
)

const (
	MessageLoginRequest         gatewaypb.ClientMessageId = gatewaypb.ClientMessageId_CLIENT_MESSAGE_ID_LOGIN_REQUEST
	MessageResumeRequest        gatewaypb.ClientMessageId = gatewaypb.ClientMessageId_CLIENT_MESSAGE_ID_RESUME_REQUEST
	MessageRefreshLoginRequest  gatewaypb.ClientMessageId = gatewaypb.ClientMessageId_CLIENT_MESSAGE_ID_REFRESH_LOGIN_REQUEST
	MessageLoginResponse        gatewaypb.ClientMessageId = gatewaypb.ClientMessageId_CLIENT_MESSAGE_ID_LOGIN_RESPONSE
	MessageResumeResponse       gatewaypb.ClientMessageId = gatewaypb.ClientMessageId_CLIENT_MESSAGE_ID_RESUME_RESPONSE
	MessageRefreshLoginResponse gatewaypb.ClientMessageId = gatewaypb.ClientMessageId_CLIENT_MESSAGE_ID_REFRESH_LOGIN_RESPONSE
	MessageErrorResponse        gatewaypb.ClientMessageId = gatewaypb.ClientMessageId_CLIENT_MESSAGE_ID_ERROR_RESPONSE
)

const (
	ErrorInvalidEnvelope uint32 = 1
	ErrorAuthentication  uint32 = 2
	ErrorResume          uint32 = 3
	ErrorUnsupported     uint32 = 4
	ErrorRefresh         uint32 = 5
	ErrorPlayer          uint32 = 6
)

type Authenticator interface {
	Authenticate(context.Context, gatewaypb.AuthProvider, string, string, string) (Authentication, error)
	Refresh(context.Context, string, string, string) (Authentication, error)
}

type PasswordAuthenticator interface {
	AuthenticatePassword(context.Context, string, string, string, string) (Authentication, error)
}

type Authentication struct {
	AccountID            string
	RefreshToken         string
	RefreshTokenExpireAt time.Time
}

type RejectingAuthenticator struct{}

func (RejectingAuthenticator) Authenticate(context.Context, gatewaypb.AuthProvider, string, string, string) (Authentication, error) {
	return Authentication{}, errors.New("user center authenticator is not configured")
}

func (RejectingAuthenticator) Refresh(context.Context, string, string, string) (Authentication, error) {
	return Authentication{}, errors.New("user center authenticator is not configured")
}

type Dispatcher struct {
	authenticator Authenticator
	sessions      *session.Manager
	now           func() time.Time
	restore       func(context.Context, *Connection, string, string, map[string]uint64)
	players       PlayerResolver
}

func (d *Dispatcher) SetRestoreHandler(handler func(context.Context, *Connection, string, string, map[string]uint64)) {
	d.restore = handler
}

func NewDispatcher(authenticator Authenticator, sessions *session.Manager, players ...PlayerResolver) *Dispatcher {
	dispatcher := &Dispatcher{
		authenticator: authenticator,
		sessions:      sessions,
		now:           time.Now,
	}
	if len(players) > 0 {
		dispatcher.players = players[0]
	}
	return dispatcher
}

func (d *Dispatcher) Dispatch(ctx context.Context, connection *Connection, data []byte) error {
	envelope := &gatewaypb.Envelope{}
	err := proto.Unmarshal(data, envelope)
	if err != nil {
		return d.sendError(connection, 0, ErrorInvalidEnvelope, "invalid envelope")
	}
	if envelope.MessageId == 0 {
		return d.sendError(connection, 0, ErrorInvalidEnvelope, "message id is required")
	}
	switch envelope.MessageId {
	case MessageLoginRequest:
		return d.login(ctx, connection, envelope)
	case MessageResumeRequest:
		return d.resume(ctx, connection, envelope)
	case MessageRefreshLoginRequest:
		return d.refreshLogin(ctx, connection, envelope)
	default:
		return d.sendError(connection, envelope.RequestId, ErrorUnsupported, "unsupported message")
	}
}

func (d *Dispatcher) login(ctx context.Context, connection *Connection, envelope *gatewaypb.Envelope) error {
	request := &gatewaypb.LoginRequest{}
	err := proto.Unmarshal(envelope.Payload, request)
	if err != nil {
		return d.sendError(connection, envelope.RequestId, ErrorInvalidEnvelope, "invalid login request")
	}
	if request.Provider == gatewaypb.AuthProvider_AUTH_PROVIDER_UNSPECIFIED || request.InstallId == "" {
		return d.sendError(connection, envelope.RequestId, ErrorInvalidEnvelope, "provider and install id are required")
	}
	if request.Provider != gatewaypb.AuthProvider_AUTH_PROVIDER_GUEST && request.Provider != gatewaypb.AuthProvider_AUTH_PROVIDER_PASSWORD {
		return d.sendError(connection, envelope.RequestId, ErrorUnsupported, "authentication provider is not enabled")
	}
	var authentication Authentication
	if request.Provider == gatewaypb.AuthProvider_AUTH_PROVIDER_PASSWORD {
		if request.Username == "" || request.Password == "" {
			return d.sendError(connection, envelope.RequestId, ErrorInvalidEnvelope, "username and password are required")
		}
		passwordAuthenticator, ok := d.authenticator.(PasswordAuthenticator)
		if !ok {
			return d.sendError(connection, envelope.RequestId, ErrorUnsupported, "password authentication is not enabled")
		}
		authentication, err = passwordAuthenticator.AuthenticatePassword(ctx, request.Username, request.Password, request.InstallId, request.IdempotencyKey)
	} else {
		authentication, err = d.authenticator.Authenticate(ctx, request.Provider, request.Credential, request.InstallId, request.IdempotencyKey)
	}
	if err != nil {
		slog.Warn("gateway authentication failed", "protocol", "websocket", "connection_id", connection.ID, "request_id", envelope.RequestId, "provider", request.Provider.String(), "error", err)
		return d.sendError(connection, envelope.RequestId, ErrorAuthentication, "authentication failed")
	}
	playerID, err := d.ensurePlayer(ctx, authentication.AccountID)
	if err != nil {
		return d.sendError(connection, envelope.RequestId, ErrorPlayer, "player initialization failed")
	}
	created, err := d.sessions.Create(ctx, authentication.AccountID, connection.ID, d.now(), playerID)
	if err != nil {
		return err
	}
	connection.BindSession(created.Record.SessionID, created.Record.ConnectionEpoch)
	if d.restore != nil {
		go d.restore(ctx, connection, created.Record.PlayerID, created.Record.SessionID, nil)
	}
	return d.send(connection, MessageLoginResponse, envelope.RequestId, created.Record.SessionID, &gatewaypb.LoginResponse{AccountId: created.Record.AccountID, PlayerId: created.Record.PlayerID, SessionId: created.Record.SessionID, ResumeToken: created.ResumeToken, ConnectionEpoch: created.Record.ConnectionEpoch, RefreshToken: authentication.RefreshToken, RefreshTokenExpireAtUnix: authentication.RefreshTokenExpireAt.Unix()})
}

func (d *Dispatcher) refreshLogin(ctx context.Context, connection *Connection, envelope *gatewaypb.Envelope) error {
	request := &gatewaypb.RefreshLoginRequest{}
	if err := proto.Unmarshal(envelope.Payload, request); err != nil {
		return d.sendError(connection, envelope.RequestId, ErrorInvalidEnvelope, "invalid refresh login request")
	}
	if request.RefreshToken == "" || request.InstallId == "" {
		return d.sendError(connection, envelope.RequestId, ErrorInvalidEnvelope, "refresh token and install id are required")
	}
	authentication, err := d.authenticator.Refresh(ctx, request.RefreshToken, request.InstallId, request.IdempotencyKey)
	if err != nil {
		return d.sendError(connection, envelope.RequestId, ErrorRefresh, "refresh authentication failed")
	}
	playerID, err := d.ensurePlayer(ctx, authentication.AccountID)
	if err != nil {
		return d.sendError(connection, envelope.RequestId, ErrorPlayer, "player initialization failed")
	}
	created, err := d.sessions.Create(ctx, authentication.AccountID, connection.ID, d.now(), playerID)
	if err != nil {
		return err
	}
	connection.BindSession(created.Record.SessionID, created.Record.ConnectionEpoch)
	if d.restore != nil {
		go d.restore(ctx, connection, created.Record.PlayerID, created.Record.SessionID, nil)
	}
	return d.send(connection, MessageRefreshLoginResponse, envelope.RequestId, created.Record.SessionID, &gatewaypb.RefreshLoginResponse{AccountId: created.Record.AccountID, PlayerId: created.Record.PlayerID, SessionId: created.Record.SessionID, ResumeToken: created.ResumeToken, ConnectionEpoch: created.Record.ConnectionEpoch, RefreshToken: authentication.RefreshToken, RefreshTokenExpireAtUnix: authentication.RefreshTokenExpireAt.Unix()})
}

func (d *Dispatcher) ensurePlayer(ctx context.Context, accountID string) (string, error) {
	if d.players == nil {
		return "", errors.New("lobby player resolver is not configured")
	}
	return d.players.EnsurePlayer(ctx, accountID)
}

func (d *Dispatcher) resume(ctx context.Context, connection *Connection, envelope *gatewaypb.Envelope) error {
	request := &gatewaypb.ResumeRequest{}
	err := proto.Unmarshal(envelope.Payload, request)
	if err != nil {
		return d.sendError(connection, envelope.RequestId, ErrorInvalidEnvelope, "invalid resume request")
	}
	if request.SessionId == "" || request.ResumeToken == "" {
		return d.sendError(connection, envelope.RequestId, ErrorInvalidEnvelope, "session credentials are required")
	}
	created, err := d.sessions.Resume(ctx, request.SessionId, request.ResumeToken, connection.ID, d.now())
	if err != nil {
		return d.sendError(connection, envelope.RequestId, ErrorResume, "session resume failed")
	}
	connection.BindSession(created.Record.SessionID, created.Record.ConnectionEpoch)
	if d.restore != nil {
		go d.restore(ctx, connection, created.Record.PlayerID, created.Record.SessionID, request.StateVersions)
	}
	return d.send(connection, MessageResumeResponse, envelope.RequestId, created.Record.SessionID, &gatewaypb.ResumeResponse{AccountId: created.Record.AccountID, SessionId: created.Record.SessionID, ResumeToken: created.ResumeToken, ConnectionEpoch: created.Record.ConnectionEpoch, PlayerId: created.Record.PlayerID})
}

func (d *Dispatcher) sendError(connection *Connection, requestID uint64, code uint32, message string) error {
	return d.send(connection, MessageErrorResponse, requestID, "", &gatewaypb.ErrorResponse{Code: code, Message: message})
}

func (d *Dispatcher) send(connection *Connection, messageID gatewaypb.ClientMessageId, requestID uint64, sessionID string, message proto.Message) error {
	payload, err := proto.Marshal(message)
	if err != nil {
		return err
	}
	envelope, err := proto.Marshal(&gatewaypb.Envelope{MessageId: messageID, RequestId: requestID, SessionId: sessionID, Payload: payload})
	if err != nil {
		return err
	}
	return connection.Send(envelope)
}
