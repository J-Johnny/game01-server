package session

import (
	"context"
	"sync"
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

func TestLifecycleEventsUseConnectionEpochForOrdering(t *testing.T) {
	store := NewMemoryStore()
	manager := NewManager(store, "gateway-1", time.Hour, time.Minute)
	var mu sync.Mutex
	events := make([]LifecycleEvent, 0, 4)
	manager.SetLifecycleHandler(func(_ context.Context, event LifecycleEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
	})
	now := time.Now().UTC()
	created, err := manager.Create(context.Background(), "account-1", "connection-1", now, "player-1")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := manager.DisconnectConnection(context.Background(), created.Record.SessionID, "connection-1", now.Add(time.Second), created.Record.ConnectionEpoch); err != nil {
		t.Fatalf("disconnect session: %v", err)
	}
	resumed, err := manager.Resume(context.Background(), created.Record.SessionID, created.ResumeToken, "connection-2", now.Add(2*time.Second))
	if err != nil {
		t.Fatalf("resume session: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 3 {
		t.Fatalf("expected connected, disconnected, resumed events, got %+v", events)
	}
	if events[0].Type != LifecycleConnected || events[0].ConnectionEpoch != 1 || events[1].Type != LifecycleDisconnected || events[1].ConnectionEpoch != 1 || events[2].Type != LifecycleResumed || events[2].ConnectionEpoch != resumed.Record.ConnectionEpoch {
		t.Fatalf("unexpected lifecycle events: %+v", events)
	}
	if events[2].EventID != created.Record.SessionID+":2:resumed" {
		t.Fatalf("unexpected resumed event ID: %s", events[2].EventID)
	}
}

func TestAccountPreemptionIncludesExactPreviousConnectionEpoch(t *testing.T) {
	store := NewMemoryStore()
	manager := NewManager(store, "gateway-1", time.Hour, time.Minute)
	var preempted Record
	manager.SetPreemptHandler(func(record Record) {
		preempted = record
	})
	now := time.Now().UTC()
	first, err := manager.Create(context.Background(), "account-1", "connection-1", now)
	if err != nil {
		t.Fatalf("create first session: %v", err)
	}
	if _, err := manager.Create(context.Background(), "account-1", "connection-2", now.Add(time.Second)); err != nil {
		t.Fatalf("create second session: %v", err)
	}
	if preempted.SessionID != first.Record.SessionID || preempted.ConnectionID != "connection-1" || preempted.ConnectionEpoch != 1 {
		t.Fatalf("unexpected preempted connection: %+v", preempted)
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
