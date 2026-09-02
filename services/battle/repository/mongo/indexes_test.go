package mongo

import "testing"

func TestRoomSnapshotIndexes(t *testing.T) {
	indexes := roomSnapshotIndexes()
	if len(indexes) != 3 {
		t.Fatalf("room snapshot index count = %d, want 3", len(indexes))
	}
	if indexes[0].Options == nil || indexes[0].Options.Unique == nil || !*indexes[0].Options.Unique {
		t.Fatal("room_id index must be unique")
	}
}
