package domain

import (
	"errors"
	"time"
)

var (
	ErrRoomNotFound       = errors.New("battle room not found")
	ErrInvalidRoom        = errors.New("invalid battle room")
	ErrDuplicateRoom      = errors.New("battle room already exists")
	ErrPlayerNotInRoom    = errors.New("player is not in battle room")
	ErrDuplicatePlayer    = errors.New("player is already in battle room")
	ErrInvalidPlayerState = errors.New("invalid battle player state")
)

type RoomStatus string

const (
	RoomStatusCreated  RoomStatus = "created"
	RoomStatusRunning  RoomStatus = "running"
	RoomStatusFinished RoomStatus = "finished"
)

type PlayerState struct {
	PlayerID  string
	HP        int32
	PositionX float32
	PositionY float32
}

func (s PlayerState) Validate() error {
	if s.PlayerID == "" || s.HP < 0 {
		return ErrInvalidPlayerState
	}
	return nil
}

type Room struct {
	ID           uint64
	Tick         uint64
	StateVersion uint64
	Status       RoomStatus
	Players      map[string]PlayerState
	UpdatedAt    time.Time
}

func NewRoom(roomID uint64, players []PlayerState, now time.Time) (*Room, error) {
	room := &Room{ID: roomID, StateVersion: 1, Status: RoomStatusCreated, Players: make(map[string]PlayerState, len(players)), UpdatedAt: now.UTC()}
	if roomID == 0 || room.UpdatedAt.IsZero() {
		return nil, ErrInvalidRoom
	}
	for _, player := range players {
		if err := room.AddPlayer(player); err != nil {
			return nil, err
		}
	}
	return room, nil
}

func (r *Room) Validate() error {
	if r == nil || r.ID == 0 || r.StateVersion == 0 || r.Status == "" || r.UpdatedAt.IsZero() {
		return ErrInvalidRoom
	}
	for _, player := range r.Players {
		if err := player.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (r *Room) AddPlayer(player PlayerState) error {
	if r == nil {
		return ErrInvalidRoom
	}
	if err := player.Validate(); err != nil {
		return err
	}
	if r.Players == nil {
		r.Players = make(map[string]PlayerState)
	}
	if _, exists := r.Players[player.PlayerID]; exists {
		return ErrDuplicatePlayer
	}
	r.Players[player.PlayerID] = player
	return nil
}

func (r *Room) UpdatePlayer(player PlayerState) error {
	if r == nil {
		return ErrInvalidRoom
	}
	if err := player.Validate(); err != nil {
		return err
	}
	if _, exists := r.Players[player.PlayerID]; !exists {
		return ErrPlayerNotInRoom
	}
	r.Players[player.PlayerID] = player
	return nil
}

func (r *Room) AdvanceTick(now time.Time) error {
	if err := r.Validate(); err != nil {
		return err
	}
	r.Tick++
	r.StateVersion++
	r.Status = RoomStatusRunning
	r.UpdatedAt = now.UTC()
	return nil
}

func (r *Room) Clone() *Room {
	if r == nil {
		return nil
	}
	clone := *r
	clone.Players = make(map[string]PlayerState, len(r.Players))
	for playerID, player := range r.Players {
		clone.Players[playerID] = player
	}
	return &clone
}
