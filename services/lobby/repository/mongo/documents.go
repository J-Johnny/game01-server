package mongo

import "time"

type playerDocument struct {
	PlayerID       string    `bson:"player_id"`
	AccountID      string    `bson:"account_id"`
	Nickname       string    `bson:"nickname"`
	Avatar         string    `bson:"avatar,omitempty"`
	Region         string    `bson:"region"`
	IsDefault      bool      `bson:"is_default"`
	ProfileVersion uint64    `bson:"profile_version"`
	CreatedAt      time.Time `bson:"created_at"`
	UpdatedAt      time.Time `bson:"updated_at"`
}

type assetsDocument struct {
	PlayerID     string           `bson:"player_id"`
	Currency     map[string]int64 `bson:"currency"`
	AssetVersion uint64           `bson:"asset_version"`
	UpdatedAt    time.Time        `bson:"updated_at"`
}

type ledgerDocument struct {
	SettlementID string    `bson:"settlement_id"`
	PlayerID     string    `bson:"player_id"`
	AssetType    string    `bson:"asset_type"`
	Delta        int64     `bson:"delta"`
	Reason       string    `bson:"reason"`
	Source       string    `bson:"source"`
	CreatedAt    time.Time `bson:"created_at"`
}

type snapshotDocument struct {
	PlayerID       string           `bson:"player_id"`
	AccountID      string           `bson:"account_id"`
	Nickname       string           `bson:"nickname"`
	Region         string           `bson:"region"`
	ProfileVersion uint64           `bson:"profile_version"`
	AssetVersion   uint64           `bson:"asset_version"`
	Currency       map[string]int64 `bson:"currency"`
	SchemaVersion  uint32           `bson:"schema_version"`
	CreatedAt      time.Time        `bson:"created_at"`
}
