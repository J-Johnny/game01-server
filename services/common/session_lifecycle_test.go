package common

import (
	"testing"

	internalpb "server/proto/gen/internalpb"
)

func TestSessionLifecycleConsumerRejectsDuplicateAndStaleEpoch(t *testing.T) {
	consumer := NewSessionLifecycleConsumer()
	current := &internalpb.SessionLifecycleEvent{EventId: "session-1:2:resumed", SessionId: "session-1", ConnectionEpoch: 2}
	if !consumer.Accept(current) {
		t.Fatal("current lifecycle event was rejected")
	}
	if consumer.Accept(current) {
		t.Fatal("duplicate lifecycle event was accepted")
	}
	if consumer.Accept(&internalpb.SessionLifecycleEvent{EventId: "session-1:1:disconnected", SessionId: "session-1", ConnectionEpoch: 1}) {
		t.Fatal("stale lifecycle event was accepted")
	}
	if !consumer.Accept(&internalpb.SessionLifecycleEvent{EventId: "session-1:2:preempted", SessionId: "session-1", ConnectionEpoch: 2}) {
		t.Fatal("distinct event for current epoch was rejected")
	}
}
