package domain

import (
	"errors"
	"time"
)

var (
	ErrPlayerNotFound       = errors.New("player not found")
	ErrAssetNotFound        = errors.New("player assets not found")
	ErrInvalidPlayer        = errors.New("invalid player")
	ErrInvalidSettlement    = errors.New("invalid settlement")
	ErrSettlementNotFound   = errors.New("settlement not found")
	ErrSettlementApplied    = errors.New("settlement already applied")
	ErrSettlementConflict   = errors.New("settlement id conflict")
	ErrInsufficientCurrency = errors.New("insufficient currency")
)

type Player struct {
	ID             string
	AccountID      string
	Nickname       string
	Avatar         string
	Region         string
	IsDefault      bool
	ProfileVersion uint64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (p Player) Validate() error {
	if p.ID == "" || p.AccountID == "" || p.Nickname == "" || p.Region == "" || p.CreatedAt.IsZero() || p.UpdatedAt.IsZero() {
		return ErrInvalidPlayer
	}
	return nil
}

type Assets struct {
	PlayerID     string
	Currency     map[string]int64
	AssetVersion uint64
	UpdatedAt    time.Time
}

func (a Assets) Balance(assetType string) int64 {
	return a.Currency[assetType]
}

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
