package components

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"server/common/streaming"
	internalpb "server/proto/gen/internalpb"
	"server/services/usercenter/domain"
	"server/services/usercenter/repository"
)

func TestDomainAuthComponentCreatesAndRotatesAuthentication(t *testing.T) {
	store := newDomainAuthMemory()
	component := NewDomainAuthComponent(store.accounts, store.identities, store.tokens, domainAuthUnitOfWork{}, time.Hour)
	clock := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	component.now = func() time.Time { return clock }

	guestPayload, _ := proto.Marshal(&internalpb.GuestAuthenticateRequest{InstallId: "install-1"})
	result, err := component.guestAuthenticate(context.Background(), streaming.Peer{}, &internalpb.InternalEnvelope{Payload: guestPayload})
	if err != nil {
		t.Fatalf("guest authentication: %v", err)
	}
	guest := result.Message.(*internalpb.GuestAuthenticateResponse)
	if !guest.Created || guest.AccountId == "" || guest.RefreshToken == "" {
		t.Fatalf("unexpected guest response: %s", guest)
	}

	result, err = component.guestAuthenticate(context.Background(), streaming.Peer{}, &internalpb.InternalEnvelope{Payload: guestPayload})
	if err != nil {
		t.Fatalf("second guest authentication: %v", err)
	}
	secondGuest := result.Message.(*internalpb.GuestAuthenticateResponse)
	if secondGuest.Created || secondGuest.AccountId != guest.AccountId {
		t.Fatalf("guest account was not reused: %s", secondGuest)
	}

	refreshPayload, _ := proto.Marshal(&internalpb.RefreshAuthenticateRequest{RefreshToken: guest.RefreshToken, InstallId: "install-1"})
	result, err = component.refreshAuthenticate(context.Background(), streaming.Peer{}, &internalpb.InternalEnvelope{Payload: refreshPayload})
	if err != nil {
		t.Fatalf("refresh authentication: %v", err)
	}
	refresh := result.Message.(*internalpb.RefreshAuthenticateResponse)
	if refresh.AccountId != guest.AccountId || refresh.RefreshToken == guest.RefreshToken {
		t.Fatalf("refresh token was not rotated: %s", refresh)
	}

	if _, err := component.refreshAuthenticate(context.Background(), streaming.Peer{}, &internalpb.InternalEnvelope{Payload: refreshPayload}); !errors.Is(err, domain.ErrInvalidToken) {
		t.Fatalf("old refresh token error = %v", err)
	}
}

func TestDomainAuthComponentPasswordValidation(t *testing.T) {
	store := newDomainAuthMemory()
	component := NewDomainAuthComponent(store.accounts, store.identities, store.tokens, domainAuthUnitOfWork{}, time.Hour)

	payload, _ := proto.Marshal(&internalpb.PasswordAuthenticateRequest{Username: "user-1", Password: "correct-password", InstallId: "install-1"})
	result, err := component.passwordAuthenticate(context.Background(), streaming.Peer{}, &internalpb.InternalEnvelope{Payload: payload})
	if err != nil {
		t.Fatalf("password authentication: %v", err)
	}
	if result.Message.(*internalpb.PasswordAuthenticateResponse).AccountId == "" {
		t.Fatal("password authentication returned no account")
	}

	wrongPayload, _ := proto.Marshal(&internalpb.PasswordAuthenticateRequest{Username: "user-1", Password: "wrong-password", InstallId: "install-1"})
	if _, err := component.passwordAuthenticate(context.Background(), streaming.Peer{}, &internalpb.InternalEnvelope{Payload: wrongPayload}); err == nil {
		t.Fatal("wrong password was accepted")
	}
}

func TestDomainAuthComponentIdempotencyReplayAndConflict(t *testing.T) {
	store := newDomainAuthMemory()
	idempotency := &domainIdempotencyMemory{values: map[string]*domain.IdempotencyRecord{}}
	component := NewDomainAuthComponent(store.accounts, store.identities, store.tokens, domainAuthUnitOfWork{}, time.Hour, idempotency)
	clock := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	component.now = func() time.Time { return clock }

	payload, _ := proto.Marshal(&internalpb.GuestAuthenticateRequest{InstallId: "install-1", IdempotencyKey: "request-1"})
	envelope := &internalpb.InternalEnvelope{Payload: payload}
	first, err := component.guestAuthenticate(context.Background(), streaming.Peer{}, envelope)
	if err != nil {
		t.Fatalf("first idempotent authentication: %v", err)
	}
	second, err := component.guestAuthenticate(context.Background(), streaming.Peer{}, envelope)
	if err != nil {
		t.Fatalf("replayed idempotent authentication: %v", err)
	}
	firstResponse := first.Message.(*internalpb.GuestAuthenticateResponse)
	secondResponse := second.Message.(*internalpb.GuestAuthenticateResponse)
	if firstResponse.AccountId != secondResponse.AccountId || firstResponse.RefreshToken != secondResponse.RefreshToken || !firstResponse.Created || !secondResponse.Created {
		t.Fatalf("replay returned a different response: first=%s second=%s", firstResponse, secondResponse)
	}
	if len(idempotency.values) != 1 || len(store.accounts.values) != 1 {
		t.Fatalf("idempotency did not prevent duplicate creation: records=%d accounts=%d", len(idempotency.values), len(store.accounts.values))
	}

	conflictingPayload, _ := proto.Marshal(&internalpb.GuestAuthenticateRequest{InstallId: "install-2", IdempotencyKey: "request-1"})
	if _, err := component.guestAuthenticate(context.Background(), streaming.Peer{}, &internalpb.InternalEnvelope{Payload: conflictingPayload}); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("conflicting idempotency request error = %v", err)
	}

	refreshPayload, _ := proto.Marshal(&internalpb.RefreshAuthenticateRequest{RefreshToken: firstResponse.RefreshToken, InstallId: "install-1", IdempotencyKey: "refresh-1"})
	firstRefresh, err := component.refreshAuthenticate(context.Background(), streaming.Peer{}, &internalpb.InternalEnvelope{Payload: refreshPayload})
	if err != nil {
		t.Fatalf("first idempotent refresh: %v", err)
	}
	secondRefresh, err := component.refreshAuthenticate(context.Background(), streaming.Peer{}, &internalpb.InternalEnvelope{Payload: refreshPayload})
	if err != nil {
		t.Fatalf("replayed idempotent refresh: %v", err)
	}
	firstRefreshResponse := firstRefresh.Message.(*internalpb.RefreshAuthenticateResponse)
	secondRefreshResponse := secondRefresh.Message.(*internalpb.RefreshAuthenticateResponse)
	if firstRefreshResponse.RefreshToken != secondRefreshResponse.RefreshToken {
		t.Fatalf("refresh replay returned a different token: first=%q second=%q", firstRefreshResponse.RefreshToken, secondRefreshResponse.RefreshToken)
	}
}

func TestDomainAuthComponentSerializesConcurrentIdempotentRequests(t *testing.T) {
	store := newDomainAuthMemory()
	idempotency := &domainIdempotencyMemory{values: map[string]*domain.IdempotencyRecord{}}
	component := NewDomainAuthComponent(store.accounts, store.identities, store.tokens, domainAuthUnitOfWork{}, time.Hour, idempotency)
	payload, _ := proto.Marshal(&internalpb.GuestAuthenticateRequest{InstallId: "install-concurrent", IdempotencyKey: "concurrent-1"})

	const callers = 16
	results := make(chan *internalpb.GuestAuthenticateResponse, callers)
	errorsCh := make(chan error, callers)
	var group sync.WaitGroup
	for i := 0; i < callers; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			result, err := component.guestAuthenticate(context.Background(), streaming.Peer{}, &internalpb.InternalEnvelope{Payload: payload})
			if err != nil {
				errorsCh <- err
				return
			}
			results <- result.Message.(*internalpb.GuestAuthenticateResponse)
		}()
	}
	group.Wait()
	close(results)
	close(errorsCh)
	for err := range errorsCh {
		t.Fatalf("concurrent idempotent authentication: %v", err)
	}
	var accountID, refreshToken string
	for response := range results {
		if accountID == "" {
			accountID, refreshToken = response.AccountId, response.RefreshToken
		}
		if response.AccountId != accountID || response.RefreshToken != refreshToken {
			t.Fatalf("concurrent response mismatch: %s", response)
		}
	}
	if len(store.accounts.values) != 1 || len(idempotency.values) != 1 {
		t.Fatalf("concurrent request created duplicate state: accounts=%d records=%d", len(store.accounts.values), len(idempotency.values))
	}
}

func TestDomainAuthComponentRenewsLongIdempotencyLease(t *testing.T) {
	store := newDomainAuthMemory()
	idempotency := &domainIdempotencyMemory{values: map[string]*domain.IdempotencyRecord{}}
	component := NewDomainAuthComponent(store.accounts, store.identities, store.tokens, domainAuthUnitOfWork{}, time.Hour, idempotency).WithIdempotencyLease(20*time.Millisecond, 5*time.Millisecond)
	now := time.Now().UTC()
	pending := &domain.IdempotencyRecord{Key: "lease-key", Operation: "test", RequestHash: "hash", State: domain.IdempotencyStatePending, ReservationID: "reservation", LeaseUntil: now.Add(20 * time.Millisecond), CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	idempotency.values[pending.Key] = pending
	result, err := component.executeReserved(context.Background(), pending.Key, pending.ReservationID, func(ctx context.Context) (*streaming.MessageResult, error) {
		select {
		case <-time.After(35 * time.Millisecond):
			return &streaming.MessageResult{MessageID: 1, Message: &internalpb.GuestAuthenticateResponse{AccountId: "account"}}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	if err != nil || result == nil {
		t.Fatalf("long idempotent execution failed: result=%v err=%v", result, err)
	}
	if !idempotency.values[pending.Key].IsCompleted() {
		t.Fatal("idempotency record was not completed after lease renewal")
	}
}

func TestDomainAuthComponentRecoversExpiredReservations(t *testing.T) {
	idempotency := &domainIdempotencyMemory{values: map[string]*domain.IdempotencyRecord{
		"expired": {Key: "expired", Operation: "test", RequestHash: "hash", State: domain.IdempotencyStatePending, ReservationID: "reservation", LeaseUntil: time.Now().UTC().Add(-time.Minute), CreatedAt: time.Now().UTC().Add(-time.Hour), ExpiresAt: time.Now().UTC().Add(time.Hour)},
	}}
	deleted, err := idempotency.RecoverExpired(context.Background(), time.Now().UTC())
	if err != nil || deleted != 1 {
		t.Fatalf("recover expired reservations: deleted=%d err=%v", deleted, err)
	}
	if _, exists := idempotency.values["expired"]; exists {
		t.Fatal("expired reservation was not removed")
	}
}

type domainAuthMemory struct {
	accounts   *domainAccountMemory
	identities *domainIdentityMemory
	tokens     *domainTokenMemory
}

func newDomainAuthMemory() *domainAuthMemory {
	return &domainAuthMemory{
		accounts:   &domainAccountMemory{values: map[string]*domain.Account{}},
		identities: &domainIdentityMemory{values: map[string]*domain.Identity{}},
		tokens:     &domainTokenMemory{values: map[string]*domain.RefreshToken{}},
	}
}

type domainAccountMemory struct{ values map[string]*domain.Account }

func (m *domainAccountMemory) EnsureIndexes(context.Context) error { return nil }
func (m *domainAccountMemory) Create(_ context.Context, item *domain.Account) error {
	if _, exists := m.values[item.ID]; exists {
		return errors.New("duplicate account")
	}
	copy := *item
	m.values[item.ID] = &copy
	return nil
}
func (m *domainAccountMemory) FindByID(_ context.Context, id string) (*domain.Account, error) {
	return m.values[id], nil
}
func (m *domainAccountMemory) LinkPlayer(context.Context, string, string, time.Time) error {
	return nil
}

type domainIdentityMemory struct{ values map[string]*domain.Identity }

func (m *domainIdentityMemory) EnsureIndexes(context.Context) error { return nil }
func (m *domainIdentityMemory) Create(_ context.Context, item *domain.Identity) error {
	key := string(item.Provider) + "\x00" + item.Subject
	if _, exists := m.values[key]; exists {
		return errors.New("duplicate identity")
	}
	copy := *item
	m.values[key] = &copy
	return nil
}
func (m *domainIdentityMemory) Find(_ context.Context, provider domain.AuthProvider, subject string) (*domain.Identity, error) {
	identity := m.values[string(provider)+"\x00"+subject]
	if identity == nil {
		return nil, domain.ErrIdentityNotFound
	}
	return identity, nil
}

type domainTokenMemory struct {
	values map[string]*domain.RefreshToken
}

type domainIdempotencyMemory struct {
	values map[string]*domain.IdempotencyRecord
}

func (m *domainIdempotencyMemory) EnsureIndexes(context.Context) error { return nil }

func (m *domainIdempotencyMemory) Find(_ context.Context, key string, now time.Time) (*domain.IdempotencyRecord, error) {
	record := m.values[key]
	if record == nil || record.IsExpired(now) {
		return nil, repository.ErrIdempotencyNotFound
	}
	copy := *record
	copy.Response = append([]byte(nil), record.Response...)
	return &copy, nil
}

func (m *domainIdempotencyMemory) Create(_ context.Context, record *domain.IdempotencyRecord) error {
	if _, exists := m.values[record.Key]; exists {
		return domain.ErrIdempotencyConflict
	}
	copy := *record
	copy.Response = append([]byte(nil), record.Response...)
	m.values[record.Key] = &copy
	return nil
}

func (m *domainIdempotencyMemory) Reserve(_ context.Context, record *domain.IdempotencyRecord, now time.Time) (*domain.IdempotencyRecord, bool, error) {
	if existing := m.values[record.Key]; existing != nil && !existing.IsExpired(now) {
		if existing.Operation != record.Operation || existing.RequestHash != record.RequestHash {
			return existing, false, domain.ErrIdempotencyConflict
		}
		if existing.IsCompleted() || existing.LeaseUntil.After(now) {
			return existing, false, nil
		}
	}
	copy := *record
	copy.Response = append([]byte(nil), record.Response...)
	m.values[record.Key] = &copy
	return &copy, true, nil
}

func (m *domainIdempotencyMemory) Complete(_ context.Context, key, reservationID string, response []byte, _ time.Time) error {
	record := m.values[key]
	if record == nil || record.ReservationID != reservationID {
		return domain.ErrIdempotencyConflict
	}
	record.State = domain.IdempotencyStateCompleted
	record.Response = append([]byte(nil), response...)
	record.LeaseUntil = time.Time{}
	return nil
}

func (m *domainIdempotencyMemory) Release(_ context.Context, key, reservationID string) error {
	record := m.values[key]
	if record != nil && record.ReservationID == reservationID && !record.IsCompleted() {
		delete(m.values, key)
	}
	return nil
}

func (m *domainIdempotencyMemory) Renew(_ context.Context, key, reservationID string, leaseUntil, now time.Time) error {
	record := m.values[key]
	if record == nil || record.ReservationID != reservationID || record.IsCompleted() || !now.Before(record.ExpiresAt) {
		return domain.ErrIdempotencyLeaseLost
	}
	record.LeaseUntil = leaseUntil
	return nil
}

func (m *domainIdempotencyMemory) RecoverExpired(_ context.Context, now time.Time) (int64, error) {
	var deleted int64
	for key, record := range m.values {
		if !record.IsCompleted() && (!now.Before(record.LeaseUntil) || record.IsExpired(now)) {
			delete(m.values, key)
			deleted++
		}
	}
	return deleted, nil
}

func (m *domainTokenMemory) EnsureIndexes(context.Context) error { return nil }
func (m *domainTokenMemory) Create(_ context.Context, item *domain.RefreshToken) error {
	copy := *item
	m.values[item.ID] = &copy
	return nil
}
func (m *domainTokenMemory) FindValid(_ context.Context, hash string, now time.Time) (*domain.RefreshToken, error) {
	for _, token := range m.values {
		if token.TokenHash == hash && token.Validate(now) == nil {
			copy := *token
			return &copy, nil
		}
	}
	return nil, domain.ErrInvalidToken
}

func (m *domainTokenMemory) Revoke(_ context.Context, id string, now time.Time) error {
	token := m.values[id]
	if token == nil {
		return domain.ErrInvalidToken
	}
	return token.Revoke(now)
}

func (m *domainTokenMemory) Rotate(_ context.Context, id string, now time.Time, replacement *domain.RefreshToken) error {
	if err := m.Revoke(context.Background(), id, now); err != nil {
		return err
	}
	copy := *replacement
	m.values[replacement.ID] = &copy
	return nil
}

type domainAuthUnitOfWork struct{}

func (domainAuthUnitOfWork) Execute(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

var _ repository.AccountRepository = (*domainAccountMemory)(nil)
var _ repository.IdentityRepository = (*domainIdentityMemory)(nil)
var _ repository.RefreshTokenRepository = (*domainTokenMemory)(nil)
var _ repository.IdempotencyRepository = (*domainIdempotencyMemory)(nil)
var _ repository.IdempotencyCoordinator = (*domainIdempotencyMemory)(nil)
