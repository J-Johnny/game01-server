package domain

import (
	"errors"
	"testing"
	"time"
)

func TestRoomRejectsDuplicateAndUnknownPlayers(t *testing.T) {
	room, err := NewRoom(1, []PlayerState{{PlayerID: "player-1", HP: 100}}, time.Now().UTC())
	if err != nil {
		t.Fatalf("new room: %v", err)
	}
	if !errors.Is(room.AddPlayer(PlayerState{PlayerID: "player-1", HP: 100}), ErrDuplicatePlayer) {
		t.Fatal("duplicate player was accepted")
	}
	if !errors.Is(room.UpdatePlayer(PlayerState{PlayerID: "player-2", HP: 100}), ErrPlayerNotInRoom) {
		t.Fatal("unknown player update was accepted")
	}
}
