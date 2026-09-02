package domain

import "time"

type IdempotencyState string

const (
	IdempotencyStatePending   IdempotencyState = "pending"
	IdempotencyStateCompleted IdempotencyState = "completed"
)

type IdempotencyRecord struct {
	Key           string
	Operation     string
	AccountID     string
	RequestHash   string
	Response      []byte
	State         IdempotencyState
	ReservationID string
	LeaseUntil    time.Time
	CreatedAt     time.Time
	ExpiresAt     time.Time
}

func (r *IdempotencyRecord) Validate(now time.Time) error {
	if r == nil || r.Key == "" || r.Operation == "" || r.RequestHash == "" || r.ExpiresAt.IsZero() {
		return ErrInvalidIdempotency
	}
	if r.State == "" {
		r.State = IdempotencyStateCompleted
	}
	if r.State != IdempotencyStatePending && r.State != IdempotencyStateCompleted {
		return ErrInvalidIdempotency
	}
	if !now.Before(r.ExpiresAt) {
		return ErrInvalidIdempotency
	}
	return nil
}

func (r *IdempotencyRecord) IsCompleted() bool {
	return r != nil && (r.State == "" || r.State == IdempotencyStateCompleted)
}

func (r *IdempotencyRecord) IsExpired(now time.Time) bool {
	return r == nil || !now.Before(r.ExpiresAt)
}
