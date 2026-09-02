package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrNotFound      = errors.New("session not found")
	ErrInvalidToken  = errors.New("invalid resume token")
	ErrSessionExpiry = errors.New("session expired")
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
}

func NewManager(store Store, gatewayID string, sessionTTL, reconnectGrace time.Duration) *Manager {
	return &Manager{
		store:          store,
		gatewayID:      gatewayID,
		sessionTTL:     sessionTTL,
		reconnectGrace: reconnectGrace,
		connections:    make(map[string]string),
	}
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
	m.mu.Lock()
	m.connections[connectionID] = sid
	m.mu.Unlock()
	return Created{Record: r, ResumeToken: token}, nil
}

func (m *Manager) Disconnect(ctx context.Context, sessionID string, now time.Time) error {
	r, err := m.store.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	r.State = StateReconnecting
	r.ConnectionID = ""
	r.ExpireAt = now.Add(m.reconnectGrace)
	if err := m.store.Save(ctx, r); err != nil {
		return err
	}
	m.mu.Lock()
	for conn, sid := range m.connections {
		if sid == sessionID {
			delete(m.connections, conn)
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
		r.State = StateExpired
		_ = m.store.Save(ctx, r)
		return Created{}, ErrSessionExpiry
	}
	if hash(token) != r.ResumeTokenHash {
		return Created{}, ErrInvalidToken
	}
	nextToken, err := randomID()
	if err != nil {
		return Created{}, err
	}
	r.State, r.ConnectionID = StateAuthenticated, connectionID
	r.ConnectionEpoch++
	r.ResumeTokenHash = hash(nextToken)
	r.ExpireAt = now.Add(m.sessionTTL)
	if err := m.store.Save(ctx, r); err != nil {
		return Created{}, err
	}
	m.mu.Lock()
	m.connections[connectionID] = sessionID
	m.mu.Unlock()
	return Created{Record: r, ResumeToken: nextToken}, nil
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
