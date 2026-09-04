package domain

import "time"

type Snapshot struct {
	Player        Player
	Assets        Assets
	SchemaVersion uint32
	CreatedAt     time.Time
}

func (s Snapshot) StateVersion() uint64 {
	if s.Player.ProfileVersion >= s.Assets.AssetVersion {
		return s.Player.ProfileVersion
	}
	return s.Assets.AssetVersion
}
