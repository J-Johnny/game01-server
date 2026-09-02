package streaming

import (
	"context"
	"errors"
	"sync"

	internalpb "server/proto/gen/internalpb"
)

var ErrConnectionClosed = errors.New("stream connection closed")

type Connection struct {
	send      func(*internalpb.InternalEnvelope) error
	done      chan struct{}
	closeOnce sync.Once
	sendMu    sync.Mutex
}

func NewConnection(send func(*internalpb.InternalEnvelope) error) *Connection {
	return &Connection{
		send: send,
		done: make(chan struct{}),
	}
}

func (c *Connection) Send(envelope *internalpb.InternalEnvelope) error {
	select {
	case <-c.done:
		return ErrConnectionClosed
	default:
		c.sendMu.Lock()
		defer c.sendMu.Unlock()
		return c.send(envelope)
	}
}

func (c *Connection) Done() <-chan struct{} {
	return c.done
}

func (c *Connection) Close() {
	c.closeOnce.Do(func() { close(c.done) })
}

type Handler interface {
	Handle(context.Context, Peer, *internalpb.InternalEnvelope) error
}

type HandlerFunc func(context.Context, Peer, *internalpb.InternalEnvelope) error

func (f HandlerFunc) Handle(ctx context.Context, peer Peer, envelope *internalpb.InternalEnvelope) error {
	return f(ctx, peer, envelope)
}

type Peer struct {
	ServiceType internalpb.ServiceType
	InstanceID  string
	Connection  *Connection
}
