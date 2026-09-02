package components

import (
	"context"
	"errors"
	"testing"
	"time"

	"server/services/battle/domain"
)

func TestRoomManagerPersistsAuthoritativeMutationsAndRestores(t *testing.T) {
	repository := &memoryRoomSnapshotRepository{rooms: make(map[uint64]*domain.Room)}
	manager := NewRoomManager(repository)
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	manager.now = func() time.Time { return now }
	room, err := domain.NewRoom(100, []domain.PlayerState{{PlayerID: "player-1", HP: 100}}, now)
	if err != nil {
		t.Fatalf("new room: %v", err)
	}
	if err := manager.Create(context.Background(), room); err != nil {
		t.Fatalf("create room: %v", err)
	}
	if repository.saveCount != 1 {
		t.Fatalf("initial snapshot saves = %d, want 1", repository.saveCount)
	}
	if err := manager.Mutate(context.Background(), room.ID, func(room *domain.Room) error {
		return room.UpdatePlayer(domain.PlayerState{PlayerID: "player-1", HP: 80, PositionX: 10})
	}); err != nil {
		t.Fatalf("mutate room: %v", err)
	}
	if repository.saveCount != 2 {
		t.Fatalf("updated snapshot saves = %d, want 2", repository.saveCount)
	}
	restored, err := manager.RestoreByPlayerID(context.Background(), "player-1")
	if err != nil {
		t.Fatalf("restore room: %v", err)
	}
	if restored.Tick != 1 || restored.StateVersion != 2 || restored.Status != domain.RoomStatusRunning || restored.Players["player-1"].HP != 80 {
		t.Fatalf("unexpected restored room: %+v", restored)
	}
}

func TestRoomManagerRollsBackMemoryWhenSnapshotFails(t *testing.T) {
	repository := &memoryRoomSnapshotRepository{rooms: make(map[uint64]*domain.Room), saveErr: errors.New("snapshot unavailable")}
	manager := NewRoomManager(repository)
	now := time.Now().UTC()
	room, err := domain.NewRoom(101, []domain.PlayerState{{PlayerID: "player-1", HP: 100}}, now)
	if err != nil {
		t.Fatalf("new room: %v", err)
	}
	if err := manager.Create(context.Background(), room); err == nil {
		t.Fatal("create room unexpectedly succeeded")
	}
	repository.saveErr = nil
	if err := manager.Create(context.Background(), room); err != nil {
		t.Fatalf("create room after repository recovery: %v", err)
	}
	repository.saveErr = errors.New("snapshot unavailable")
	if err := manager.Mutate(context.Background(), room.ID, func(room *domain.Room) error {
		return room.UpdatePlayer(domain.PlayerState{PlayerID: "player-1", HP: 1})
	}); err == nil {
		t.Fatal("mutate unexpectedly succeeded")
	}
	restored, err := manager.RestoreByPlayerID(context.Background(), "player-1")
	if err != nil {
		t.Fatalf("restore room after failed mutation: %v", err)
	}
	if restored.Tick != 0 || restored.StateVersion != 1 || restored.Players["player-1"].HP != 100 {
		t.Fatalf("room was not rolled back: %+v", restored)
	}
}

func TestRoomManagerRestoreModes(t *testing.T) {
	repository := &memoryRoomSnapshotRepository{rooms: make(map[uint64]*domain.Room)}
	manager := NewRoomManager(repository)
	now := time.Now().UTC()
	room, _ := domain.NewRoom(102, []domain.PlayerState{{PlayerID: "player-1", HP: 100}}, now)
	if err := manager.Create(context.Background(), room); err != nil {
		t.Fatal(err)
	}
	if err := manager.Mutate(context.Background(), room.ID, func(room *domain.Room) error {
		return room.UpdatePlayer(domain.PlayerState{PlayerID: "player-1", HP: 90})
	}); err != nil {
		t.Fatal(err)
	}
	result, err := manager.RestoreByPlayerIDSince(context.Background(), "player-1", 1, true)
	if err != nil || result.Mode != RestoreModeDelta || result.Delta == nil {
		t.Fatalf("expected delta, got %+v, err=%v", result, err)
	}
	result, err = manager.RestoreByPlayerIDSince(context.Background(), "player-1", 2, true)
	if err != nil || result.Mode != RestoreModeNoop {
		t.Fatalf("expected noop, got %+v, err=%v", result, err)
	}
	result, err = manager.RestoreByPlayerIDSince(context.Background(), "player-1", 1, false)
	if err != nil || result.Mode != RestoreModeFull {
		t.Fatalf("expected full, got %+v, err=%v", result, err)
	}
}

type memoryRoomSnapshotRepository struct {
	rooms     map[uint64]*domain.Room
	saveErr   error
	saveCount int
}

func (r *memoryRoomSnapshotRepository) EnsureIndexes(context.Context) error { return nil }

func (r *memoryRoomSnapshotRepository) Save(_ context.Context, room *domain.Room) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	r.saveCount++
	r.rooms[room.ID] = room.Clone()
	return nil
}

func (r *memoryRoomSnapshotRepository) FindByPlayerID(_ context.Context, playerID string) (*domain.Room, error) {
	for _, room := range r.rooms {
		if _, exists := room.Players[playerID]; exists {
			return room.Clone(), nil
		}
	}
	return nil, domain.ErrRoomNotFound
}
