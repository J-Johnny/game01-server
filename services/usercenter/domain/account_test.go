package domain

import (
	"errors"
	"testing"
	"time"
)

func TestAccountIdentityAndPlayerBehavior(t *testing.T) {
	account := &Account{ID: "account-1", Identities: []Identity{{Provider: AuthProviderPassword, Subject: "user", PasswordHash: "hash"}}}
	if err := account.Validate(); err != nil {
		t.Fatalf("validate account: %v", err)
	}

	identity, found := account.FindIdentity(AuthProviderPassword, "user")
	if !found || identity.PasswordHash != "hash" {
		t.Fatalf("identity lookup failed: found=%v identity=%+v", found, identity)
	}
	if err := account.LinkPlayer("player-1"); err != nil {
		t.Fatalf("link player: %v", err)
	}
	if !account.HasPlayer("player-1") {
		t.Fatal("linked player was not found")
	}
	if !errors.Is(account.LinkPlayer("player-1"), ErrPlayerLinked) {
		t.Fatal("duplicate player link did not fail")
	}
}

func TestAccountRejectsDuplicateIdentity(t *testing.T) {
	account := &Account{ID: "account-1", Identities: []Identity{{Provider: AuthProviderGuest, Subject: "install-1"}, {Provider: AuthProviderGuest, Subject: "install-2"}}}
	if err := account.AddIdentity(Identity{Provider: AuthProviderGuest, Subject: "install-1"}); !errors.Is(err, ErrInvalidIdentity) {
		t.Fatalf("duplicate identity error = %v", err)
	}
	account.Identities = append(account.Identities, Identity{Provider: AuthProviderGuest, Subject: "install-1"})
	if err := account.Validate(); !errors.Is(err, ErrInvalidAccount) {
		t.Fatalf("duplicate identity in account error = %v", err)
	}
}

func TestRefreshTokenLifecycle(t *testing.T) {
	now := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	token := &RefreshToken{AccountID: "account-1", TokenHash: "hash", InstallID: "install-1", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if !token.IsUsable(now, "install-1") {
		t.Fatal("valid refresh token was not usable")
	}
	if err := token.Revoke(now); err != nil {
		t.Fatalf("revoke token: %v", err)
	}
	if token.IsUsable(now, "install-1") {
		t.Fatal("revoked refresh token remained usable")
	}
}
