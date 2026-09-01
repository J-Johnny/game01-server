package components

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"server/common/streaming"
	internalpb "server/proto/gen/internalpb"
	"server/services/usercenter/repository"
	"server/services/usercenter/repository/models"
)

type memoryAccountRepository struct {
	accounts []models.Account
	tokens   map[string][]models.RefreshToken
}

func (r *memoryAccountRepository) EnsureIndexes(context.Context) error { return nil }

func (r *memoryAccountRepository) Create(_ context.Context, account *models.Account) error {
	for _, item := range r.accounts {
		if item.AccountID == account.AccountID {
			return errors.New("duplicate account")
		}
	}
	r.accounts = append(r.accounts, *account)
	return nil
}

func (r *memoryAccountRepository) FindByID(_ context.Context, accountID string) (models.Account, error) {
	for _, account := range r.accounts {
		if account.AccountID == accountID {
			return account, nil
		}
	}
	return models.Account{}, models.ErrAccountNotFound
}

func (r *memoryAccountRepository) FindByIdentity(_ context.Context, provider models.AuthProvider, subject string) (models.Account, error) {
	for _, account := range r.accounts {
		for _, identity := range account.Identities.Data().Rows.GetValueSlice() {
			if identity.Provider == provider && identity.Subject == subject {
				return account, nil
			}
		}
	}
	return models.Account{}, models.ErrAccountNotFound
}

func (r *memoryAccountRepository) FindByRefreshTokenHash(_ context.Context, tokenHash string, now time.Time) (models.Account, error) {
	for _, account := range r.accounts {
		for _, token := range r.tokens[account.AccountID] {
			if token.TokenHash == tokenHash && token.RevokedAt == nil && now.Before(token.ExpiresAt) {
				return account, nil
			}
		}
	}
	return models.Account{}, models.ErrAccountNotFound
}

func (r *memoryAccountRepository) LinkPlayer(context.Context, string, string, time.Time) error {
	return nil
}

func (r *memoryAccountRepository) StoreRefreshToken(_ context.Context, accountID string, token models.RefreshToken, _ time.Time) error {
	for i := range r.accounts {
		if r.accounts[i].AccountID == accountID {
			if r.tokens == nil {
				r.tokens = make(map[string][]models.RefreshToken)
			}
			r.tokens[accountID] = append(r.tokens[accountID], token)
			return nil
		}
	}
	return models.ErrAccountNotFound
}

func (r *memoryAccountRepository) RevokeRefreshToken(_ context.Context, accountID, tokenHash string, now time.Time) error {
	for i := range r.tokens[accountID] {
		if r.tokens[accountID][i].TokenHash == tokenHash {
			r.tokens[accountID][i].RevokedAt = &now
			return nil
		}
	}
	return models.ErrAccountNotFound
}

func (r *memoryAccountRepository) RotateRefreshToken(_ context.Context, accountID, tokenHash, installID string, now time.Time, replacement models.RefreshToken) (models.Account, error) {
	for i := range r.accounts {
		account := &r.accounts[i]
		if account.AccountID != accountID {
			continue
		}
		for i := range r.tokens[accountID] {
			token := &r.tokens[accountID][i]
			if token.TokenHash == tokenHash && token.InstallID == installID && token.RevokedAt == nil && now.Before(token.ExpiresAt) {
				token.RevokedAt = &now
				r.tokens[accountID] = append(r.tokens[accountID], replacement)
				return *account, nil
			}
		}
	}
	return models.Account{}, models.ErrAccountNotFound
}

func TestAuthComponentGuestAndRefresh(t *testing.T) {
	repo := &memoryAccountRepository{}
	component := NewAuthComponent(repo, time.Hour)
	clock := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	component.now = func() time.Time { return clock }

	guestPayload, err := proto.Marshal(&internalpb.GuestAuthenticateRequest{InstallId: "install-1"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := component.guestAuthenticate(context.Background(), streaming.Peer{}, &internalpb.InternalEnvelope{Payload: guestPayload})
	if err != nil {
		t.Fatalf("guestAuthenticate() error = %v", err)
	}
	guestResponse := result.Message.(*internalpb.GuestAuthenticateResponse)
	if !guestResponse.Created || guestResponse.AccountId == "" || guestResponse.RefreshToken == "" {
		t.Fatalf("unexpected guest response: %+v", guestResponse)
	}

	secondResult, err := component.guestAuthenticate(context.Background(), streaming.Peer{}, &internalpb.InternalEnvelope{Payload: guestPayload})
	if err != nil {
		t.Fatalf("second guestAuthenticate() error = %v", err)
	}
	secondResponse := secondResult.Message.(*internalpb.GuestAuthenticateResponse)
	if secondResponse.Created || secondResponse.AccountId != guestResponse.AccountId {
		t.Fatalf("guest login was not idempotent: %+v", secondResponse)
	}

	refreshPayload, err := proto.Marshal(&internalpb.RefreshAuthenticateRequest{RefreshToken: secondResponse.RefreshToken, InstallId: "install-1"})
	if err != nil {
		t.Fatal(err)
	}
	refreshResult, err := component.refreshAuthenticate(context.Background(), streaming.Peer{}, &internalpb.InternalEnvelope{Payload: refreshPayload})
	if err != nil {
		t.Fatalf("refreshAuthenticate() error = %v", err)
	}
	refreshResponse := refreshResult.Message.(*internalpb.RefreshAuthenticateResponse)
	if refreshResponse.AccountId != guestResponse.AccountId || refreshResponse.RefreshToken == secondResponse.RefreshToken {
		t.Fatalf("refresh token was not rotated: %+v", refreshResponse)
	}

	if _, err := component.refreshAuthenticate(context.Background(), streaming.Peer{}, &internalpb.InternalEnvelope{Payload: refreshPayload}); !errors.Is(err, models.ErrAccountNotFound) {
		t.Fatalf("old refresh token error = %v, want account not found", err)
	}
}

func TestAuthComponentPasswordAuthenticate(t *testing.T) {
	repo := &memoryAccountRepository{}
	component := NewAuthComponent(repo, time.Hour)
	component.now = func() time.Time { return time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC) }

	request, err := proto.Marshal(&internalpb.PasswordAuthenticateRequest{Username: "player-one", Password: "correct-password", InstallId: "install-1"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := component.passwordAuthenticate(context.Background(), streaming.Peer{}, &internalpb.InternalEnvelope{Payload: request})
	if err != nil {
		t.Fatalf("passwordAuthenticate() error = %v", err)
	}
	response := result.Message.(*internalpb.PasswordAuthenticateResponse)
	if response.AccountId == "" || response.RefreshToken == "" {
		t.Fatalf("unexpected password response: %+v", response)
	}

	wrongPassword, err := proto.Marshal(&internalpb.PasswordAuthenticateRequest{Username: "player-one", Password: "wrong-password", InstallId: "install-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := component.passwordAuthenticate(context.Background(), streaming.Peer{}, &internalpb.InternalEnvelope{Payload: wrongPassword}); err == nil {
		t.Fatal("passwordAuthenticate() accepted an invalid password")
	}
}

func TestAuthComponentRegistersHandlers(t *testing.T) {
	component := NewAuthComponent(&memoryAccountRepository{}, time.Hour)
	router := streaming.NewRouter()
	component.RegisterInternal(router)

	request, err := proto.Marshal(&internalpb.GuestAuthenticateRequest{InstallId: "install-1"})
	if err != nil {
		t.Fatal(err)
	}
	var response *internalpb.InternalEnvelope
	peer := streaming.Peer{ServiceType: internalpb.ServiceType_SERVICE_TYPE_GATEWAY, Connection: streaming.NewConnection(func(envelope *internalpb.InternalEnvelope) error {
		response = envelope
		return nil
	})}
	if err := router.Handle(context.Background(), peer, &internalpb.InternalEnvelope{Kind: internalpb.EnvelopeKind_ENVELOPE_KIND_REQUEST, TargetService: internalpb.ServiceType_SERVICE_TYPE_USERCENTER, MessageId: uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_GUEST_AUTHENTICATE_REQUEST), Payload: request}); err != nil {
		t.Fatalf("router.Handle() error = %v", err)
	}
	if response == nil || response.MessageId != uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_GUEST_AUTHENTICATE_RESPONSE) {
		t.Fatalf("unexpected handler response: %+v", response)
	}
}

var _ repository.IAccountRepository = (*memoryAccountRepository)(nil)
