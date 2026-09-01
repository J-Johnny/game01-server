package idgen

import "github.com/google/uuid"

// NewUUID returns a time-ordered UUIDv7 suitable for externally visible IDs.
func NewUUID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}

	return id.String(), nil
}
