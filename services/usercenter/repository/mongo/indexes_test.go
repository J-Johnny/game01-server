package mongo

import "testing"

func TestAccountIndexes(t *testing.T) {
	indexes := accountIndexes()
	if len(indexes) != 1 || indexes[0].Options == nil || indexes[0].Options.Unique == nil || !*indexes[0].Options.Unique {
		t.Fatalf("account_id must have one unique index: %+v", indexes)
	}
}

func TestIdentityIndexes(t *testing.T) {
	indexes := identityIndexes()
	if len(indexes) != 3 {
		t.Fatalf("identity index count = %d, want 3", len(indexes))
	}
	if indexes[1].Options == nil || indexes[1].Options.Unique == nil || !*indexes[1].Options.Unique {
		t.Fatal("provider and subject index must be unique")
	}
}

func TestRefreshTokenIndexes(t *testing.T) {
	indexes := refreshTokenIndexes()
	if len(indexes) != 4 {
		t.Fatalf("refresh token index count = %d, want 4", len(indexes))
	}
	if indexes[3].Options == nil || indexes[3].Options.ExpireAfterSeconds == nil || *indexes[3].Options.ExpireAfterSeconds != 0 {
		t.Fatal("expires_at index must be a TTL index")
	}
}

func TestIdempotencyIndexes(t *testing.T) {
	indexes := idempotencyIndexes()
	if len(indexes) != 3 {
		t.Fatalf("idempotency index count = %d, want 3", len(indexes))
	}
	if indexes[0].Options == nil || indexes[0].Options.Unique == nil || !*indexes[0].Options.Unique {
		t.Fatal("idempotency key index must be unique")
	}
	if indexes[1].Options == nil || indexes[1].Options.ExpireAfterSeconds == nil || *indexes[1].Options.ExpireAfterSeconds != 0 {
		t.Fatal("idempotency expires_at index must be a TTL index")
	}
}
