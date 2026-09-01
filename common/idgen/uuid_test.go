package idgen

import (
	"testing"

	"github.com/google/uuid"
)

func TestNewUUID(t *testing.T) {
	first, err := NewUUID()
	if err != nil {
		t.Fatalf("NewUUID() error = %v", err)
	}

	second, err := NewUUID()
	if err != nil {
		t.Fatalf("NewUUID() error = %v", err)
	}

	if first == second {
		t.Fatal("NewUUID() generated duplicate UUIDs")
	}

	id, err := uuid.Parse(first)
	if err != nil {
		t.Fatalf("uuid.Parse(%q) error = %v", first, err)
	}

	if id.Version() != uuid.Version(7) {
		t.Errorf("NewUUID() version = %d, want 7", id.Version())
	}
}
