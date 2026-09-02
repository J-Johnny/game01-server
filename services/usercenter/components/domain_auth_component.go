package components

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"server/common/idgen"
	"server/common/mongodb"
	"server/common/streaming"
	gatewaypb "server/proto/gen/client"
	internalpb "server/proto/gen/internalpb"
	"server/services/usercenter/domain"
	"server/services/usercenter/repository"
)

type DomainAuthComponent struct {
	accounts                 repository.AccountRepository
	identities               repository.IdentityRepository
	refreshTokens            repository.RefreshTokenRepository
	idempotency              repository.IdempotencyRepository
	coordinator              repository.IdempotencyCoordinator
	unitOfWork               mongodb.UnitOfWork
	refreshTokenTTL          time.Duration
	idempotencyTTL           time.Duration
	idempotencyLease         time.Duration
	idempotencyRenewInterval time.Duration
	now                      func() time.Time
	idempotencyMu            sync.Mutex
	idempotencyLock          map[string]*idempotencyLockEntry
}

type idempotencyLockEntry struct {
	mu   sync.Mutex
	refs int
}

func NewDomainAuthComponent(accounts repository.AccountRepository, identities repository.IdentityRepository, refreshTokens repository.RefreshTokenRepository, unitOfWork mongodb.UnitOfWork, refreshTokenTTL time.Duration, idempotency ...repository.IdempotencyRepository) *DomainAuthComponent {
	component := &DomainAuthComponent{
		accounts:                 accounts,
		identities:               identities,
		refreshTokens:            refreshTokens,
		unitOfWork:               unitOfWork,
		refreshTokenTTL:          refreshTokenTTL,
		idempotencyTTL:           24 * time.Hour,
		idempotencyLease:         30 * time.Second,
		idempotencyRenewInterval: 10 * time.Second,
		now:                      time.Now,
		idempotencyLock:          make(map[string]*idempotencyLockEntry),
	}
	if len(idempotency) > 0 {
		component.idempotency = idempotency[0]
		if coordinator, ok := idempotency[0].(repository.IdempotencyCoordinator); ok {
			component.coordinator = coordinator
		}
	}
	return component
}

func (c *DomainAuthComponent) WithIdempotencyLease(lease, renewInterval time.Duration) *DomainAuthComponent {
	if lease > 0 {
		c.idempotencyLease = lease
	}
	if renewInterval > 0 {
		c.idempotencyRenewInterval = renewInterval
	}
	return c
}

func (c *DomainAuthComponent) WithIdempotencyTTL(ttl time.Duration) *DomainAuthComponent {
	if ttl > 0 {
		c.idempotencyTTL = ttl
	}
	return c
}

func (c *DomainAuthComponent) RegisterInternal(router *streaming.Router) {
	router.Register(internalpb.ServiceType_SERVICE_TYPE_USERCENTER, uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_GUEST_AUTHENTICATE_REQUEST), streaming.MessageHandlerFunc(c.guestAuthenticate))
	router.Register(internalpb.ServiceType_SERVICE_TYPE_USERCENTER, uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_REFRESH_AUTHENTICATE_REQUEST), streaming.MessageHandlerFunc(c.refreshAuthenticate))
	router.Register(internalpb.ServiceType_SERVICE_TYPE_USERCENTER, uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_REVOKE_REFRESH_TOKEN_REQUEST), streaming.MessageHandlerFunc(c.revokeRefreshToken))
	router.Register(internalpb.ServiceType_SERVICE_TYPE_USERCENTER, uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_PASSWORD_AUTHENTICATE_REQUEST), streaming.MessageHandlerFunc(c.passwordAuthenticate))
	router.Register(internalpb.ServiceType_SERVICE_TYPE_USERCENTER, uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_LINK_PLAYER_REQUEST), streaming.MessageHandlerFunc(c.linkPlayer))
}

func (c *DomainAuthComponent) clientLoginAuthenticate(ctx context.Context, _ streaming.Peer, envelope *internalpb.InternalEnvelope) (*streaming.MessageResult, error) {
	request := &internalpb.ClientLoginAuthenticateRequest{}
	if err := proto.Unmarshal(envelope.Payload, request); err != nil {
		return nil, errors.New("invalid client login authentication request")
	}
	login := &gatewaypb.LoginRequest{}
	if err := proto.Unmarshal(request.LoginRequest, login); err != nil {
		return nil, errors.New("invalid client login request")
	}
	if strings.TrimSpace(login.InstallId) == "" {
		return nil, errors.New("install_id is required")
	}
	return c.withIdempotency(ctx, login.IdempotencyKey, "client_login_authenticate", login, &internalpb.ClientLoginAuthenticateResponse{}, func(executionContext context.Context) (*streaming.MessageResult, error) {
		return c.clientLoginAuthenticateCore(executionContext, login)
	})
}

func (c *DomainAuthComponent) clientLoginAuthenticateCore(ctx context.Context, login *gatewaypb.LoginRequest) (*streaming.MessageResult, error) {
	switch login.Provider {
	case gatewaypb.AuthProvider_AUTH_PROVIDER_GUEST:
		result, err := c.guestAuthenticateCore(ctx, &internalpb.GuestAuthenticateRequest{
			InstallId:      login.InstallId,
			IdempotencyKey: login.IdempotencyKey,
		})
		if err != nil {
			return nil, err
		}
		message := result.Message.(*internalpb.GuestAuthenticateResponse)
		return clientLoginGrant(message.AccountId, message.RefreshToken, message.RefreshTokenExpireAtUnix), nil
	case gatewaypb.AuthProvider_AUTH_PROVIDER_PASSWORD:
		result, err := c.passwordAuthenticateCore(ctx, &internalpb.PasswordAuthenticateRequest{
			Username:       login.Username,
			Password:       login.Password,
			InstallId:      login.InstallId,
			IdempotencyKey: login.IdempotencyKey,
		})
		if err != nil {
			return nil, err
		}
		message := result.Message.(*internalpb.PasswordAuthenticateResponse)
		return clientLoginGrant(message.AccountId, message.RefreshToken, message.RefreshTokenExpireAtUnix), nil
	default:
		return nil, errors.New("authentication provider is not enabled")
	}
}

func (c *DomainAuthComponent) clientRefreshAuthenticate(ctx context.Context, _ streaming.Peer, envelope *internalpb.InternalEnvelope) (*streaming.MessageResult, error) {
	request := &internalpb.ClientRefreshAuthenticateRequest{}
	if err := proto.Unmarshal(envelope.Payload, request); err != nil {
		return nil, errors.New("invalid client refresh authentication request")
	}
	refresh := &gatewaypb.RefreshLoginRequest{}
	if err := proto.Unmarshal(request.RefreshLoginRequest, refresh); err != nil {
		return nil, errors.New("invalid client refresh login request")
	}
	if strings.TrimSpace(refresh.RefreshToken) == "" || strings.TrimSpace(refresh.InstallId) == "" {
		return nil, errors.New("refresh_token and install_id are required")
	}
	return c.withIdempotency(ctx, refresh.IdempotencyKey, "client_refresh_authenticate", refresh, &internalpb.ClientRefreshAuthenticateResponse{}, func(executionContext context.Context) (*streaming.MessageResult, error) {
		result, err := c.refreshAuthenticateCore(executionContext, &internalpb.RefreshAuthenticateRequest{
			RefreshToken:   refresh.RefreshToken,
			InstallId:      refresh.InstallId,
			IdempotencyKey: refresh.IdempotencyKey,
		})
		if err != nil {
			return nil, err
		}
		message := result.Message.(*internalpb.RefreshAuthenticateResponse)
		return clientRefreshGrant(message.AccountId, message.RefreshToken, message.RefreshTokenExpireAtUnix), nil
	})
}

func clientLoginGrant(accountID, refreshToken string, expiresAtUnix int64) *streaming.MessageResult {
	return &streaming.MessageResult{
		MessageID: uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_CLIENT_LOGIN_AUTHENTICATE_RESPONSE),
		Message: &internalpb.ClientLoginAuthenticateResponse{
			Grant: &internalpb.AuthenticationGrant{
				AccountId:                accountID,
				RefreshToken:             refreshToken,
				RefreshTokenExpireAtUnix: expiresAtUnix,
			},
		},
	}
}

func clientRefreshGrant(accountID, refreshToken string, expiresAtUnix int64) *streaming.MessageResult {
	return &streaming.MessageResult{
		MessageID: uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_CLIENT_REFRESH_AUTHENTICATE_RESPONSE),
		Message: &internalpb.ClientRefreshAuthenticateResponse{
			Grant: &internalpb.AuthenticationGrant{
				AccountId:                accountID,
				RefreshToken:             refreshToken,
				RefreshTokenExpireAtUnix: expiresAtUnix,
			},
		},
	}
}

func (c *DomainAuthComponent) linkPlayer(ctx context.Context, _ streaming.Peer, envelope *internalpb.InternalEnvelope) (*streaming.MessageResult, error) {
	request := &internalpb.LinkPlayerRequest{}
	if err := proto.Unmarshal(envelope.Payload, request); err != nil {
		return nil, err
	}
	if strings.TrimSpace(request.AccountId) == "" || strings.TrimSpace(request.PlayerId) == "" {
		return nil, errors.New("account_id and player_id are required")
	}
	if err := c.accounts.LinkPlayer(ctx, request.AccountId, request.PlayerId, c.now().UTC()); err != nil {
		return nil, err
	}
	return &streaming.MessageResult{MessageID: uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_LINK_PLAYER_RESPONSE), Message: &internalpb.LinkPlayerResponse{AccountId: request.AccountId, PlayerId: request.PlayerId}}, nil
}

func (c *DomainAuthComponent) guestAuthenticate(ctx context.Context, _ streaming.Peer, envelope *internalpb.InternalEnvelope) (*streaming.MessageResult, error) {
	request := &internalpb.GuestAuthenticateRequest{}
	if err := proto.Unmarshal(envelope.Payload, request); err != nil || strings.TrimSpace(request.InstallId) == "" {
		return nil, errors.New("install_id is required")
	}
	return c.withIdempotency(ctx, request.IdempotencyKey, "guest_authenticate", request, &internalpb.GuestAuthenticateResponse{}, func(executionContext context.Context) (*streaming.MessageResult, error) {
		return c.guestAuthenticateCore(executionContext, request)
	})
}

func (c *DomainAuthComponent) guestAuthenticateCore(ctx context.Context, request *internalpb.GuestAuthenticateRequest) (*streaming.MessageResult, error) {
	installID := strings.TrimSpace(request.InstallId)
	identity, err := c.identities.Find(ctx, domain.AuthProviderGuest, installID)
	created := false
	if errors.Is(err, domain.ErrIdentityNotFound) {
		accountID, token, rawToken, createErr := c.createAccountAndToken(ctx, domain.AuthProviderGuest, installID, "", installID)
		if createErr == nil {
			return c.guestResult(accountID, token, rawToken, true), nil
		} else {
			identity, err = c.identities.Find(ctx, domain.AuthProviderGuest, installID)
			if err != nil {
				return nil, fmt.Errorf("create guest account: %w", createErr)
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("find guest identity: %w", err)
	}
	token, rawToken, err := c.issueToken(ctx, identity.AccountID, installID)
	if err != nil {
		return nil, err
	}
	return c.guestResult(identity.AccountID, token, rawToken, created), nil
}

func (c *DomainAuthComponent) guestResult(accountID string, token domain.RefreshToken, rawToken string, created bool) *streaming.MessageResult {
	return &streaming.MessageResult{
		MessageID: uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_GUEST_AUTHENTICATE_RESPONSE),
		Message: &internalpb.GuestAuthenticateResponse{
			AccountId:                accountID,
			RefreshToken:             rawToken,
			RefreshTokenExpireAtUnix: token.ExpiresAt.Unix(),
			Created:                  created,
		},
	}
}

func (c *DomainAuthComponent) passwordAuthenticate(ctx context.Context, _ streaming.Peer, envelope *internalpb.InternalEnvelope) (*streaming.MessageResult, error) {
	request := &internalpb.PasswordAuthenticateRequest{}
	if err := proto.Unmarshal(envelope.Payload, request); err != nil {
		return nil, errors.New("invalid password authentication request")
	}
	return c.withIdempotency(ctx, request.IdempotencyKey, "password_authenticate", request, &internalpb.PasswordAuthenticateResponse{}, func(executionContext context.Context) (*streaming.MessageResult, error) {
		return c.passwordAuthenticateCore(executionContext, request)
	})
}

func (c *DomainAuthComponent) passwordAuthenticateCore(ctx context.Context, request *internalpb.PasswordAuthenticateRequest) (*streaming.MessageResult, error) {
	username := strings.TrimSpace(request.Username)
	installID := strings.TrimSpace(request.InstallId)
	if username == "" || request.Password == "" || installID == "" || len(username) < 3 || len(username) > 64 || len(request.Password) < 6 || len(request.Password) > 128 {
		return nil, errors.New("invalid username or password")
	}
	identity, err := c.identities.Find(ctx, domain.AuthProviderPassword, username)
	if errors.Is(err, domain.ErrIdentityNotFound) {
		accountID, token, rawToken, createErr := c.createAccountAndToken(ctx, domain.AuthProviderPassword, username, request.Password, installID)
		if createErr == nil {
			return &streaming.MessageResult{
				MessageID: uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_PASSWORD_AUTHENTICATE_RESPONSE),
				Message: &internalpb.PasswordAuthenticateResponse{
					AccountId:                accountID,
					RefreshToken:             rawToken,
					RefreshTokenExpireAtUnix: token.ExpiresAt.Unix(),
				},
			}, nil
		} else {
			identity, err = c.identities.Find(ctx, domain.AuthProviderPassword, username)
			if err != nil {
				return nil, fmt.Errorf("create password account: %w", createErr)
			}
			if identity.PasswordHash == "" || bcrypt.CompareHashAndPassword([]byte(identity.PasswordHash), []byte(request.Password)) != nil {
				return nil, errors.New("invalid username or password")
			}
		}
	} else if err == nil && (identity.PasswordHash == "" || bcrypt.CompareHashAndPassword([]byte(identity.PasswordHash), []byte(request.Password)) != nil) {
		return nil, errors.New("invalid username or password")
	}
	if err != nil {
		return nil, fmt.Errorf("find password identity: %w", err)
	}
	token, rawToken, err := c.issueToken(ctx, identity.AccountID, installID)
	if err != nil {
		return nil, err
	}
	return &streaming.MessageResult{
		MessageID: uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_PASSWORD_AUTHENTICATE_RESPONSE),
		Message: &internalpb.PasswordAuthenticateResponse{
			AccountId:                identity.AccountID,
			RefreshToken:             rawToken,
			RefreshTokenExpireAtUnix: token.ExpiresAt.Unix(),
		},
	}, nil
}

func (c *DomainAuthComponent) refreshAuthenticate(ctx context.Context, _ streaming.Peer, envelope *internalpb.InternalEnvelope) (*streaming.MessageResult, error) {
	request := &internalpb.RefreshAuthenticateRequest{}
	if err := proto.Unmarshal(envelope.Payload, request); err != nil || strings.TrimSpace(request.RefreshToken) == "" || strings.TrimSpace(request.InstallId) == "" {
		return nil, errors.New("refresh_token and install_id are required")
	}
	return c.withIdempotency(ctx, request.IdempotencyKey, "refresh_authenticate", request, &internalpb.RefreshAuthenticateResponse{}, func(executionContext context.Context) (*streaming.MessageResult, error) {
		return c.refreshAuthenticateCore(executionContext, request)
	})
}

func (c *DomainAuthComponent) refreshAuthenticateCore(ctx context.Context, request *internalpb.RefreshAuthenticateRequest) (*streaming.MessageResult, error) {
	now := c.now().UTC()
	oldToken, err := c.refreshTokens.FindValid(ctx, authHashToken(request.RefreshToken), now)
	if err != nil {
		return nil, fmt.Errorf("find refresh token: %w", err)
	}
	if !oldToken.IsUsable(now, strings.TrimSpace(request.InstallId)) {
		return nil, domain.ErrInvalidToken
	}
	replacement, rawToken, err := c.newToken(oldToken.AccountID, request.InstallId)
	if err != nil {
		return nil, err
	}
	if err := c.refreshTokens.Rotate(ctx, oldToken.ID, now, &replacement); err != nil {
		return nil, fmt.Errorf("rotate refresh token: %w", err)
	}
	return &streaming.MessageResult{
		MessageID: uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_REFRESH_AUTHENTICATE_RESPONSE),
		Message: &internalpb.RefreshAuthenticateResponse{
			AccountId:                oldToken.AccountID,
			RefreshToken:             rawToken,
			RefreshTokenExpireAtUnix: replacement.ExpiresAt.Unix(),
		},
	}, nil
}

func (c *DomainAuthComponent) revokeRefreshToken(ctx context.Context, _ streaming.Peer, envelope *internalpb.InternalEnvelope) (*streaming.MessageResult, error) {
	request := &internalpb.RevokeRefreshTokenRequest{}
	if err := proto.Unmarshal(envelope.Payload, request); err != nil || strings.TrimSpace(request.AccountId) == "" || strings.TrimSpace(request.RefreshToken) == "" {
		return nil, errors.New("account_id and refresh_token are required")
	}
	return c.withIdempotency(ctx, request.IdempotencyKey, "revoke_refresh_token", request, &internalpb.RevokeRefreshTokenResponse{}, func(executionContext context.Context) (*streaming.MessageResult, error) {
		return c.revokeRefreshTokenCore(executionContext, request)
	})
}

func (c *DomainAuthComponent) revokeRefreshTokenCore(ctx context.Context, request *internalpb.RevokeRefreshTokenRequest) (*streaming.MessageResult, error) {
	token, err := c.refreshTokens.FindValid(ctx, authHashToken(request.RefreshToken), c.now().UTC())
	if err != nil || token.AccountID != strings.TrimSpace(request.AccountId) {
		return nil, domain.ErrInvalidToken
	}
	if err := c.refreshTokens.Revoke(ctx, token.ID, c.now().UTC()); err != nil {
		return nil, fmt.Errorf("revoke refresh token: %w", err)
	}
	return &streaming.MessageResult{
		MessageID: uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_REVOKE_REFRESH_TOKEN_RESPONSE),
		Message:   &internalpb.RevokeRefreshTokenResponse{},
	}, nil
}

func (c *DomainAuthComponent) withIdempotency(ctx context.Context, key, operation string, request, response proto.Message, execute func(context.Context) (*streaming.MessageResult, error)) (*streaming.MessageResult, error) {
	key = strings.TrimSpace(key)
	if c.idempotency == nil || key == "" {
		return execute(ctx)
	}
	lock := c.acquireIdempotencyLock(key)
	lock.mu.Lock()
	defer func() {
		lock.mu.Unlock()
		c.releaseIdempotencyLock(key, lock)
	}()
	requestBytes := proto.Clone(request)
	clearIdempotencyKey(requestBytes)
	canonical, err := proto.Marshal(requestBytes)
	if err != nil {
		return nil, fmt.Errorf("marshal idempotency request: %w", err)
	}
	hash := sha256.Sum256(canonical)
	requestHash := hex.EncodeToString(hash[:])
	now := c.now().UTC()
	record, err := c.idempotency.Find(ctx, key, now)
	if err == nil {
		if record.IsCompleted() {
			return replayIdempotency(record, operation, requestHash, response)
		}
		if c.coordinator != nil {
			if record.Operation != operation || record.RequestHash != requestHash {
				return nil, domain.ErrIdempotencyConflict
			}
			return c.waitForIdempotency(ctx, key, operation, requestHash, response, record.LeaseUntil, execute)
		}
		return nil, domain.ErrIdempotencyConflict
	}
	if !errors.Is(err, repository.ErrIdempotencyNotFound) {
		return nil, fmt.Errorf("find idempotency record: %w", err)
	}
	if c.coordinator != nil {
		reservationID, reservationErr := idgen.NewUUID()
		if reservationErr != nil {
			return nil, fmt.Errorf("generate idempotency reservation: %w", reservationErr)
		}
		pending := &domain.IdempotencyRecord{
			Key:           key,
			Operation:     operation,
			RequestHash:   requestHash,
			State:         domain.IdempotencyStatePending,
			ReservationID: reservationID,
			LeaseUntil:    now.Add(c.idempotencyLease),
			CreatedAt:     now,
			ExpiresAt:     now.Add(c.idempotencyTTL),
		}
		claimed, ownsReservation, reserveErr := c.coordinator.Reserve(ctx, pending, now)
		if reserveErr != nil {
			return nil, reserveErr
		}
		if !ownsReservation {
			if claimed != nil && claimed.IsCompleted() {
				return replayIdempotency(claimed, operation, requestHash, response)
			}
			return c.waitForIdempotency(ctx, key, operation, requestHash, response, pending.LeaseUntil, execute)
		}
		return c.executeReserved(ctx, key, reservationID, execute)
	}
	result, err := execute(ctx)
	if err != nil || result == nil || result.Message == nil {
		return result, err
	}
	responseBytes, err := proto.Marshal(result.Message)
	if err != nil {
		return nil, fmt.Errorf("marshal idempotency response: %w", err)
	}
	record = &domain.IdempotencyRecord{Key: key, Operation: operation, RequestHash: requestHash, Response: responseBytes, CreatedAt: now, ExpiresAt: now.Add(c.idempotencyTTL)}
	if err := c.idempotency.Create(ctx, record); err != nil {
		if errors.Is(err, domain.ErrIdempotencyConflict) {
			existing, findErr := c.idempotency.Find(ctx, key, now)
			if findErr == nil {
				return replayIdempotency(existing, operation, requestHash, response)
			}
			if errors.Is(findErr, repository.ErrIdempotencyNotFound) {
				return nil, domain.ErrIdempotencyConflict
			}
			return nil, fmt.Errorf("find conflicting idempotency record: %w", findErr)
		}
		return nil, fmt.Errorf("store idempotency record: %w", err)
	}
	return result, nil
}

func (c *DomainAuthComponent) executeReserved(ctx context.Context, key, reservationID string, execute func(context.Context) (*streaming.MessageResult, error)) (*streaming.MessageResult, error) {
	executionContext, cancel := context.WithCancel(ctx)
	renewErr := make(chan error, 1)
	renewDone := make(chan struct{})
	go func() {
		defer close(renewDone)
		c.renewIdempotencyLease(executionContext, key, reservationID, renewErr, cancel)
	}()
	result, executeErr := execute(executionContext)
	cancel()
	<-renewDone
	select {
	case leaseErr := <-renewErr:
		if executeErr == nil {
			executeErr = leaseErr
		}
	default:
	}
	if executeErr != nil || result == nil || result.Message == nil {
		_ = c.coordinator.Release(ctx, key, reservationID)
		return result, executeErr
	}
	responseBytes, marshalErr := proto.Marshal(result.Message)
	if marshalErr != nil {
		_ = c.coordinator.Release(ctx, key, reservationID)
		return nil, fmt.Errorf("marshal idempotency response: %w", marshalErr)
	}
	if completeErr := c.coordinator.Complete(ctx, key, reservationID, responseBytes, c.now().UTC()); completeErr != nil {
		return nil, completeErr
	}
	return result, nil
}

func (c *DomainAuthComponent) renewIdempotencyLease(ctx context.Context, key, reservationID string, errors chan<- error, cancel context.CancelFunc) {
	interval := c.idempotencyRenewInterval
	if interval <= 0 {
		interval = c.idempotencyLease / 3
	}
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := c.coordinator.Renew(ctx, key, reservationID, now.Add(c.idempotencyLease), now); err != nil {
				select {
				case errors <- err:
				default:
				}
				cancel()
				return
			}
		}
	}
}

func (c *DomainAuthComponent) waitForIdempotency(ctx context.Context, key, operation, requestHash string, response proto.Message, deadline time.Time, execute func(context.Context) (*streaming.MessageResult, error)) (*streaming.MessageResult, error) {
	for {
		now := c.now().UTC()
		if now.After(deadline) {
			reservationID, err := idgen.NewUUID()
			if err != nil {
				return nil, err
			}
			pending := &domain.IdempotencyRecord{
				Key:           key,
				Operation:     operation,
				RequestHash:   requestHash,
				State:         domain.IdempotencyStatePending,
				ReservationID: reservationID,
				LeaseUntil:    now.Add(c.idempotencyLease),
				CreatedAt:     now,
				ExpiresAt:     now.Add(c.idempotencyTTL),
			}
			claimed, owns, reserveErr := c.coordinator.Reserve(ctx, pending, now)
			if reserveErr != nil {
				return nil, reserveErr
			}
			if owns {
				return c.executeReserved(ctx, key, reservationID, execute)
			}
			if claimed != nil && claimed.IsCompleted() {
				return replayIdempotency(claimed, operation, requestHash, response)
			}
			if claimed != nil {
				deadline = claimed.LeaseUntil
			}
		}
		record, err := c.idempotency.Find(ctx, key, now)
		if err == nil {
			if record.IsCompleted() {
				return replayIdempotency(record, operation, requestHash, response)
			}
		} else if !errors.Is(err, repository.ErrIdempotencyNotFound) {
			return nil, err
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (c *DomainAuthComponent) acquireIdempotencyLock(key string) *idempotencyLockEntry {
	c.idempotencyMu.Lock()
	defer c.idempotencyMu.Unlock()
	lock := c.idempotencyLock[key]
	if lock == nil {
		lock = &idempotencyLockEntry{}
		c.idempotencyLock[key] = lock
	}
	lock.refs++
	return lock
}

func (c *DomainAuthComponent) releaseIdempotencyLock(key string, lock *idempotencyLockEntry) {
	c.idempotencyMu.Lock()
	defer c.idempotencyMu.Unlock()
	lock.refs--
	if lock.refs == 0 && c.idempotencyLock[key] == lock {
		delete(c.idempotencyLock, key)
	}
}

func replayIdempotency(record *domain.IdempotencyRecord, operation, requestHash string, response proto.Message) (*streaming.MessageResult, error) {
	if record == nil || record.Operation != operation || record.RequestHash != requestHash {
		return nil, domain.ErrIdempotencyConflict
	}
	if err := proto.Unmarshal(record.Response, response); err != nil {
		return nil, fmt.Errorf("unmarshal idempotency response: %w", err)
	}
	return &streaming.MessageResult{MessageID: responseMessageID(operation), Message: response}, nil
}

func clearIdempotencyKey(message proto.Message) {
	message.ProtoReflect().Set(message.ProtoReflect().Descriptor().Fields().ByName("idempotency_key"), protoreflect.ValueOfString(""))
}

func responseMessageID(operation string) uint32 {
	switch operation {
	case "guest_authenticate":
		return uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_GUEST_AUTHENTICATE_RESPONSE)
	case "password_authenticate":
		return uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_PASSWORD_AUTHENTICATE_RESPONSE)
	case "refresh_authenticate":
		return uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_REFRESH_AUTHENTICATE_RESPONSE)
	default:
		return uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_REVOKE_REFRESH_TOKEN_RESPONSE)
	}
}

func (c *DomainAuthComponent) createAccountAndToken(ctx context.Context, provider domain.AuthProvider, subject, password, installID string) (string, domain.RefreshToken, string, error) {
	if c.unitOfWork == nil {
		return "", domain.RefreshToken{}, "", errors.New("user center transaction is not configured")
	}
	accountID, err := idgen.NewUUID()
	if err != nil {
		return "", domain.RefreshToken{}, "", fmt.Errorf("generate account id: %w", err)
	}
	identityID, err := idgen.NewUUID()
	if err != nil {
		return "", domain.RefreshToken{}, "", fmt.Errorf("generate identity id: %w", err)
	}
	passwordHash := ""
	if password != "" {
		hash, hashErr := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if hashErr != nil {
			return "", domain.RefreshToken{}, "", fmt.Errorf("hash password: %w", hashErr)
		}
		passwordHash = string(hash)
	}
	now := c.now().UTC()
	account := &domain.Account{
		ID:        accountID,
		CreatedAt: now,
		UpdatedAt: now,
	}
	identity := &domain.Identity{
		ID:           identityID,
		AccountID:    accountID,
		Provider:     provider,
		Subject:      subject,
		PasswordHash: passwordHash,
		LinkedAt:     now,
	}
	token, rawToken, err := c.newToken(accountID, installID)
	if err != nil {
		return "", domain.RefreshToken{}, "", err
	}
	err = c.unitOfWork.Execute(ctx, func(transactionContext context.Context) error {
		if err := c.accounts.Create(transactionContext, account); err != nil {
			return err
		}
		if err := c.identities.Create(transactionContext, identity); err != nil {
			return err
		}
		return c.refreshTokens.Create(transactionContext, &token)
	})
	return accountID, token, rawToken, err
}

func (c *DomainAuthComponent) issueToken(ctx context.Context, accountID, installID string) (domain.RefreshToken, string, error) {
	token, rawToken, err := c.newToken(accountID, installID)
	if err != nil {
		return domain.RefreshToken{}, "", err
	}
	if err := c.refreshTokens.Create(ctx, &token); err != nil {
		return domain.RefreshToken{}, "", fmt.Errorf("store refresh token: %w", err)
	}
	return token, rawToken, nil
}

func (c *DomainAuthComponent) newToken(accountID, installID string) (domain.RefreshToken, string, error) {
	if accountID == "" || installID == "" || c.refreshTokenTTL <= 0 {
		return domain.RefreshToken{}, "", domain.ErrInvalidToken
	}
	tokenID, err := idgen.NewUUID()
	if err != nil {
		return domain.RefreshToken{}, "", err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return domain.RefreshToken{}, "", err
	}
	rawToken := base64.RawURLEncoding.EncodeToString(raw)
	now := c.now().UTC()
	return domain.RefreshToken{ID: tokenID, AccountID: accountID, TokenHash: authHashToken(rawToken), InstallID: strings.TrimSpace(installID), CreatedAt: now, ExpiresAt: now.Add(c.refreshTokenTTL)}, rawToken, nil
}

func authHashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}
