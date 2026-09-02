package mongo

import "time"

type accountDocument struct {
	AccountID string    `bson:"account_id"`
	PlayerIDs []string  `bson:"player_ids"`
	CreatedAt time.Time `bson:"created_at"`
	UpdatedAt time.Time `bson:"updated_at"`
}

type identityDocument struct {
	IdentityID   string    `bson:"identity_id"`
	AccountID    string    `bson:"account_id"`
	Provider     string    `bson:"provider"`
	Subject      string    `bson:"subject"`
	PasswordHash string    `bson:"password_hash,omitempty"`
	LinkedAt     time.Time `bson:"linked_at"`
}

type refreshTokenDocument struct {
	TokenID   string     `bson:"token_id"`
	AccountID string     `bson:"account_id"`
	TokenHash string     `bson:"token_hash"`
	InstallID string     `bson:"install_id"`
	CreatedAt time.Time  `bson:"created_at"`
	ExpiresAt time.Time  `bson:"expires_at"`
	RevokedAt *time.Time `bson:"revoked_at,omitempty"`
}

type idempotencyDocument struct {
	Key           string    `bson:"key"`
	Operation     string    `bson:"operation"`
	AccountID     string    `bson:"account_id,omitempty"`
	RequestHash   string    `bson:"request_hash"`
	Response      []byte    `bson:"response"`
	State         string    `bson:"state"`
	ReservationID string    `bson:"reservation_id,omitempty"`
	LeaseUntil    time.Time `bson:"lease_until,omitempty"`
	CreatedAt     time.Time `bson:"created_at"`
	ExpiresAt     time.Time `bson:"expires_at"`
}
