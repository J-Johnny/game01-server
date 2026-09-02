package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"time"

	"server/common/app"
	"server/common/reliability"
	"server/common/streaming"
	gatewaypb "server/proto/gen/client"
	internalpb "server/proto/gen/internalpb"
	servicecommon "server/services/common"
	"server/services/gateway/session"

	"github.com/gin-gonic/gin"
	"google.golang.org/protobuf/proto"
)

type Module struct {
	*servicecommon.Module
	handler       *Handler
	ready         atomic.Bool
	preemptCancel context.CancelFunc
}

type preemptEvent struct {
	ConnectionID string `json:"connection_id"`
	SessionID    string `json:"session_id"`
	AccountID    string `json:"account_id"`
}

func NewModule(deps app.Dependencies) *Module {
	base := servicecommon.NewModule("gateway", internalpb.ServiceType_SERVICE_TYPE_GATEWAY, deps)
	store := session.NewRedisStore(deps.Redis, "game01:gateway:session:")
	manager := session.NewManager(store, deps.Config.App.InstanceID, deps.Config.Gateway.SessionTTL, deps.Config.Gateway.ReconnectGrace)
	authenticator := NewUserCenterAuthenticator(func() (*streaming.Client, bool) {
		return base.Client("usercenter")
	})
	authenticator.SetReliability(reliability.RetryPolicy{
		MaxAttempts:  deps.Config.Gateway.RetryAttempts,
		InitialDelay: 50 * time.Millisecond,
		MaxDelay:     500 * time.Millisecond,
		Observer:     deps.Metrics.RetryObserver("gateway", "usercenter", "authenticate", classifyUserCenterError),
		ShouldRetry: func(err error) bool {
			return errors.Is(err, ErrUserCenterUnavailable) || errors.Is(err, streaming.ErrConnectionClosed) || errors.Is(err, streaming.ErrRequestTimeout)
		},
	}, reliability.NewCircuitBreaker(deps.Config.Gateway.CircuitFailures, deps.Config.Gateway.CircuitReset, deps.Metrics.CircuitObserver("gateway", "usercenter", "authenticate")))
	players := NewLobbyPlayerResolver(func() (*streaming.Client, bool) {
		return base.Client("lobby")
	})
	dispatcher := NewDispatcher(authenticator, manager, players)
	module := &Module{
		Module:  base,
		handler: NewHandler(dispatcher, manager),
	}
	dispatcher.SetRestoreHandler(module.restoreState)
	manager.SetPreemptHandler(func(record session.Record) {
		if connection, ok := module.handler.connections.Load(record.ConnectionID); ok {
			connection.(*Connection).Close()
		}
		event, _ := json.Marshal(preemptEvent{ConnectionID: record.ConnectionID, SessionID: record.SessionID, AccountID: record.AccountID})
		_ = deps.Redis.Publish(context.Background(), "game01:gateway:preempt", event).Err()
	})
	module.handler.SetRateLimit(deps.Config.Gateway.RateLimitBurst, deps.Config.Gateway.RateLimitPerSecond)
	module.handler.SetRateLimitObserver(deps.Metrics.RateLimitObserver("websocket_connection"))
	module.handler.SetAccepting(module.IsReady)
	return module
}

func classifyUserCenterError(err error) string {
	if errors.Is(err, ErrUserCenterUnavailable) {
		return "service_unavailable"
	}
	if errors.Is(err, streaming.ErrConnectionClosed) {
		return "connection_closed"
	}
	if errors.Is(err, streaming.ErrRequestTimeout) {
		return "request_timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "context_canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "context_deadline_exceeded"
	}
	return "error"
}

func (m *Module) restoreState(ctx context.Context, connection *Connection, playerID, sessionID string, stateVersions map[string]uint64) {
	if playerID == "" || connection == nil {
		return
	}
	for _, target := range []struct {
		name    string
		service internalpb.ServiceType
	}{{"lobby", internalpb.ServiceType_SERVICE_TYPE_LOBBY}, {"battle", internalpb.ServiceType_SERVICE_TYPE_BATTLE}} {
		client, ok := m.Module.Client(target.name)
		if !ok {
			continue
		}
		payload, err := proto.Marshal(&internalpb.RestorePlayerStateRequest{
			PlayerId:         playerID,
			SessionId:        sessionID,
			LastStateVersion: stateVersions[target.name],
			AllowIncremental: target.name == "battle",
		})
		if err != nil {
			continue
		}
		response, err := client.Request(ctx, &internalpb.InternalEnvelope{
			TargetService: target.service,
			MessageId:     uint32(internalpb.InternalMessageId_INTERNAL_MESSAGE_ID_RESTORE_PLAYER_STATE_REQUEST),
			PlayerId:      playerID,
			Payload:       payload})
		if err != nil || response.Kind != internalpb.EnvelopeKind_ENVELOPE_KIND_RESPONSE {
			continue
		}
		state := &internalpb.RestorePlayerStateResponse{}
		if proto.Unmarshal(response.Payload, state) != nil {
			continue
		}
		event, err := proto.Marshal(&gatewaypb.StateRestoreEvent{
			Service:          target.name,
			PlayerId:         state.PlayerId,
			StateVersion:     state.StateVersion,
			Snapshot:         state.Snapshot,
			Available:        state.Available,
			Mode:             gatewaypb.RestoreMode(state.Mode),
			BaseStateVersion: state.BaseStateVersion,
			PayloadType:      gatewaypb.StatePayloadType(state.PayloadType),
			SchemaVersion:    state.SchemaVersion,
		})
		if err == nil {
			_ = connection.Send(marshalGatewayEvent(gatewaypb.ClientMessageId_CLIENT_MESSAGE_ID_STATE_RESTORE_EVENT, sessionID, event))
		}
	}
}

func marshalGatewayEvent(messageID gatewaypb.ClientMessageId, sessionID string, payload []byte) []byte {
	data, _ := proto.Marshal(&gatewaypb.Envelope{MessageId: messageID, SessionId: sessionID, Payload: payload})
	return data
}

func (m *Module) RegisterRoutes(router gin.IRouter) {
	router.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "pong"})
	})
	router.GET("/readyz", func(c *gin.Context) {
		if !m.IsReady() {
			c.Status(http.StatusServiceUnavailable)
			return
		}
		c.Status(http.StatusNoContent)
	})
	m.handler.RegisterRoutes(router)
}

func (m *Module) Start(ctx context.Context) error {
	if err := m.Module.Start(ctx); err != nil {
		return err
	}
	m.ready.Store(true)
	preemptCtx, cancel := context.WithCancel(ctx)
	m.preemptCancel = cancel
	go m.watchPreempt(preemptCtx)
	go func() {
		<-ctx.Done()
		m.ready.Store(false)
	}()
	return nil
}

func (m *Module) Stop(ctx context.Context) error {
	m.ready.Store(false)
	if m.preemptCancel != nil {
		m.preemptCancel()
	}
	return m.Module.Stop(ctx)
}

func (m *Module) watchPreempt(ctx context.Context) {
	client := m.Module.Redis()
	if client == nil {
		return
	}
	sub := client.Subscribe(ctx, "game01:gateway:preempt")
	defer sub.Close()
	channel := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case message, ok := <-channel:
			if !ok {
				return
			}
			var event preemptEvent
			if json.Unmarshal([]byte(message.Payload), &event) == nil {
				if connection, exists := m.handler.connections.Load(event.ConnectionID); exists {
					connection.(*Connection).Close()
				}
			}
		}
	}
}

func (m *Module) IsReady() bool {
	return m.ready.Load()
}
