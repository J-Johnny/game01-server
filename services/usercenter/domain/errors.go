package domain

import "errors"

var (
	ErrAccountNotFound      = errors.New("account not found")
	ErrIdentityNotFound     = errors.New("identity not found")
	ErrInvalidAccount       = errors.New("invalid account")
	ErrInvalidIdentity      = errors.New("invalid identity")
	ErrInvalidToken         = errors.New("invalid refresh token")
	ErrPlayerLinked         = errors.New("player is already linked")
	ErrInvalidIdempotency   = errors.New("invalid idempotency record")
	ErrIdempotencyConflict  = errors.New("idempotency key conflict")
	ErrIdempotencyLeaseLost = errors.New("idempotency lease lost")
)
