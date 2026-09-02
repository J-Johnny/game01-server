package mongo

import (
	"errors"
	"testing"
	"time"

	"server/services/usercenter/domain"
)

func TestAccountMapperKeepsDomainIndependentPersistenceShape(t *testing.T) {
	account := &domain.Account{
		ID:        "account-1",
		PlayerIDs: []string{"player-1"},
		Identities: []domain.Identity{
			{ID: "identity-1", AccountID: "account-1", Provider: domain.AuthProviderPassword, Subject: "user", PasswordHash: "hash"},
		},
	}

	document, err := accountDocumentFromDomain(account)
	if err != nil {
		t.Fatalf("account to document: %v", err)
	}
	if document.AccountID != account.ID || len(document.PlayerIDs) != 1 {
		t.Fatalf("unexpected account document: %+v", document)
	}
	document.PlayerIDs[0] = "changed"
	if account.PlayerIDs[0] == "changed" {
		t.Fatal("account player IDs share document storage")
	}

	identityDoc, err := identityDocumentFromDomain(&account.Identities[0])
	if err != nil {
		t.Fatalf("identity to document: %v", err)
	}
	decoded, err := accountDomainFromDocument(document, []identityDocument{identityDoc})
	if err != nil {
		t.Fatalf("account from document: %v", err)
	}
	if decoded.ID != account.ID || decoded.Identities[0].Subject != "user" {
		t.Fatalf("unexpected decoded account: %+v", decoded)
	}
}

func TestMapperRejectsInvalidDomainObjects(t *testing.T) {
	if _, err := accountDocumentFromDomain(nil); !errors.Is(err, domain.ErrInvalidAccount) {
		t.Fatalf("nil account error = %v", err)
	}
	if _, err := identityDocumentFromDomain(&domain.Identity{Provider: domain.AuthProviderPassword, Subject: "user"}); !errors.Is(err, domain.ErrInvalidIdentity) {
		t.Fatalf("invalid identity error = %v", err)
	}
	if _, err := refreshTokenDocumentFromDomain(&domain.RefreshToken{}); !errors.Is(err, domain.ErrInvalidToken) {
		t.Fatalf("invalid token error = %v", err)
	}
}

func TestRefreshTokenMapperCopiesRevokedAt(t *testing.T) {
	revokedAt := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	document := refreshTokenDocument{TokenID: "token-1", AccountID: "account-1", TokenHash: "hash", InstallID: "install-1", ExpiresAt: revokedAt.Add(time.Hour), RevokedAt: &revokedAt}
	token := refreshTokenDomainFromDocument(document)
	if token.RevokedAt == nil || !token.RevokedAt.Equal(revokedAt) {
		t.Fatalf("unexpected revoked time: %+v", token.RevokedAt)
	}
	*token.RevokedAt = token.RevokedAt.Add(time.Hour)
	if document.RevokedAt.Equal(*token.RevokedAt) {
		t.Fatal("token and document share revoked time pointer")
	}
}
