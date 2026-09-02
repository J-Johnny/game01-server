package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"
)

var (
	ErrNotFound        = errors.New("session not found")
	ErrInvalidToken    = errors.New("invalid resume token")
	ErrSessionExpiry   = errors.New("session expired")
	ErrSessionConflict = errors.New("session was changed concurrently")
)

type State string

const (
	StateAuthenticated State = "authenticated"
	StateActive        State = "active"
	StateReconnecting  State = "reconnecting"
	StateExpired       State = "expired"
)

type Record struct {
	SessionID         string    `json:"session_id"`
	AccountID         string    `json:"account_id"`
	PlayerID          string    `json:"player_id"`
	GatewayInstanceID string    `json:"gateway_instance_id"`
	ConnectionID      string    `json:"connection_id"`
	ConnectionEpoch   uint64    `json:"connection_epoch"`
	State             State     `json:"state"`
	ResumeTokenHash   string    `json:"resume_token_hash"`
	ExpireAt          time.Time `json:"expire_at"`
}

type Store interface {
	Create(context.Context, Record) error
	Get(context.Context, string) (Record, error)
	Save(context.Context, Record) error
	Delete(context.Context, string) error
}

type CompareAndSwapStore interface {
	CompareAndSwap(context.Context, Record, Record) (bool, error)
}

type AccountSessionStore interface {
	ClaimAccount(context.Context, string, Record) (Record, bool, error)
	ReleaseAccount(context.Context, string, string) error
}

type MemoryStore struct {
	mu      sync.RWMutex
	records map[string]Record
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		records: make(map[string]Record),
	}
}

func (s *MemoryStore) Create(_ context.Context, r Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[r.SessionID]; ok {
		return fmt.Errorf("session %s already exists", r.SessionID)
	}
	s.records[r.SessionID] = r
	return nil
}

func (s *MemoryStore) ClaimAccount(_ context.Context, accountID string, record Record) (Record, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var previous Record
	for _, candidate := range s.records {
		if candidate.AccountID == accountID && candidate.ConnectionID != "" {
			previous = candidate
			break
		}
	}
	_ = record
	return previous, previous.ConnectionID != "", nil
}

func (s *MemoryStore) ReleaseAccount(_ context.Context, accountID, connectionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, candidate := range s.records {
		if candidate.AccountID == accountID && candidate.ConnectionID == connectionID {
			candidate.ConnectionID = ""
			s.records[id] = candidate
		}
	}
	return nil
}

func (s *MemoryStore) Get(_ context.Context, id string) (Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.records[id]
	if !ok {
		return Record{}, ErrNotFound
	}
	return r, nil
}

func (s *MemoryStore) Save(_ context.Context, r Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[r.SessionID]; !ok {
		return ErrNotFound
	}
	s.records[r.SessionID] = r
	return nil
}

func (s *MemoryStore) CompareAndSwap(_ context.Context, expected, updated Record) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.records[expected.SessionID]
	if !ok {
		return false, ErrNotFound
	}
	if !reflect.DeepEqual(current, expected) {
		return false, nil
	}
	s.records[updated.SessionID] = updated
	return true, nil
}

func (s *MemoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, id)
	return nil
}

type Manager struct {
	store                      Store
	gatewayID                  string
	sessionTTL, reconnectGrace time.Duration
	mu                         sync.Mutex
	connections                map[string]string
	accountConnections         map[string]string
	preempt                    func(Record)
}

func NewManager(store Store, gatewayID string, sessionTTL, reconnectGrace time.Duration) *Manager {
	return &Manager{
		store:              store,
		gatewayID:          gatewayID,
		sessionTTL:         sessionTTL,
		reconnectGrace:     reconnectGrace,
		connections:        make(map[string]string),
		accountConnections: make(map[string]string),
	}
}

func (m *Manager) SetPreemptHandler(handler func(Record)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.preempt = handler
}

type Created struct {
	Record      Record
	ResumeToken string
}

func (m *Manager) Create(ctx context.Context, accountID string, connectionID string, now time.Time) (Created, error) {
	sid, err := randomID()
	if err != nil {
		return Created{}, err
	}
	token, err := randomID()
	if err != nil {
		return Created{}, err
	}
	r := Record{SessionID: sid, AccountID: accountID, GatewayInstanceID: m.gatewayID, ConnectionID: connectionID, ConnectionEpoch: 1, State: StateAuthenticated, ResumeTokenHash: hash(token), ExpireAt: now.Add(m.sessionTTL)}
	if err := m.store.Create(ctx, r); err != nil {
		return Created{}, err
	}
	var previousRecord Record
	var previous bool
	if accountStore, ok := m.store.(AccountSessionStore); ok {
		previousRecord, previous, err = accountStore.ClaimAccount(ctx, accountID, r)
		if err != nil {
			return Created{}, err
		}
	}
	m.mu.Lock()
	if !previous {
		previousID := m.accountConnections[accountID]
		previous = previousID != ""
		previousRecord = Record{AccountID: accountID, ConnectionID: previousID}
	}
	m.accountConnections[accountID] = connectionID
	m.connections[connectionID] = sid
	preempt := m.preempt
	m.mu.Unlock()
	if previous && previousRecord.ConnectionID != "" && previousRecord.ConnectionID != connectionID && preempt != nil {
		preempt(previousRecord)
	}
	return Created{Record: r, ResumeToken: token}, nil
}

func (m *Manager) Disconnect(ctx context.Context, sessionID string, now time.Time) error {
	return m.disconnect(ctx, sessionID, "", now)
}

func (m *Manager) DisconnectConnection(ctx context.Context, sessionID, connectionID string, now time.Time, epochs ...uint64) error {
	return m.disconnect(ctx, sessionID, connectionID, now, epochs...)
}

func (m *Manager) disconnect(ctx context.Context, sessionID, connectionID string, now time.Time, epochs ...uint64) error {
	r, err := m.store.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	if connectionID != "" && r.ConnectionID != connectionID {
		return nil
	}
	if len(epochs) > 0 && epochs[0] != 0 && r.ConnectionEpoch != epochs[0] {
		return nil
	}
	expected := r
	r.State = StateReconnecting
	r.ConnectionID = ""
	r.ExpireAt = now.Add(m.reconnectGrace)
	if err := m.saveVersion(ctx, expected, r); err != nil {
		return err
	}
	if accountStore, ok := m.store.(AccountSessionStore); ok {
		if err := accountStore.ReleaseAccount(ctx, r.AccountID, connectionID); err != nil {
			return err
		}
	}
	m.mu.Lock()
	for conn, sid := range m.connections {
		if sid == sessionID {
			delete(m.connections, conn)
			if m.accountConnections[r.AccountID] == conn {
				delete(m.accountConnections, r.AccountID)
			}
		}
	}
	m.mu.Unlock()
	return nil
}

func (m *Manager) Resume(ctx context.Context, sessionID, token, connectionID string, now time.Time) (Created, error) {
	r, err := m.store.Get(ctx, sessionID)
	if err != nil {
		return Created{}, err
	}
	if !now.Before(r.ExpireAt) {
		expected := r
		r.State = StateExpired
		_ = m.saveVersion(ctx, expected, r)
		return Created{}, ErrSessionExpiry
	}
	if hash(token) != r.ResumeTokenHash {
		return Created{}, ErrInvalidToken
	}
	nextToken, err := randomID()
	if err != nil {
		return Created{}, err
	}
	expected := r
	r.State, r.ConnectionID = StateAuthenticated, connectionID
	r.GatewayInstanceID = m.gatewayID
	r.ConnectionEpoch++
	r.ResumeTokenHash = hash(nextToken)
	r.ExpireAt = now.Add(m.sessionTTL)
	if ok, err := m.saveVersionResult(ctx, expected, r); err != nil {
		return Created{}, err
	} else if !ok {
		return Created{}, ErrSessionConflict
	}
	if accountStore, ok := m.store.(AccountSessionStore); ok {
		if _, _, err := accountStore.ClaimAccount(ctx, r.AccountID, r); err != nil {
			return Created{}, err
		}
	}
	m.mu.Lock()
	m.connections[connectionID] = sessionID
	m.mu.Unlock()
	return Created{Record: r, ResumeToken: nextToken}, nil
}

func (m *Manager) saveVersion(ctx context.Context, expected, updated Record) error {
	_, err := m.saveVersionResult(ctx, expected, updated)
	return err
}

func (m *Manager) saveVersionResult(ctx context.Context, expected, updated Record) (bool, error) {
	if atomicStore, ok := m.store.(CompareAndSwapStore); ok {
		return atomicStore.CompareAndSwap(ctx, expected, updated)
	}
	if err := m.store.Save(ctx, updated); err != nil {
		return false, err
	}
	return true, nil
}

func randomID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func hash(v string) string {
	h := sha256.Sum256([]byte(v))
	return hex.EncodeToString(h[:])
}
