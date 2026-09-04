package domain

import "time"

type Settlement struct {
	ID        string
	PlayerID  string
	AssetType string
	Delta     int64
	Reason    string
	Source    string
	CreatedAt time.Time
}

func (s Settlement) Validate() error {
	if s.ID == "" || s.PlayerID == "" || s.AssetType == "" || s.Delta == 0 || s.Reason == "" || s.Source == "" || s.CreatedAt.IsZero() {
		return ErrInvalidSettlement
	}
	return nil
}
