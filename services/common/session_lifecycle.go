package common

import (
	"context"
	"sync"

	"server/common/streaming"
	internalpb "server/proto/gen/internalpb"

	"google.golang.org/protobuf/proto"
)

// SessionLifecycleConsumer provides idempotent lifecycle delivery for services
// that need to react to Gateway connection ownership changes.
type SessionLifecycleConsumer struct {
	mu          sync.RWMutex
	latestEpoch map[string]uint64
	eventIDs    map[string]struct{}
}

func NewSessionLifecycleConsumer() *SessionLifecycleConsumer {
	return &SessionLifecycleConsumer{
		latestEpoch: make(map[string]uint64),
		eventIDs:    make(map[string]struct{}),
	}
}

func (c *SessionLifecycleConsumer) Register(router *streaming.Router, target internalpb.ServiceType) {
	router.Register(target, uint32(internalpb.InternalMessageId_INTERNAL_MESSAGE_ID_SESSION_LIFECYCLE_EVENT), streaming.MessageHandlerFunc(c.handle))
}

func (c *SessionLifecycleConsumer) handle(_ context.Context, _ streaming.Peer, envelope *internalpb.InternalEnvelope) (*streaming.MessageResult, error) {
	event := &internalpb.SessionLifecycleEvent{}
	if err := proto.Unmarshal(envelope.Payload, event); err != nil {
		return nil, err
	}
	if event.EventId == "" || event.SessionId == "" || event.ConnectionEpoch == 0 {
		return nil, nil
	}
	if !c.Accept(event) {
		return nil, nil
	}
	return nil, nil
}

func (c *SessionLifecycleConsumer) Accept(event *internalpb.SessionLifecycleEvent) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.eventIDs[event.EventId]; exists {
		return false
	}
	if latest := c.latestEpoch[event.SessionId]; event.ConnectionEpoch < latest {
		return false
	}
	c.eventIDs[event.EventId] = struct{}{}
	if event.ConnectionEpoch > c.latestEpoch[event.SessionId] {
		c.latestEpoch[event.SessionId] = event.ConnectionEpoch
	}
	return true
}

func (c *SessionLifecycleConsumer) LatestEpoch(sessionID string) uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.latestEpoch[sessionID]
}
