package streaming

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	internalpb "server/proto/gen/internalpb"
)

var ErrRequestTimeout = errors.New("stream request timed out")

type Client struct {
	connection *grpc.ClientConn
	stream     internalpb.ServiceStream_ConnectClient
	service    internalpb.ServiceType
	instanceID string
	requests   map[uint64]chan *internalpb.InternalEnvelope
	mu         sync.Mutex
	sendMu     sync.Mutex
	nextID     atomic.Uint64
	done       chan struct{}
	closeOnce  sync.Once
	onEvent    Handler
}

func NewClient(ctx context.Context, connection *grpc.ClientConn, service internalpb.ServiceType, instanceID string, onEvent Handler) (*Client, error) {
	stream, err := internalpb.NewServiceStreamClient(connection).Connect(ctx)
	if err != nil {
		return nil, err
	}
	client := &Client{connection: connection, stream: stream, service: service, instanceID: instanceID, requests: make(map[uint64]chan *internalpb.InternalEnvelope), done: make(chan struct{}), onEvent: onEvent}
	if err := client.sendHello(); err != nil {
		client.Close()
		return nil, err
	}
	go client.readLoop(ctx)
	return client, nil
}

func (c *Client) Request(ctx context.Context, envelope *internalpb.InternalEnvelope) (*internalpb.InternalEnvelope, error) {
	if envelope.RequestId == 0 {
		envelope.RequestId = c.nextID.Add(1)
	}
	envelope.Kind = internalpb.EnvelopeKind_ENVELOPE_KIND_REQUEST
	response := make(chan *internalpb.InternalEnvelope, 1)
	c.mu.Lock()
	c.requests[envelope.RequestId] = response
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.requests, envelope.RequestId); c.mu.Unlock() }()
	if err := c.send(envelope); err != nil {
		return nil, err
	}
	select {
	case result := <-response:
		return result, nil
	case <-ctx.Done():
		return nil, ErrRequestTimeout
	case <-c.done:
		return nil, ErrConnectionClosed
	}
}

func (c *Client) SendEvent(envelope *internalpb.InternalEnvelope) error {
	envelope.Kind = internalpb.EnvelopeKind_ENVELOPE_KIND_EVENT
	return c.send(envelope)
}

func (c *Client) Close() {
	c.closeOnce.Do(func() { close(c.done); _ = c.stream.CloseSend() })
}

func (c *Client) Done() <-chan struct{} {
	return c.done
}

func (c *Client) sendHello() error {
	payload, err := proto.Marshal(&internalpb.Hello{ServiceType: c.service, InstanceId: c.instanceID})
	if err != nil {
		return err
	}
	return c.send(&internalpb.InternalEnvelope{Kind: internalpb.EnvelopeKind_ENVELOPE_KIND_HELLO, SourceService: c.service, Payload: payload})
}

func (c *Client) send(envelope *internalpb.InternalEnvelope) error {
	select {
	case <-c.done:
		return ErrConnectionClosed
	default:
	}
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	return c.stream.Send(envelope)
}

func (c *Client) readLoop(ctx context.Context) {
	defer c.Close()
	for {
		envelope, err := c.stream.Recv()
		if err != nil {
			return
		}
		if envelope.Kind == internalpb.EnvelopeKind_ENVELOPE_KIND_RESPONSE || envelope.Kind == internalpb.EnvelopeKind_ENVELOPE_KIND_ERROR {
			c.mu.Lock()
			response := c.requests[envelope.RequestId]
			c.mu.Unlock()
			if response != nil {
				response <- envelope
			}
			continue
		}
		if envelope.Kind == internalpb.EnvelopeKind_ENVELOPE_KIND_EVENT && c.onEvent != nil {
			_ = c.onEvent.Handle(ctx, Peer{}, envelope)
		}
	}
}
