package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
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
	ID        string
	ws        *websocket.Conn
	send      chan []byte
	done      chan struct{}
	closeOnce sync.Once
	mu        sync.RWMutex
	closed    bool
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

type Handler struct {
	upgrader    websocket.Upgrader
	dispatch    MessageDispatcher
	connections sync.Map
}

func NewHandler(dispatch MessageDispatcher) *Handler {
	return &Handler{
		upgrader: websocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 4096, CheckOrigin: func(*http.Request) bool { return true }},
		dispatch: dispatch,
	}
}

func (h *Handler) RegisterRoutes(r gin.IRouter) {
	r.GET("/ws", h.Handle)
}

func (h *Handler) Handle(c *gin.Context) {
	ws, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	ws.SetReadLimit(maxMessageSize)
	conn := &Connection{ws: ws, send: make(chan []byte, 64), done: make(chan struct{})}
	connectionID, err := newConnectionID()
	if err != nil {
		slog.Error("create websocket connection ID", "protocol", "websocket", "request_id", requestID(c), "error", err)
		conn.Close()
		return
	}
	conn.ID = connectionID
	h.connections.Store(connectionID, conn)
	defer h.connections.Delete(connectionID)
	slog.Info("websocket connected", "protocol", "websocket", "request_id", requestID(c), "connection_id", connectionID, "client_ip", c.ClientIP())
	defer slog.Info("websocket disconnected", "protocol", "websocket", "request_id", requestID(c), "connection_id", connectionID)
	go h.writePump(conn)
	h.readPump(c, conn)
	conn.Close()
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
		if h.dispatch != nil {
			if err := h.dispatch.Dispatch(ctx, conn, payload); err != nil {
				slog.Warn("websocket message dispatch failed", "protocol", "websocket", "connection_id", conn.ID, "error", err)
				return
			}
		}
	}
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
