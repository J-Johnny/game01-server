package domain

import "time"

type Account struct {
	ID         string
	Identities []Identity
	PlayerIDs  []string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (a *Account) Validate() error {
	if a == nil || a.ID == "" {
		return ErrInvalidAccount
	}

	identityKeys := make(map[string]struct{}, len(a.Identities))
	for _, identity := range a.Identities {
		if err := identity.Validate(); err != nil {
			return err
		}
		key := string(identity.Provider) + "\x00" + identity.Subject
		if _, exists := identityKeys[key]; exists {
			return ErrInvalidAccount
		}
		identityKeys[key] = struct{}{}
	}

	playerIDs := make(map[string]struct{}, len(a.PlayerIDs))
	for _, playerID := range a.PlayerIDs {
		if playerID == "" {
			return ErrInvalidAccount
		}
		if _, exists := playerIDs[playerID]; exists {
			return ErrInvalidAccount
		}
		playerIDs[playerID] = struct{}{}
	}

	return nil
}

func (a *Account) FindIdentity(provider AuthProvider, subject string) (*Identity, bool) {
	if a == nil {
		return nil, false
	}
	for index := range a.Identities {
		identity := &a.Identities[index]
		if identity.Provider == provider && identity.Subject == subject {
			return identity, true
		}
	}
	return nil, false
}

func (a *Account) AddIdentity(identity Identity) error {
	if a == nil || identity.Validate() != nil {
		return ErrInvalidIdentity
	}
	if _, exists := a.FindIdentity(identity.Provider, identity.Subject); exists {
		return ErrInvalidIdentity
	}
	identity.AccountID = a.ID
	a.Identities = append(a.Identities, identity)
	return nil
}

func (a *Account) LinkPlayer(playerID string) error {
	if a == nil || a.ID == "" || playerID == "" {
		return ErrInvalidAccount
	}
	if a.HasPlayer(playerID) {
		return ErrPlayerLinked
	}
	a.PlayerIDs = append(a.PlayerIDs, playerID)
	a.UpdatedAt = time.Now().UTC()
	return nil
}

func (a *Account) HasPlayer(playerID string) bool {
	if a == nil || playerID == "" {
		return false
	}
	for _, linkedPlayerID := range a.PlayerIDs {
		if linkedPlayerID == playerID {
			return true
		}
	}
	return false
}
