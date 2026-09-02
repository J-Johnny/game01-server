package components

import (
	"context"
	"errors"
	"sync"
	"time"

	"server/services/battle/domain"
	"server/services/battle/repository"
)

type RoomManager struct {
	mu        sync.RWMutex
	rooms     map[uint64]*domain.Room
	deltas    map[uint64][]domain.RoomDelta
	deltaSize int
	snapshots repository.RoomSnapshotRepository
	now       func() time.Time
}

func NewRoomManager(snapshots repository.RoomSnapshotRepository) *RoomManager {
	return &RoomManager{rooms: make(map[uint64]*domain.Room), deltas: make(map[uint64][]domain.RoomDelta), deltaSize: 1024, snapshots: snapshots, now: time.Now}
}

func (m *RoomManager) EnsureIndexes(ctx context.Context) error {
	if m == nil || m.snapshots == nil {
		return errors.New("battle room snapshot repository is not configured")
	}
	return m.snapshots.EnsureIndexes(ctx)
}

func (m *RoomManager) Create(ctx context.Context, room *domain.Room) error {
	if m == nil || room == nil {
		return domain.ErrInvalidRoom
	}
	if err := room.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.rooms[room.ID]; exists {
		return domain.ErrDuplicateRoom
	}
	if err := m.saveLocked(ctx, room); err != nil {
		return err
	}
	m.rooms[room.ID] = room.Clone()
	m.deltas[room.ID] = nil
	return nil
}

func (m *RoomManager) Mutate(ctx context.Context, roomID uint64, mutate func(*domain.Room) error) error {
	if m == nil || roomID == 0 || mutate == nil {
		return domain.ErrInvalidRoom
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	room, exists := m.rooms[roomID]
	if !exists {
		return domain.ErrRoomNotFound
	}
	previous := room.Clone()
	if err := mutate(room); err != nil {
		return err
	}
	if err := room.AdvanceTick(m.now()); err != nil {
		*room = *previous
		return err
	}
	if err := m.saveLocked(ctx, room); err != nil {
		*room = *previous
		return err
	}
	m.recordDeltaLocked(previous, room)
	return nil
}

type RestoreResult struct {
	Room             *domain.Room
	Mode             RestoreMode
	BaseStateVersion uint64
	Delta            *domain.RoomDelta
}

type RestoreMode string

const (
	RestoreModeFull  RestoreMode = "full"
	RestoreModeDelta RestoreMode = "delta"
	RestoreModeNoop  RestoreMode = "noop"
)

func (m *RoomManager) RestoreByPlayerIDSince(ctx context.Context, playerID string, lastStateVersion uint64, allowIncremental bool) (RestoreResult, error) {
	room, err := m.RestoreByPlayerID(ctx, playerID)
	if err != nil {
		return RestoreResult{}, err
	}
	if lastStateVersion == room.StateVersion {
		return RestoreResult{Room: room, Mode: RestoreModeNoop, BaseStateVersion: lastStateVersion}, nil
	}
	if allowIncremental && lastStateVersion < room.StateVersion {
		m.mu.RLock()
		deltas := append([]domain.RoomDelta(nil), m.deltas[room.ID]...)
		m.mu.RUnlock()
		if delta, ok := composeDeltas(room.ID, deltas, lastStateVersion, room.StateVersion); ok {
			return RestoreResult{Room: room, Mode: RestoreModeDelta, BaseStateVersion: lastStateVersion, Delta: &delta}, nil
		}
	}
	return RestoreResult{Room: room, Mode: RestoreModeFull}, nil
}

func (m *RoomManager) RestoreByPlayerID(ctx context.Context, playerID string) (*domain.Room, error) {
	if m == nil || playerID == "" {
		return nil, domain.ErrPlayerNotInRoom
	}
	m.mu.RLock()
	for _, room := range m.rooms {
		if _, exists := room.Players[playerID]; exists {
			clone := room.Clone()
			m.mu.RUnlock()
			return clone, nil
		}
	}
	m.mu.RUnlock()
	if m.snapshots == nil {
		return nil, domain.ErrRoomNotFound
	}
	room, err := m.snapshots.FindByPlayerID(ctx, playerID)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.rooms[room.ID] = room.Clone()
	m.mu.Unlock()
	return room, nil
}

func (m *RoomManager) saveLocked(ctx context.Context, room *domain.Room) error {
	if m.snapshots == nil {
		return errors.New("battle room snapshot repository is not configured")
	}
	return m.snapshots.Save(ctx, room.Clone())
}

func (m *RoomManager) recordDeltaLocked(previous, current *domain.Room) {
	delta := domain.RoomDelta{RoomID: current.ID, FromStateVersion: previous.StateVersion, ToStateVersion: current.StateVersion, Tick: current.Tick, Status: current.Status}
	for playerID, player := range current.Players {
		old, exists := previous.Players[playerID]
		if !exists || old != player {
			delta.UpsertPlayers = append(delta.UpsertPlayers, player)
		}
	}
	for playerID := range previous.Players {
		if _, exists := current.Players[playerID]; !exists {
			delta.RemovedPlayerIDs = append(delta.RemovedPlayerIDs, playerID)
		}
	}
	deltas := append(m.deltas[current.ID], delta)
	if len(deltas) > m.deltaSize {
		deltas = deltas[len(deltas)-m.deltaSize:]
	}
	m.deltas[current.ID] = deltas
}

func composeDeltas(roomID uint64, deltas []domain.RoomDelta, fromVersion, toVersion uint64) (domain.RoomDelta, bool) {
	if fromVersion >= toVersion || len(deltas) == 0 {
		return domain.RoomDelta{}, false
	}
	combined := domain.RoomDelta{RoomID: roomID, FromStateVersion: fromVersion, ToStateVersion: toVersion, UpsertPlayers: make([]domain.PlayerState, 0), RemovedPlayerIDs: make([]string, 0)}
	players := make(map[string]domain.PlayerState)
	removed := make(map[string]struct{})
	nextVersion := fromVersion
	for _, delta := range deltas {
		if delta.FromStateVersion != nextVersion || delta.ToStateVersion > toVersion {
			continue
		}
		for _, player := range delta.UpsertPlayers {
			players[player.PlayerID] = player
			delete(removed, player.PlayerID)
		}
		for _, playerID := range delta.RemovedPlayerIDs {
			delete(players, playerID)
			removed[playerID] = struct{}{}
		}
		combined.Tick = delta.Tick
		combined.Status = delta.Status
		nextVersion = delta.ToStateVersion
		if nextVersion == toVersion {
			break
		}
	}
	if nextVersion != toVersion {
		return domain.RoomDelta{}, false
	}
	for _, player := range players {
		combined.UpsertPlayers = append(combined.UpsertPlayers, player)
	}
	for playerID := range removed {
		combined.RemovedPlayerIDs = append(combined.RemovedPlayerIDs, playerID)
	}
	return combined, true
}
