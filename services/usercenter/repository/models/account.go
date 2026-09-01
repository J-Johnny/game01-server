package models

import (
	"errors"
	"server/services/common/repository/dbmodel"
	"server/services/common/repository/dbmodel/md"
	"time"
)

var (
	ErrAccountNotFound = errors.New("account not found")
	ErrInvalidAccount  = errors.New("invalid account")
)

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
	Root                 *md.NodeRoot `bson:"-"`
	dbmodel.DefaultField `bson:",inline"`
	Provider             AuthProvider `bson:"provider"`
	Subject              string       `bson:"subject"`
	PasswordHash         string       `bson:"password_hash,omitempty"`
	LinkedAt             time.Time    `bson:"linked_at"`
}

type Identities struct {
	Rows *md.Map[int64, *Identity] `bson:"rows"`
}

type RefreshToken struct {
	TokenHash string     `bson:"token_hash"`
	InstallID string     `bson:"install_id"`
	CreatedAt time.Time  `bson:"created_at"`
	ExpiresAt time.Time  `bson:"expires_at"`
	RevokedAt *time.Time `bson:"revoked_at,omitempty"`
}

type RefreshTokens struct {
	Rows *md.Map[int64, *RefreshToken] `bson:"rows"`
}

type Account struct {
	Root          *md.NodeRoot             `bson:"-"`
	AccountID     string                   `bson:"account_id"`
	Identities    *md.Node[*Identities]    `bson:"identities"`
	PlayerIDs     []string                 `bson:"player_ids"`
	RefreshTokens *md.Node[*RefreshTokens] `bson:"refresh_tokens"`
}

func (a *Account) SetDefault() error {
	if a == nil || a.AccountID == "" {
		return ErrInvalidAccount
	}

	if a.Identities == nil {
		a.Identities = md.NewNode(&Identities{
			Rows: md.NewMap[int64, *Identity](),
		})
	}

	if a.RefreshTokens == nil {
		a.RefreshTokens = md.NewNode(&RefreshTokens{
			Rows: md.NewMap[int64, *RefreshToken](),
		})
	}

	return validateAccount(*a)
}

func validateAccount(account Account) error {
	if account.AccountID == "" || account.Identities == nil || account.Identities.Data() == nil || account.Identities.Data().Rows == nil {
		return ErrInvalidAccount
	}
	l := account.Identities.Data().Rows.Len()
	if l == 0 {
		return ErrInvalidAccount
	}

	identities := make(map[string]struct{}, l)
	for _, identity := range account.Identities.Data().Rows.GetValueSlice() {
		if identity.Provider == "" || identity.Subject == "" {
			return ErrInvalidAccount
		}
		if identity.Provider == AuthProviderPassword && identity.PasswordHash == "" {
			return ErrInvalidAccount
		}

		key := string(identity.Provider) + "\x00" + identity.Subject
		if _, exists := identities[key]; exists {
			return ErrInvalidAccount
		}
		identities[key] = struct{}{}
	}

	players := make(map[string]struct{}, len(account.PlayerIDs))
	for _, playerID := range account.PlayerIDs {
		if playerID == "" {
			return ErrInvalidAccount
		}

		if _, exists := players[playerID]; exists {
			return ErrInvalidAccount
		}
		players[playerID] = struct{}{}
	}

	return nil
}

func init() {
	md.GlobalSchemaManager.Register(&Account{})
}
