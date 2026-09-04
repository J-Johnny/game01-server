package domain

import "time"

type Assets struct {
	PlayerID     string
	Currency     map[string]int64
	AssetVersion uint64
	UpdatedAt    time.Time
}

func (a Assets) Balance(assetType string) int64 {
	return a.Currency[assetType]
}
