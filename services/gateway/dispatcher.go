package gateway

import (
	"context"
	"errors"
	"log/slog"
	"time"

	gatewaypb "server/proto/gen/client"
	servicecommon "server/services/common"
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

type AuthenticationService = servicecommon.AuthenticationService

type AuthenticationGrant = servicecommon.AuthenticationGrant

type RejectingAuthenticator struct{}

func (RejectingAuthenticator) AuthenticateLogin(context.Context, []byte) (AuthenticationGrant, error) {
	return AuthenticationGrant{}, errors.New("authentication service is not configured")
}

func (RejectingAuthenticator) RefreshLogin(context.Context, []byte) (AuthenticationGrant, error) {
	return AuthenticationGrant{}, errors.New("authentication service is not configured")
}

type Dispatcher struct {
	authenticator AuthenticationService
	sessions      *session.Manager
	now           func() time.Time
	restore       func(context.Context, *Connection, string, string, map[string]uint64)
	players       PlayerResolver
	errors        *ErrorMapper
}

func (d *Dispatcher) SetRestoreHandler(handler func(context.Context, *Connection, string, string, map[string]uint64)) {
	d.restore = handler
}

func NewDispatcher(authenticator AuthenticationService, sessions *session.Manager, players ...PlayerResolver) *Dispatcher {
	dispatcher := &Dispatcher{
		authenticator: authenticator,
		sessions:      sessions,
		now:           time.Now,
		errors:        NewErrorMapper(nil),
	}
	if len(players) > 0 {
		dispatcher.players = players[0]
	}
	return dispatcher
}

func (d *Dispatcher) SetErrorMapper(mapper *ErrorMapper) {
	if mapper != nil {
		d.errors = mapper
	}
}

func (d *Dispatcher) Dispatch(ctx context.Context, connection *Connection, data []byte) error {
	envelope := &gatewaypb.Envelope{}
	err := proto.Unmarshal(data, envelope)
	if err != nil {
		return d.sendKnownError(connection, 0, ErrorInvalidEnvelope, "invalid envelope")
	}
	if envelope.MessageId == 0 {
		return d.sendKnownError(connection, 0, ErrorInvalidEnvelope, "message id is required")
	}
	switch envelope.MessageId {
	case MessageLoginRequest:
		return d.login(ctx, connection, envelope)
	case MessageResumeRequest:
		return d.resume(ctx, connection, envelope)
	case MessageRefreshLoginRequest:
		return d.refreshLogin(ctx, connection, envelope)
	default:
		return d.sendKnownError(connection, envelope.RequestId, ErrorUnsupported, "unsupported message")
	}
}

func (d *Dispatcher) login(ctx context.Context, connection *Connection, envelope *gatewaypb.Envelope) error {
	authentication, err := d.authenticator.AuthenticateLogin(ctx, envelope.Payload)
	if err != nil {
		slog.Warn("gateway authentication failed", "protocol", "websocket", "connection_id", connection.ID, "request_id", envelope.RequestId, "error", err)
		return d.sendMappedError(connection, envelope.RequestId, err, ErrorAuthentication, "authentication failed")
	}
	playerID, err := d.ensurePlayer(ctx, authentication.AccountID)
	if err != nil {
		return d.sendMappedError(connection, envelope.RequestId, err, ErrorPlayer, "player initialization failed")
	}
	created, err := d.sessions.Create(ctx, authentication.AccountID, connection.ID, d.now(), playerID)
	if err != nil {
		return d.sendMappedError(connection, envelope.RequestId, err, ErrorInternal, "create session failed")
	}
	connection.BindSession(created.Record.SessionID, created.Record.ConnectionEpoch)
	if d.restore != nil {
		go d.restore(ctx, connection, created.Record.PlayerID, created.Record.SessionID, nil)
	}
	return d.send(connection, MessageLoginResponse, envelope.RequestId, created.Record.SessionID, &gatewaypb.LoginResponse{AccountId: created.Record.AccountID, PlayerId: created.Record.PlayerID, SessionId: created.Record.SessionID, ResumeToken: created.ResumeToken, ConnectionEpoch: created.Record.ConnectionEpoch, RefreshToken: authentication.RefreshToken, RefreshTokenExpireAtUnix: authentication.RefreshTokenExpireAt.Unix()})
}

func (d *Dispatcher) refreshLogin(ctx context.Context, connection *Connection, envelope *gatewaypb.Envelope) error {
	authentication, err := d.authenticator.RefreshLogin(ctx, envelope.Payload)
	if err != nil {
		return d.sendMappedError(connection, envelope.RequestId, err, ErrorRefresh, "refresh authentication failed")
	}
	playerID, err := d.ensurePlayer(ctx, authentication.AccountID)
	if err != nil {
		return d.sendMappedError(connection, envelope.RequestId, err, ErrorPlayer, "player initialization failed")
	}
	created, err := d.sessions.Create(ctx, authentication.AccountID, connection.ID, d.now(), playerID)
	if err != nil {
		return d.sendMappedError(connection, envelope.RequestId, err, ErrorInternal, "create session failed")
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
		return d.sendKnownError(connection, envelope.RequestId, ErrorInvalidEnvelope, "invalid resume request")
	}
	if request.SessionId == "" || request.ResumeToken == "" {
		return d.sendKnownError(connection, envelope.RequestId, ErrorInvalidEnvelope, "session credentials are required")
	}
	created, err := d.sessions.Resume(ctx, request.SessionId, request.ResumeToken, connection.ID, d.now())
	if err != nil {
		return d.sendMappedError(connection, envelope.RequestId, err, ErrorResume, "session resume failed")
	}
	connection.BindSession(created.Record.SessionID, created.Record.ConnectionEpoch)
	if d.restore != nil {
		go d.restore(ctx, connection, created.Record.PlayerID, created.Record.SessionID, request.StateVersions)
	}
	return d.send(connection, MessageResumeResponse, envelope.RequestId, created.Record.SessionID, &gatewaypb.ResumeResponse{AccountId: created.Record.AccountID, SessionId: created.Record.SessionID, ResumeToken: created.ResumeToken, ConnectionEpoch: created.Record.ConnectionEpoch, PlayerId: created.Record.PlayerID})
}

func (d *Dispatcher) sendKnownError(connection *Connection, requestID uint64, code gatewaypb.GatewayErrorCode, message string) error {
	return d.sendPublicError(connection, requestID, d.errors.Known(code, message))
}

func (d *Dispatcher) sendMappedError(connection *Connection, requestID uint64, err error, fallback gatewaypb.GatewayErrorCode, message string) error {
	return d.sendPublicError(connection, requestID, d.errors.Map(err, fallback, message))
}

func (d *Dispatcher) sendPublicError(connection *Connection, requestID uint64, publicError PublicError) error {
	d.errors.Observe(publicError)
	return d.send(connection, MessageErrorResponse, requestID, "", publicError.Response())
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
