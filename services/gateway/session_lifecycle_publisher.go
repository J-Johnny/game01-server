package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"server/common/observability"
	"server/common/reliability"
	"server/common/streaming"
	internalpb "server/proto/gen/internalpb"
	"server/services/gateway/session"

	"google.golang.org/protobuf/proto"
)

type sessionLifecyclePublisher struct {
	client  func(string) (*streaming.Client, bool)
	metrics *observability.Metrics
	retry   reliability.RetryPolicy
	queue   chan session.LifecycleEvent
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	once    sync.Once
}

func newSessionLifecyclePublisher(client func(string) (*streaming.Client, bool), metrics *observability.Metrics, retry reliability.RetryPolicy) *sessionLifecyclePublisher {
	ctx, cancel := context.WithCancel(context.Background())
	publisher := &sessionLifecyclePublisher{
		client:  client,
		metrics: metrics,
		retry:   retry,
		queue:   make(chan session.LifecycleEvent, 256),
		ctx:     ctx,
		cancel:  cancel,
		done:    make(chan struct{}),
	}
	go publisher.run()
	return publisher
}

func (p *sessionLifecyclePublisher) Publish(_ context.Context, event session.LifecycleEvent) {
	select {
	case <-p.ctx.Done():
		return
	case p.queue <- event:
		p.metrics.SetSessionLifecycleQueueDepth(len(p.queue))
	default:
		p.metrics.ObserveSessionLifecycle("queue", string(event.Type), "dropped")
		slog.Error("gateway session lifecycle queue is full", "event_id", event.EventID, "type", event.Type)
	}
}

func (p *sessionLifecyclePublisher) Close() {
	p.once.Do(func() {
		p.cancel()
		<-p.done
	})
}

func (p *sessionLifecyclePublisher) run() {
	defer close(p.done)
	for {
		select {
		case <-p.ctx.Done():
			return
		case event := <-p.queue:
			p.metrics.SetSessionLifecycleQueueDepth(len(p.queue))
			p.deliver(event)
		}
	}
}

func (p *sessionLifecyclePublisher) deliver(event session.LifecycleEvent) {
	payload, err := proto.Marshal(&internalpb.SessionLifecycleEvent{
		EventId:              event.EventID,
		Type:                 lifecycleTypeProto(event.Type),
		SessionId:            event.SessionID,
		AccountId:            event.AccountID,
		PlayerId:             event.PlayerID,
		ConnectionId:         event.ConnectionID,
		ConnectionEpoch:      event.ConnectionEpoch,
		GatewayInstanceId:    event.GatewayInstanceID,
		OccurredAtUnixMillis: event.OccurredAt.UnixMilli(),
	})
	if err != nil {
		slog.Error("marshal session lifecycle event", "event_id", event.EventID, "error", err)
		return
	}
	for _, target := range []struct {
		name        string
		serviceType internalpb.ServiceType
	}{{"lobby", internalpb.ServiceType_SERVICE_TYPE_LOBBY}, {"battle", internalpb.ServiceType_SERVICE_TYPE_BATTLE}} {
		delivery := func(ctx context.Context) error {
			client, ok := p.client(target.name)
			if !ok || client == nil {
				return fmt.Errorf("%s lifecycle client unavailable", target.name)
			}
			return client.SendEvent(&internalpb.InternalEnvelope{
				TargetService: target.serviceType,
				MessageId:     uint32(internalpb.InternalMessageId_INTERNAL_MESSAGE_ID_SESSION_LIFECYCLE_EVENT),
				Payload:       payload,
			})
		}
		if err := p.retry.Do(p.ctx, delivery); err != nil {
			p.metrics.ObserveSessionLifecycle(target.name, string(event.Type), "failed")
			slog.Warn("deliver session lifecycle event failed", "event_id", event.EventID, "target_service", target.name, "error", err)
			continue
		}
		p.metrics.ObserveSessionLifecycle(target.name, string(event.Type), "delivered")
	}
}

func lifecycleTypeProto(eventType session.LifecycleType) internalpb.SessionLifecycleType {
	switch eventType {
	case session.LifecycleConnected:
		return internalpb.SessionLifecycleType_SESSION_LIFECYCLE_TYPE_CONNECTED
	case session.LifecycleDisconnected:
		return internalpb.SessionLifecycleType_SESSION_LIFECYCLE_TYPE_DISCONNECTED
	case session.LifecycleResumed:
		return internalpb.SessionLifecycleType_SESSION_LIFECYCLE_TYPE_RESUMED
	case session.LifecycleExpired:
		return internalpb.SessionLifecycleType_SESSION_LIFECYCLE_TYPE_EXPIRED
	case session.LifecyclePreempted:
		return internalpb.SessionLifecycleType_SESSION_LIFECYCLE_TYPE_PREEMPTED
	default:
		return internalpb.SessionLifecycleType_SESSION_LIFECYCLE_TYPE_UNSPECIFIED
	}
}
