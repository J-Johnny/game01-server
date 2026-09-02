package mongo

import (
	"errors"

	"server/services/usercenter/repository"
)

var (
	ErrCollectionRequired  = errors.New("mongo collection is required")
	ErrTransactionRequired = errors.New("mongo transaction unit of work is required")
	ErrIdempotencyNotFound = repository.ErrIdempotencyNotFound
)
