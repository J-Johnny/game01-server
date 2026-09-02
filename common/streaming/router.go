package streaming

import (
	"context"
	"sync"

	"google.golang.org/protobuf/proto"
	internalpb "server/proto/gen/internalpb"
)

type MessageHandler interface {
	HandleMessage(context.Context, Peer, *internalpb.InternalEnvelope) (*MessageResult, error)
}

type MessageResult struct {
	MessageID uint32
	Message   proto.Message
}

type MessageHandlerFunc func(context.Context, Peer, *internalpb.InternalEnvelope) (*MessageResult, error)

func (f MessageHandlerFunc) HandleMessage(ctx context.Context, peer Peer, envelope *internalpb.InternalEnvelope) (*MessageResult, error) {
	return f(ctx, peer, envelope)
}

type Router struct {
	mu       sync.RWMutex
	handlers map[internalpb.ServiceType]map[uint32]MessageHandler
}

func NewRouter() *Router {
	return &Router{
		handlers: make(map[internalpb.ServiceType]map[uint32]MessageHandler),
	}
}

func (r *Router) Register(service internalpb.ServiceType, messageID uint32, handler MessageHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.handlers[service] == nil {
		r.handlers[service] = make(map[uint32]MessageHandler)
	}
	r.handlers[service][messageID] = handler
}

func (r *Router) Handle(ctx context.Context, peer Peer, envelope *internalpb.InternalEnvelope) error {
	if envelope.Kind == internalpb.EnvelopeKind_ENVELOPE_KIND_EVENT {
		return r.dispatch(ctx, peer, envelope, false)
	}
	if envelope.Kind != internalpb.EnvelopeKind_ENVELOPE_KIND_REQUEST {
		return r.sendError(peer, envelope.RequestId, 1, "unsupported envelope kind")
	}
	return r.dispatch(ctx, peer, envelope, true)
}

func (r *Router) dispatch(ctx context.Context, peer Peer, envelope *internalpb.InternalEnvelope, reply bool) error {
	r.mu.RLock()
	handler := r.handlers[envelope.TargetService][envelope.MessageId]
	r.mu.RUnlock()
	if handler == nil {
		if reply {
			return r.sendError(peer, envelope.RequestId, 2, "target service or message handler is unavailable")
		}
		return nil
	}
	result, err := handler.HandleMessage(ctx, peer, envelope)
	if err != nil {
		if reply {
			return r.sendError(peer, envelope.RequestId, 3, err.Error())
		}
		return err
	}
	if !reply || result == nil || result.Message == nil {
		return nil
	}
	payload, err := proto.Marshal(result.Message)
	if err != nil {
		return err
	}
	return peer.Connection.Send(&internalpb.InternalEnvelope{Kind: internalpb.EnvelopeKind_ENVELOPE_KIND_RESPONSE, RequestId: envelope.RequestId, SourceService: envelope.TargetService, TargetService: peer.ServiceType, MessageId: result.MessageID, Payload: payload})
}

func (r *Router) sendError(peer Peer, requestID uint64, code uint32, message string) error {
	return peer.Connection.Send(&internalpb.InternalEnvelope{Kind: internalpb.EnvelopeKind_ENVELOPE_KIND_ERROR, RequestId: requestID, ErrorCode: code, ErrorMessage: message})
}
