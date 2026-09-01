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
	"time"

	"github.com/qiniu/qmgo"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/protobuf/proto"
	"server/common/idgen"
	"server/common/streaming"
	internalpb "server/proto/gen/internalpb"
	"server/services/common/repository/dbmodel/md"
	"server/services/usercenter/repository"
	"server/services/usercenter/repository/models"
)

const (
	ErrorInvalidRequest uint32 = 1001
	ErrorUnauthorized   uint32 = 1002
)

type AuthComponent struct {
	accounts        repository.IAccountRepository
	refreshTokenTTL time.Duration
	now             func() time.Time
}

func NewAuthComponent(accounts repository.IAccountRepository, refreshTokenTTL time.Duration) *AuthComponent {
	return &AuthComponent{accounts: accounts, refreshTokenTTL: refreshTokenTTL, now: time.Now}
}

func (c *AuthComponent) RegisterInternal(router *streaming.Router) {
	router.Register(internalpb.ServiceType_SERVICE_TYPE_USERCENTER, uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_GUEST_AUTHENTICATE_REQUEST), streaming.MessageHandlerFunc(c.guestAuthenticate))
	router.Register(internalpb.ServiceType_SERVICE_TYPE_USERCENTER, uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_REFRESH_AUTHENTICATE_REQUEST), streaming.MessageHandlerFunc(c.refreshAuthenticate))
	router.Register(internalpb.ServiceType_SERVICE_TYPE_USERCENTER, uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_REVOKE_REFRESH_TOKEN_REQUEST), streaming.MessageHandlerFunc(c.revokeRefreshToken))
	router.Register(internalpb.ServiceType_SERVICE_TYPE_USERCENTER, uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_PASSWORD_AUTHENTICATE_REQUEST), streaming.MessageHandlerFunc(c.passwordAuthenticate))
}

func (c *AuthComponent) passwordAuthenticate(ctx context.Context, _ streaming.Peer, envelope *internalpb.InternalEnvelope) (*streaming.MessageResult, error) {
	request := &internalpb.PasswordAuthenticateRequest{}
	if err := proto.Unmarshal(envelope.Payload, request); err != nil {
		return nil, errors.New("invalid password authentication request")
	}
	username := strings.TrimSpace(request.Username)
	installID := strings.TrimSpace(request.InstallId)
	if username == "" || request.Password == "" || installID == "" {
		return nil, errors.New("username, password and install_id are required")
	}
	if len(username) < 3 || len(username) > 64 || len(request.Password) < 6 || len(request.Password) > 128 {
		return nil, errors.New("invalid username or password")
	}

	account, err := c.accounts.FindByIdentity(ctx, models.AuthProviderPassword, username)
	if errors.Is(err, models.ErrAccountNotFound) {
		var created bool
		account, created, err = c.createPasswordAccount(ctx, username, request.Password)
		if err == nil && !created {
			identity := findIdentity(account, models.AuthProviderPassword, username)
			if identity == nil || bcrypt.CompareHashAndPassword([]byte(identity.PasswordHash), []byte(request.Password)) != nil {
				return nil, errors.New("invalid username or password")
			}
		}
	} else if err == nil {
		identity := findIdentity(account, models.AuthProviderPassword, username)
		if identity == nil || bcrypt.CompareHashAndPassword([]byte(identity.PasswordHash), []byte(request.Password)) != nil {
			return nil, errors.New("invalid username or password")
		}
	}
	if err != nil {
		return nil, fmt.Errorf("authenticate password: %w", err)
	}
	token, refreshToken, err := c.newRefreshToken(account.AccountID, installID)
	if err != nil {
		return nil, err
	}
	if err := c.accounts.StoreRefreshToken(ctx, account.AccountID, token, c.now().UTC()); err != nil {
		return nil, fmt.Errorf("store password refresh token: %w", err)
	}
	return &streaming.MessageResult{
		MessageID: uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_PASSWORD_AUTHENTICATE_RESPONSE),
		Message: &internalpb.PasswordAuthenticateResponse{
			AccountId: account.AccountID, RefreshToken: refreshToken, RefreshTokenExpireAtUnix: token.ExpiresAt.Unix(),
		},
	}, nil
}

func (c *AuthComponent) createPasswordAccount(ctx context.Context, username, password string) (models.Account, bool, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return models.Account{}, false, fmt.Errorf("hash password: %w", err)
	}
	accountID, err := idgen.NewUUID()
	if err != nil {
		return models.Account{}, false, fmt.Errorf("generate account id: %w", err)
	}
	account := &models.Account{AccountID: accountID, Identities: md.NewNode(&models.Identities{Rows: md.NewMap[int64, *models.Identity]()})}
	account.Identities.Data().Rows.Set(1, &models.Identity{Provider: models.AuthProviderPassword, Subject: username, PasswordHash: string(hash)})
	if err := c.accounts.Create(ctx, account); err != nil {
		if qmgo.IsDup(err) {
			found, findErr := c.accounts.FindByIdentity(ctx, models.AuthProviderPassword, username)
			return found, false, findErr
		}
		return models.Account{}, false, err
	}
	return *account, true, nil
}

func findIdentity(account models.Account, provider models.AuthProvider, subject string) *models.Identity {
	if account.Identities == nil || account.Identities.Data() == nil || account.Identities.Data().Rows == nil {
		return nil
	}
	var found *models.Identity
	for _, identity := range account.Identities.Data().Rows.GetValueSlice() {
		if identity != nil && identity.Provider == provider && identity.Subject == subject {
			found = identity
			break
		}
	}
	return found
}

func (c *AuthComponent) guestAuthenticate(ctx context.Context, _ streaming.Peer, envelope *internalpb.InternalEnvelope) (*streaming.MessageResult, error) {
	request := &internalpb.GuestAuthenticateRequest{}
	if err := proto.Unmarshal(envelope.Payload, request); err != nil || strings.TrimSpace(request.InstallId) == "" {
		return nil, errors.New("install_id is required")
	}

	installID := strings.TrimSpace(request.InstallId)
	account, err := c.accounts.FindByIdentity(ctx, models.AuthProviderGuest, installID)
	created := false
	if errors.Is(err, models.ErrAccountNotFound) {
		account, err = c.createGuestAccount(ctx, installID)
		created = err == nil
	}
	if err != nil {
		return nil, fmt.Errorf("authenticate guest: %w", err)
	}

	token, refreshToken, err := c.newRefreshToken(account.AccountID, installID)
	if err != nil {
		return nil, err
	}
	if err := c.accounts.StoreRefreshToken(ctx, account.AccountID, token, c.now().UTC()); err != nil {
		return nil, fmt.Errorf("store guest refresh token: %w", err)
	}

	return &streaming.MessageResult{
		MessageID: uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_GUEST_AUTHENTICATE_RESPONSE),
		Message: &internalpb.GuestAuthenticateResponse{
			AccountId:                account.AccountID,
			RefreshToken:             refreshToken,
			RefreshTokenExpireAtUnix: token.ExpiresAt.Unix(),
			Created:                  created,
		},
	}, nil
}

func (c *AuthComponent) refreshAuthenticate(ctx context.Context, _ streaming.Peer, envelope *internalpb.InternalEnvelope) (*streaming.MessageResult, error) {
	request := &internalpb.RefreshAuthenticateRequest{}
	if err := proto.Unmarshal(envelope.Payload, request); err != nil || strings.TrimSpace(request.RefreshToken) == "" || strings.TrimSpace(request.InstallId) == "" {
		return nil, errors.New("refresh_token and install_id are required")
	}

	now := c.now().UTC()
	oldHash := hashToken(request.RefreshToken)
	account, err := c.accounts.FindByRefreshTokenHash(ctx, oldHash, now)
	if err != nil {
		return nil, fmt.Errorf("find refresh token: %w", err)
	}
	token, refreshToken, err := c.newRefreshToken(account.AccountID, strings.TrimSpace(request.InstallId))
	if err != nil {
		return nil, err
	}
	if _, err := c.accounts.RotateRefreshToken(ctx, account.AccountID, oldHash, strings.TrimSpace(request.InstallId), now, token); err != nil {
		return nil, fmt.Errorf("rotate refresh token: %w", err)
	}

	return &streaming.MessageResult{
		MessageID: uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_REFRESH_AUTHENTICATE_RESPONSE),
		Message: &internalpb.RefreshAuthenticateResponse{
			AccountId:                account.AccountID,
			RefreshToken:             refreshToken,
			RefreshTokenExpireAtUnix: token.ExpiresAt.Unix(),
		},
	}, nil
}

func (c *AuthComponent) revokeRefreshToken(ctx context.Context, _ streaming.Peer, envelope *internalpb.InternalEnvelope) (*streaming.MessageResult, error) {
	request := &internalpb.RevokeRefreshTokenRequest{}
	if err := proto.Unmarshal(envelope.Payload, request); err != nil || strings.TrimSpace(request.AccountId) == "" || strings.TrimSpace(request.RefreshToken) == "" {
		return nil, errors.New("account_id and refresh_token are required")
	}

	if err := c.accounts.RevokeRefreshToken(ctx, strings.TrimSpace(request.AccountId), hashToken(request.RefreshToken), c.now().UTC()); err != nil {
		return nil, fmt.Errorf("revoke refresh token: %w", err)
	}

	return &streaming.MessageResult{
		MessageID: uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_REVOKE_REFRESH_TOKEN_RESPONSE),
		Message:   &internalpb.RevokeRefreshTokenResponse{},
	}, nil
}

func (c *AuthComponent) createGuestAccount(ctx context.Context, installID string) (models.Account, error) {
	accountID, err := idgen.NewUUID()
	if err != nil {
		return models.Account{}, fmt.Errorf("generate account id: %w", err)
	}
	account := &models.Account{
		AccountID:  accountID,
		Identities: md.NewNode(&models.Identities{Rows: md.NewMap[int64, *models.Identity]()}),
	}
	account.Identities.Data().Rows.Set(1, &models.Identity{Provider: models.AuthProviderGuest, Subject: installID})
	if err := c.accounts.Create(ctx, account); err != nil {
		if !qmgo.IsDup(err) {
			return models.Account{}, err
		}
		return c.accounts.FindByIdentity(ctx, models.AuthProviderGuest, installID)
	}
	return *account, nil
}

func (c *AuthComponent) newRefreshToken(accountID, installID string) (models.RefreshToken, string, error) {
	if accountID == "" || installID == "" || c.refreshTokenTTL <= 0 {
		return models.RefreshToken{}, "", models.ErrInvalidAccount
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return models.RefreshToken{}, "", fmt.Errorf("generate refresh token: %w", err)
	}
	value := base64.RawURLEncoding.EncodeToString(raw)
	now := c.now().UTC()
	return models.RefreshToken{TokenHash: hashToken(value), InstallID: installID, CreatedAt: now, ExpiresAt: now.Add(c.refreshTokenTTL)}, value, nil
}

func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}
