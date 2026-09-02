package mongo

import "time"

type playerStateDocument struct {
	PlayerID  string  `bson:"player_id"`
	HP        int32   `bson:"hp"`
	PositionX float32 `bson:"position_x"`
	PositionY float32 `bson:"position_y"`
}

type roomSnapshotDocument struct {
	RoomID       uint64                `bson:"room_id"`
	Tick         uint64                `bson:"tick"`
	StateVersion uint64                `bson:"state_version"`
	Status       string                `bson:"status"`
	Players      []playerStateDocument `bson:"players"`
	UpdatedAt    time.Time             `bson:"updated_at"`
}
