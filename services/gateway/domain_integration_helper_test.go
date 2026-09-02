package gateway

import (
	"context"
	"errors"
	"sync"
	"time"

	"server/services/usercenter/domain"
)

type gatewayDomainStore struct {
	mu         sync.Mutex
	accounts   map[string]*domain.Account
	identities map[string]*domain.Identity
	tokens     map[string]*domain.RefreshToken
}

func newGatewayDomainStore() *gatewayDomainStore {
	return &gatewayDomainStore{
		accounts:   make(map[string]*domain.Account),
		identities: make(map[string]*domain.Identity),
		tokens:     make(map[string]*domain.RefreshToken),
	}
}

func (s *gatewayDomainStore) EnsureIndexes(context.Context) error {
	return nil
}

func (s *gatewayDomainStore) Create(ctx context.Context, account *domain.Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.accounts[account.ID]; exists {
		return errors.New("duplicate account")
	}
	copy := *account
	copy.PlayerIDs = append([]string(nil), account.PlayerIDs...)
	s.accounts[account.ID] = &copy
	return nil
}

func (s *gatewayDomainStore) FindByID(ctx context.Context, accountID string) (*domain.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	account := s.accounts[accountID]
	if account == nil {
		return nil, domain.ErrAccountNotFound
	}
	copy := *account
	copy.PlayerIDs = append([]string(nil), account.PlayerIDs...)
	return &copy, nil
}

func (s *gatewayDomainStore) LinkPlayer(ctx context.Context, accountID, playerID string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	account := s.accounts[accountID]
	if account == nil {
		return domain.ErrAccountNotFound
	}
	for _, existing := range account.PlayerIDs {
		if existing == playerID {
			return nil
		}
	}
	account.PlayerIDs = append(account.PlayerIDs, playerID)
	account.UpdatedAt = now
	return nil
}

func (s *gatewayDomainStore) CreateIdentity(ctx context.Context, identity *domain.Identity) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := string(identity.Provider) + "\x00" + identity.Subject
	if _, exists := s.identities[key]; exists {
		return errors.New("duplicate identity")
	}
	copy := *identity
	s.identities[key] = &copy
	return nil
}

func (s *gatewayDomainStore) Find(ctx context.Context, provider domain.AuthProvider, subject string) (*domain.Identity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	identity := s.identities[string(provider)+"\x00"+subject]
	if identity == nil {
		return nil, domain.ErrIdentityNotFound
	}
	copy := *identity
	return &copy, nil
}

func (s *gatewayDomainStore) CreateToken(ctx context.Context, token *domain.RefreshToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.accounts[token.AccountID]; !exists {
		return domain.ErrAccountNotFound
	}
	copy := *token
	s.tokens[token.ID] = &copy
	return nil
}

func (s *gatewayDomainStore) FindValid(ctx context.Context, tokenHash string, now time.Time) (*domain.RefreshToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, token := range s.tokens {
		if token.TokenHash == tokenHash && token.Validate(now) == nil {
			copy := *token
			return &copy, nil
		}
	}
	return nil, domain.ErrInvalidToken
}

func (s *gatewayDomainStore) Revoke(ctx context.Context, tokenID string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	token := s.tokens[tokenID]
	if token == nil {
		return domain.ErrInvalidToken
	}
	return token.Revoke(now)
}

func (s *gatewayDomainStore) Rotate(ctx context.Context, tokenID string, now time.Time, replacement *domain.RefreshToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	token := s.tokens[tokenID]
	if token == nil || token.Validate(now) != nil {
		return domain.ErrInvalidToken
	}
	if err := token.Revoke(now); err != nil {
		return err
	}
	copy := *replacement
	s.tokens[replacement.ID] = &copy
	return nil
}

type gatewayDomainUnitOfWork struct{}

func (gatewayDomainUnitOfWork) Execute(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

type gatewayIdentityStore struct {
	store *gatewayDomainStore
}

func (s *gatewayIdentityStore) EnsureIndexes(ctx context.Context) error {
	return nil
}

func (s *gatewayIdentityStore) Create(ctx context.Context, identity *domain.Identity) error {
	return s.store.CreateIdentity(ctx, identity)
}

func (s *gatewayIdentityStore) Find(ctx context.Context, provider domain.AuthProvider, subject string) (*domain.Identity, error) {
	return s.store.Find(ctx, provider, subject)
}

type gatewayTokenStore struct {
	store *gatewayDomainStore
}

func (s *gatewayTokenStore) EnsureIndexes(ctx context.Context) error {
	return nil
}

func (s *gatewayTokenStore) Create(ctx context.Context, token *domain.RefreshToken) error {
	return s.store.CreateToken(ctx, token)
}

func (s *gatewayTokenStore) FindValid(ctx context.Context, tokenHash string, now time.Time) (*domain.RefreshToken, error) {
	return s.store.FindValid(ctx, tokenHash, now)
}

func (s *gatewayTokenStore) Revoke(ctx context.Context, tokenID string, now time.Time) error {
	return s.store.Revoke(ctx, tokenID, now)
}

func (s *gatewayTokenStore) Rotate(ctx context.Context, tokenID string, now time.Time, replacement *domain.RefreshToken) error {
	return s.store.Rotate(ctx, tokenID, now, replacement)
}
