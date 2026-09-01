package idgen

import (
	"sync"
	"testing"
	"time"
)

func TestGeneratorProducesUniqueIDsConcurrently(t *testing.T) {
	generator, err := New(17)
	if err != nil {
		t.Fatalf("new generator: %v", err)
	}
	const count = 10000
	ids := make(chan uint64, count)
	var wait sync.WaitGroup
	for range 10 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range count / 10 {
				ids <- generator.Next()
			}
		}()
	}
	wait.Wait()
	close(ids)
	seen := make(map[uint64]struct{}, count)
	for id := range ids {
		if _, exists := seen[id]; exists {
			t.Fatalf("duplicate id: %d", id)
		}
		seen[id] = struct{}{}
	}
}

func TestGeneratorParsesParts(t *testing.T) {
	now := func() time.Time { return DefaultEpoch.Add(5 * time.Second) }
	generator, err := NewWithClock(7, DefaultEpoch, now)
	if err != nil {
		t.Fatalf("new generator: %v", err)
	}
	parts := generator.Parse(generator.Next())
	if parts.Timestamp != DefaultEpoch.Add(5*time.Second) {
		t.Fatalf("timestamp = %s", parts.Timestamp)
	}
	if parts.NodeID != 7 || parts.Sequence != 0 {
		t.Fatalf("parts = %+v", parts)
	}
}

func TestGeneratorRejectsInvalidNodeID(t *testing.T) {
	if _, err := New(MaxNodeID + 1); err == nil {
		t.Fatal("expected invalid node id error")
	}
}
