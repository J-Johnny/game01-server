package domain

import "time"

type AuthProvider string

const (
	AuthProviderGuest    AuthProvider = "guest"
	AuthProviderSteam    AuthProvider = "steam"
	AuthProviderApple    AuthProvider = "apple"
	AuthProviderGoogle   AuthProvider = "google"
	AuthProviderWeChat   AuthProvider = "wechat"
	AuthProviderPassword AuthProvider = "password"
)

type Identity struct {
	ID           string
	AccountID    string
	Provider     AuthProvider
	Subject      string
	PasswordHash string
	LinkedAt     time.Time
}

func (i Identity) Validate() error {
	if i.Provider == "" || i.Subject == "" {
		return ErrInvalidIdentity
	}
	if i.Provider == AuthProviderPassword && i.PasswordHash == "" {
		return ErrInvalidIdentity
	}
	return nil
}
