package mongo

import (
	"server/services/battle/domain"
)

func roomSnapshotDocumentFromDomain(room *domain.Room) (roomSnapshotDocument, error) {
	if err := room.Validate(); err != nil {
		return roomSnapshotDocument{}, err
	}
	document := roomSnapshotDocument{RoomID: room.ID, Tick: room.Tick, StateVersion: room.StateVersion, Status: string(room.Status), Players: make([]playerStateDocument, 0, len(room.Players)), UpdatedAt: room.UpdatedAt}
	for _, player := range room.Players {
		document.Players = append(document.Players, playerStateDocument{PlayerID: player.PlayerID, HP: player.HP, PositionX: player.PositionX, PositionY: player.PositionY})
	}
	return document, nil
}

func roomSnapshotDomainFromDocument(document roomSnapshotDocument) (*domain.Room, error) {
	room := &domain.Room{ID: document.RoomID, Tick: document.Tick, StateVersion: document.StateVersion, Status: domain.RoomStatus(document.Status), Players: make(map[string]domain.PlayerState, len(document.Players)), UpdatedAt: document.UpdatedAt}
	for _, player := range document.Players {
		if err := room.AddPlayer(domain.PlayerState{PlayerID: player.PlayerID, HP: player.HP, PositionX: player.PositionX, PositionY: player.PositionY}); err != nil {
			return nil, err
		}
	}
	if err := room.Validate(); err != nil {
		return nil, err
	}
	return room, nil
}
