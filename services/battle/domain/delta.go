package domain

type RoomDelta struct {
	RoomID           uint64
	FromStateVersion uint64
	ToStateVersion   uint64
	Tick             uint64
	Status           RoomStatus
	UpsertPlayers    []PlayerState
	RemovedPlayerIDs []string
}

func (d RoomDelta) Covers(lastStateVersion uint64) bool {
	return d.FromStateVersion == lastStateVersion && d.ToStateVersion > lastStateVersion
}
