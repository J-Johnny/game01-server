package mongo

import "testing"

func TestPlayerIndexesAllowMultiplePlayersPerAccount(t *testing.T) {
	indexes := playerIndexes()
	if len(indexes) != 3 {
		t.Fatalf("player index count = %d, want 3", len(indexes))
	}

	if indexes[1].Options != nil && indexes[1].Options.Unique != nil && *indexes[1].Options.Unique {
		t.Fatal("account_id index must not be unique")
	}

	if indexes[2].Options == nil || indexes[2].Options.Unique == nil || !*indexes[2].Options.Unique {
		t.Fatal("default player index must be unique")
	}

	if indexes[2].Options.PartialFilterExpression == nil {
		t.Fatal("default player index must apply only to default players")
	}
}
