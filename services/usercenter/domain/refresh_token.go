package domain

import "time"

type RefreshToken struct {
	ID        string
	AccountID string
	TokenHash string
	InstallID string
	CreatedAt time.Time
	ExpiresAt time.Time
	RevokedAt *time.Time
}

func (t RefreshToken) Validate(now time.Time) error {
	if t.AccountID == "" || t.TokenHash == "" || t.InstallID == "" || t.ExpiresAt.IsZero() {
		return ErrInvalidToken
	}
	if !now.Before(t.ExpiresAt) {
		return ErrInvalidToken
	}
	if t.RevokedAt != nil {
		return ErrInvalidToken
	}
	return nil
}

func (t RefreshToken) IsUsable(now time.Time, installID string) bool {
	return installID != "" && t.InstallID == installID && t.Validate(now) == nil
}

func (t *RefreshToken) Revoke(now time.Time) error {
	if t == nil || t.TokenHash == "" {
		return ErrInvalidToken
	}
	if t.RevokedAt == nil {
		revokedAt := now
		t.RevokedAt = &revokedAt
	}
	return nil
}
