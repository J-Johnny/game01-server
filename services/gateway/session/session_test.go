package session

import (
	"context"
	"testing"
	"time"
)

func TestDisconnectConnectionEnablesResumeWithinGracePeriod(t *testing.T) {
	store := NewMemoryStore()
	manager := NewManager(store, "gateway-1", time.Hour, time.Minute)
	now := time.Now().UTC()
	created, err := manager.Create(context.Background(), "account-1", "connection-1", now)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	if err := manager.DisconnectConnection(context.Background(), created.Record.SessionID, "connection-1", now); err != nil {
		t.Fatalf("disconnect session: %v", err)
	}
	reconnecting, err := store.Get(context.Background(), created.Record.SessionID)
	if err != nil {
		t.Fatalf("get reconnecting session: %v", err)
	}
	if reconnecting.State != StateReconnecting || reconnecting.ConnectionID != "" || !reconnecting.ExpireAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("unexpected reconnecting session: %+v", reconnecting)
	}

	resumed, err := manager.Resume(context.Background(), created.Record.SessionID, created.ResumeToken, "connection-2", now.Add(time.Second))
	if err != nil {
		t.Fatalf("resume session: %v", err)
	}
	if resumed.Record.ConnectionID != "connection-2" || resumed.Record.ConnectionEpoch != 2 || resumed.ResumeToken == created.ResumeToken {
		t.Fatalf("unexpected resumed session: %+v", resumed)
	}
}

func TestOldConnectionCannotDisconnectResumedSession(t *testing.T) {
	store := NewMemoryStore()
	manager := NewManager(store, "gateway-1", time.Hour, time.Minute)
	now := time.Now().UTC()
	created, err := manager.Create(context.Background(), "account-1", "connection-old", now)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	resumed, err := manager.Resume(context.Background(), created.Record.SessionID, created.ResumeToken, "connection-new", now.Add(time.Second))
	if err != nil {
		t.Fatalf("resume session: %v", err)
	}

	if err := manager.DisconnectConnection(context.Background(), created.Record.SessionID, "connection-old", now.Add(2*time.Second)); err != nil {
		t.Fatalf("disconnect old connection: %v", err)
	}
	current, err := store.Get(context.Background(), created.Record.SessionID)
	if err != nil {
		t.Fatalf("get current session: %v", err)
	}
	if current.ConnectionID != resumed.Record.ConnectionID || current.State != StateAuthenticated {
		t.Fatalf("old connection changed current session: %+v", current)
	}
}
