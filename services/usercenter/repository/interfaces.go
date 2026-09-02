package repository

import (
	"context"
	"errors"
	"time"

	"server/services/usercenter/domain"
)

var ErrIdempotencyNotFound = errors.New("idempotency record not found")

// AccountRepository exposes account operations in business terms.
// Implementations must not leak MongoDB, BSON, or persistence wrappers.
type AccountRepository interface {
	EnsureIndexes(context.Context) error
	Create(context.Context, *domain.Account) error
	FindByID(context.Context, string) (*domain.Account, error)
	LinkPlayer(context.Context, string, string, time.Time) error
}

type IdentityRepository interface {
	EnsureIndexes(context.Context) error
	Create(context.Context, *domain.Identity) error
	Find(context.Context, domain.AuthProvider, string) (*domain.Identity, error)
}

type RefreshTokenRepository interface {
	EnsureIndexes(context.Context) error
	Create(context.Context, *domain.RefreshToken) error
	FindValid(context.Context, string, time.Time) (*domain.RefreshToken, error)
	Revoke(context.Context, string, time.Time) error
	Rotate(context.Context, string, time.Time, *domain.RefreshToken) error
}

type IdempotencyRepository interface {
	EnsureIndexes(context.Context) error
	Find(context.Context, string, time.Time) (*domain.IdempotencyRecord, error)
	Create(context.Context, *domain.IdempotencyRecord) error
}

type IdempotencyCoordinator interface {
	IdempotencyRepository
	Reserve(context.Context, *domain.IdempotencyRecord, time.Time) (*domain.IdempotencyRecord, bool, error)
	Complete(context.Context, string, string, []byte, time.Time) error
	Release(context.Context, string, string) error
	Renew(context.Context, string, string, time.Time, time.Time) error
	RecoverExpired(context.Context, time.Time) (int64, error)
}
