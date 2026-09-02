package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"
	"server/common/reliability"
	gatewaypb "server/proto/gen/client"
	"server/services/gateway/session"
)

const (
	writeWait      = 10 * time.Second
	maxMessageSize = 64 * 1024
)

type MessageDispatcher interface {
	Dispatch(context.Context, *Connection, []byte) error
}

type DispatcherFunc func(context.Context, *Connection, []byte) error

func (f DispatcherFunc) Dispatch(ctx context.Context, c *Connection, payload []byte) error {
	return f(ctx, c, payload)
}

type Connection struct {
	ID           string
	ws           *websocket.Conn
	send         chan []byte
	done         chan struct{}
	closeOnce    sync.Once
	mu           sync.RWMutex
	closed       bool
	sessionID    string
	sessionEpoch uint64
	rateLimiter  *reliability.TokenBucket
}

func (c *Connection) Send(payload []byte) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
		return errors.New("connection closed")
	}
	select {
	case c.send <- payload:
		return nil
	default:
		return errors.New("connection send buffer full")
	}
}

func (c *Connection) Close() {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		close(c.done)
		_ = c.ws.Close()
	})
}

func (c *Connection) BindSession(sessionID string, epochs ...uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessionID = sessionID
	if len(epochs) > 0 {
		c.sessionEpoch = epochs[0]
	}
}

func (c *Connection) SessionID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sessionID
}

func (c *Connection) SessionEpoch() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sessionEpoch
}

type Handler struct {
	upgrader           websocket.Upgrader
	dispatch           MessageDispatcher
	sessionManager     *session.Manager
	accepting          func() bool
	connections        sync.Map
	rateLimitBurst     int
	rateLimitRate      float64
	rateLimitObserver  reliability.RateLimitObserver
	errorMapper        *ErrorMapper
	activeConnections  atomic.Int64
	connectionObserver func(int)
}

func NewHandler(dispatch MessageDispatcher, sessionManagers ...*session.Manager) *Handler {
	handler := &Handler{
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			CheckOrigin:     func(*http.Request) bool { return true },
		},
		dispatch: dispatch,
	}
	if len(sessionManagers) > 0 {
		handler.sessionManager = sessionManagers[0]
	}
	handler.rateLimitBurst = 30
	handler.rateLimitRate = 10
	handler.errorMapper = NewErrorMapper(nil)
	return handler
}

func (h *Handler) SetErrorMapper(mapper *ErrorMapper) {
	if mapper != nil {
		h.errorMapper = mapper
	}
}

func (h *Handler) SetConnectionObserver(observer func(int)) {
	h.connectionObserver = observer
}

func (h *Handler) SetRateLimit(burst int, perSecond float64) {
	h.rateLimitBurst = burst
	h.rateLimitRate = perSecond
}

func (h *Handler) SetRateLimitObserver(observer reliability.RateLimitObserver) {
	h.rateLimitObserver = observer
}

func (h *Handler) RegisterRoutes(r gin.IRouter) {
	r.GET("/ws", h.Handle)
}

func (h *Handler) SetAccepting(check func() bool) {
	h.accepting = check
}

func (h *Handler) Handle(c *gin.Context) {
	if h.accepting != nil && !h.accepting() {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	ws, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	ws.SetReadLimit(maxMessageSize)
	conn := &Connection{
		ws:          ws,
		send:        make(chan []byte, 64),
		done:        make(chan struct{}),
		rateLimiter: reliability.NewTokenBucket(h.rateLimitBurst, h.rateLimitRate, h.rateLimitObserver),
	}
	connectionID, err := newConnectionID()
	if err != nil {
		slog.Error("create websocket connection ID", "protocol", "websocket", "request_id", requestID(c), "error", err)
		conn.Close()
		return
	}
	conn.ID = connectionID
	h.connections.Store(connectionID, conn)
	h.observeConnectionCount(h.activeConnections.Add(1))
	defer h.disconnectSession(conn)
	defer h.connections.Delete(connectionID)
	defer h.observeConnectionCount(h.activeConnections.Add(-1))
	slog.Info("websocket connected", "protocol", "websocket", "request_id", requestID(c), "connection_id", connectionID, "client_ip", c.ClientIP())
	defer slog.Info("websocket disconnected", "protocol", "websocket", "request_id", requestID(c), "connection_id", connectionID)
	go h.writePump(conn)
	h.readPump(c, conn)
	conn.Close()
}

func (h *Handler) disconnectSession(conn *Connection) {
	if h.sessionManager == nil || conn == nil {
		return
	}
	sessionID := conn.SessionID()
	if sessionID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := h.sessionManager.DisconnectConnection(ctx, sessionID, conn.ID, time.Now(), conn.SessionEpoch()); err != nil {
		slog.Warn("disconnect websocket session failed", "protocol", "websocket", "connection_id", conn.ID, "session_id", sessionID, "error", err)
	}
}

func newConnectionID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func (h *Handler) readPump(ctx context.Context, conn *Connection) {
	_ = conn.ws.SetReadDeadline(time.Now().Add(30 * time.Second))
	conn.ws.SetPongHandler(func(string) error { return conn.ws.SetReadDeadline(time.Now().Add(30 * time.Second)) })
	for {
		messageType, payload, err := conn.ws.ReadMessage()
		if err != nil {
			return
		}
		if messageType != websocket.BinaryMessage {
			return
		}
		if conn.rateLimiter != nil && !conn.rateLimiter.Allow() {
			slog.Warn("websocket request rate limited", "protocol", "websocket", "connection_id", conn.ID)
			h.sendRateLimited(conn, payload)
			continue
		}
		if h.dispatch != nil {
			if err := h.dispatch.Dispatch(ctx, conn, payload); err != nil {
				slog.Warn("websocket message dispatch failed", "protocol", "websocket", "connection_id", conn.ID, "error", err)
				return
			}
		}
	}
}

func (h *Handler) sendRateLimited(conn *Connection, payload []byte) {
	requestID := uint64(0)
	envelope := &gatewaypb.Envelope{}
	if proto.Unmarshal(payload, envelope) == nil {
		requestID = envelope.RequestId
	}
	retryAfter := time.Duration(float64(time.Second) / h.rateLimitRate)
	publicError := h.errorMapper.Known(ErrorRateLimited, "request rate limit exceeded")
	publicError.RetryAfter = retryAfter
	h.errorMapper.Observe(publicError)
	data, err := proto.Marshal(&gatewaypb.ErrorResponse{
		Code:             publicError.Code,
		Message:          publicError.Message,
		Retryable:        publicError.Retryable,
		RetryAfterMillis: uint64(publicError.RetryAfter.Milliseconds()),
	})
	if err != nil {
		return
	}
	_ = conn.Send(withRequestID(requestID, data))
}

func withRequestID(requestID uint64, payload []byte) []byte {
	data, err := proto.Marshal(&gatewaypb.Envelope{MessageId: gatewaypb.ClientMessageId_CLIENT_MESSAGE_ID_ERROR_RESPONSE, RequestId: requestID, Payload: payload})
	if err != nil {
		return nil
	}
	return data
}

func (h *Handler) observeConnectionCount(count int64) {
	if h.connectionObserver != nil {
		h.connectionObserver(int(count))
	}
}

func (h *Handler) NotifyDraining(reconnectAfter time.Duration) {
	payload, err := proto.Marshal(&gatewaypb.GatewayDrainingEvent{ReconnectAfterMillis: uint64(reconnectAfter.Milliseconds())})
	if err != nil {
		return
	}
	h.connections.Range(func(_, value any) bool {
		connection := value.(*Connection)
		_ = connection.Send(marshalGatewayEvent(gatewaypb.ClientMessageId_CLIENT_MESSAGE_ID_GATEWAY_DRAINING_EVENT, connection.SessionID(), payload))
		return true
	})
}

func (h *Handler) Preempt(record session.Record) {
	value, exists := h.connections.Load(record.ConnectionID)
	if !exists {
		return
	}
	connection := value.(*Connection)
	if connection.SessionID() != record.SessionID || (record.ConnectionEpoch != 0 && connection.SessionEpoch() != record.ConnectionEpoch) {
		return
	}
	payload, err := proto.Marshal(&gatewaypb.SessionPreemptedEvent{
		SessionId:       record.SessionID,
		ConnectionEpoch: record.ConnectionEpoch,
		Reason:          "connection was superseded by a newer login",
	})
	if err == nil {
		_ = connection.Send(marshalGatewayEvent(gatewaypb.ClientMessageId_CLIENT_MESSAGE_ID_SESSION_PREEMPTED_EVENT, record.SessionID, payload))
	}
	go func() {
		timer := time.NewTimer(100 * time.Millisecond)
		defer timer.Stop()
		<-timer.C
		connection.Close()
	}()
}

func (h *Handler) CloseAll() {
	h.connections.Range(func(_, value any) bool {
		value.(*Connection).Close()
		return true
	})
}

func requestID(c *gin.Context) string {
	requestID, _ := c.Get("request_id")
	value, _ := requestID.(string)
	return value
}

func (h *Handler) writePump(conn *Connection) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case payload := <-conn.send:
			_ = conn.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.ws.WriteMessage(websocket.BinaryMessage, payload); err != nil {
				conn.Close()
				return
			}
		case <-ticker.C:
			_ = conn.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				conn.Close()
				return
			}
		case <-conn.done:
			return
		}
	}
}
