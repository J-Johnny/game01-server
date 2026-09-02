package mongo

import (
	"time"

	"server/services/usercenter/domain"
)

func accountDocumentFromDomain(account *domain.Account) (accountDocument, error) {
	if account == nil {
		return accountDocument{}, domain.ErrInvalidAccount
	}
	if err := account.Validate(); err != nil {
		return accountDocument{}, err
	}
	return accountDocument{
		AccountID: account.ID,
		PlayerIDs: cloneStrings(account.PlayerIDs),
		CreatedAt: account.CreatedAt,
		UpdatedAt: account.UpdatedAt,
	}, nil
}

func accountDomainFromDocument(document accountDocument, identities []identityDocument) (domain.Account, error) {
	account := domain.Account{
		ID:         document.AccountID,
		PlayerIDs:  cloneStrings(document.PlayerIDs),
		CreatedAt:  document.CreatedAt,
		UpdatedAt:  document.UpdatedAt,
		Identities: make([]domain.Identity, 0, len(identities)),
	}
	for _, identity := range identities {
		account.Identities = append(account.Identities, identityDomainFromDocument(identity))
	}
	if err := account.Validate(); err != nil {
		return domain.Account{}, err
	}
	return account, nil
}

func identityDocumentFromDomain(identity *domain.Identity) (identityDocument, error) {
	if identity == nil {
		return identityDocument{}, domain.ErrInvalidIdentity
	}
	if err := identity.Validate(); err != nil {
		return identityDocument{}, err
	}
	if identity.AccountID == "" {
		return identityDocument{}, domain.ErrInvalidIdentity
	}
	return identityDocument{
		IdentityID:   identity.ID,
		AccountID:    identity.AccountID,
		Provider:     string(identity.Provider),
		Subject:      identity.Subject,
		PasswordHash: identity.PasswordHash,
		LinkedAt:     identity.LinkedAt,
	}, nil
}

func identityDomainFromDocument(document identityDocument) domain.Identity {
	return domain.Identity{
		ID:           document.IdentityID,
		AccountID:    document.AccountID,
		Provider:     domain.AuthProvider(document.Provider),
		Subject:      document.Subject,
		PasswordHash: document.PasswordHash,
		LinkedAt:     document.LinkedAt,
	}
}

func refreshTokenDocumentFromDomain(token *domain.RefreshToken) (refreshTokenDocument, error) {
	if token == nil {
		return refreshTokenDocument{}, domain.ErrInvalidToken
	}
	if token.AccountID == "" || token.TokenHash == "" || token.InstallID == "" || token.ExpiresAt.IsZero() {
		return refreshTokenDocument{}, domain.ErrInvalidToken
	}
	if token.RevokedAt == nil && !time.Now().UTC().Before(token.ExpiresAt) {
		return refreshTokenDocument{}, domain.ErrInvalidToken
	}
	return refreshTokenDocument{
		TokenID:   token.ID,
		AccountID: token.AccountID,
		TokenHash: token.TokenHash,
		InstallID: token.InstallID,
		CreatedAt: token.CreatedAt,
		ExpiresAt: token.ExpiresAt,
		RevokedAt: cloneTime(token.RevokedAt),
	}, nil
}

func refreshTokenDomainFromDocument(document refreshTokenDocument) domain.RefreshToken {
	return domain.RefreshToken{
		ID:        document.TokenID,
		AccountID: document.AccountID,
		TokenHash: document.TokenHash,
		InstallID: document.InstallID,
		CreatedAt: document.CreatedAt,
		ExpiresAt: document.ExpiresAt,
		RevokedAt: cloneTime(document.RevokedAt),
	}
}

func idempotencyDocumentFromDomain(record *domain.IdempotencyRecord) (idempotencyDocument, error) {
	if record == nil {
		return idempotencyDocument{}, domain.ErrInvalidIdempotency
	}
	if err := record.Validate(time.Now().UTC()); err != nil {
		return idempotencyDocument{}, err
	}
	return idempotencyDocument{
		Key:           record.Key,
		Operation:     record.Operation,
		AccountID:     record.AccountID,
		RequestHash:   record.RequestHash,
		Response:      append([]byte(nil), record.Response...),
		State:         string(record.State),
		ReservationID: record.ReservationID,
		LeaseUntil:    record.LeaseUntil,
		CreatedAt:     record.CreatedAt,
		ExpiresAt:     record.ExpiresAt,
	}, nil
}

func idempotencyDomainFromDocument(document idempotencyDocument) domain.IdempotencyRecord {
	return domain.IdempotencyRecord{
		Key:           document.Key,
		Operation:     document.Operation,
		AccountID:     document.AccountID,
		RequestHash:   document.RequestHash,
		Response:      append([]byte(nil), document.Response...),
		State:         domain.IdempotencyState(document.State),
		ReservationID: document.ReservationID,
		LeaseUntil:    document.LeaseUntil,
		CreatedAt:     document.CreatedAt,
		ExpiresAt:     document.ExpiresAt,
	}
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneStrings(values []string) []string {
	cloned := make([]string, len(values))
	copy(cloned, values)
	return cloned
}
