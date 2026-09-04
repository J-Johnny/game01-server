package domain

import (
	"time"
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
