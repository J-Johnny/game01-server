package gateway

import (
	"context"
	"errors"
	"net"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
	"server/common/streaming"
	gatewaypb "server/proto/gen/client"
	internalpb "server/proto/gen/internalpb"
	"server/services/gateway/session"
	"server/services/usercenter/components"
	"server/services/usercenter/repository"
	"server/services/usercenter/repository/models"
)

func TestPasswordLoginOverWebSocketThroughUserCenterStreaming(t *testing.T) {
	grpcServer, grpcAddress := startUserCenterForIntegration(t)
	defer grpcServer.Stop()

	clientContext, cancelClient := context.WithCancel(context.Background())
	defer cancelClient()
	connection, err := grpc.DialContext(clientContext, grpcAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial user center: %v", err)
	}
	defer connection.Close()

	streamClient, err := streaming.NewClient(clientContext, connection, internalpb.ServiceType_SERVICE_TYPE_GATEWAY, "gateway-integration", nil)
	if err != nil {
		t.Fatalf("create streaming client: %v", err)
	}
	defer streamClient.Close()

	authenticator := NewUserCenterAuthenticator(func() (*streaming.Client, bool) {
		return streamClient, true
	})
	dispatcher := NewDispatcher(authenticator, session.NewManager(session.NewMemoryStore(), "gateway-integration", time.Hour, time.Minute))
	handler := NewHandler(dispatcher)
	router := gin.New()
	handler.RegisterRoutes(router)
	httpServer := httptest.NewServer(router)
	defer httpServer.Close()

	wsURL := "ws" + httpServer.URL[len("http"):]
	ws, _, err := websocket.DefaultDialer.Dial(wsURL+"/ws", nil)
	if err != nil {
		t.Fatalf("dial gateway websocket: %v", err)
	}

	loginRequest := gatewaypb.LoginRequest{Provider: gatewaypb.AuthProvider_AUTH_PROVIDER_PASSWORD, Username: "integration-user", Password: "correct-password", InstallId: "install-integration"}
	loginPayload, err := proto.Marshal(&loginRequest)
	if err != nil {
		t.Fatal(err)
	}
	request := gatewayEnvelopeLogin(loginPayload, 42)
	requestBytes, err := proto.Marshal(&request)
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteMessage(websocket.BinaryMessage, requestBytes); err != nil {
		t.Fatalf("write login request: %v", err)
	}

	_, responseBytes, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("read login response: %v", err)
	}
	responseEnvelope := &gatewaypb.Envelope{}
	if err := proto.Unmarshal(responseBytes, responseEnvelope); err != nil {
		t.Fatalf("unmarshal login response envelope: %v", err)
	}
	if responseEnvelope.MessageId != MessageLoginResponse || responseEnvelope.RequestId != 42 || responseEnvelope.SessionId == "" {
		t.Fatalf("unexpected login envelope: %s", responseEnvelope)
	}
	loginResponse := &gatewaypb.LoginResponse{}
	if err := proto.Unmarshal(responseEnvelope.Payload, loginResponse); err != nil {
		t.Fatalf("unmarshal login response: %v", err)
	}
	if loginResponse.AccountId == "" || loginResponse.SessionId != responseEnvelope.SessionId || loginResponse.ResumeToken == "" || loginResponse.RefreshToken == "" {
		t.Fatalf("incomplete login response: %s", loginResponse)
	}
	if loginResponse.RefreshTokenExpireAtUnix <= time.Now().Unix() {
		t.Fatalf("refresh token expiration is not in the future: %d", loginResponse.RefreshTokenExpireAtUnix)
	}
	_ = ws.Close()
}

func TestLoginOverWebSocketReturnsAuthenticationError(t *testing.T) {
	grpcServer, grpcAddress := startUserCenterForIntegration(t)
	defer grpcServer.Stop()

	clientContext, cancelClient := context.WithCancel(context.Background())
	defer cancelClient()
	connection, err := grpc.DialContext(clientContext, grpcAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial user center: %v", err)
	}
	defer connection.Close()
	streamClient, err := streaming.NewClient(clientContext, connection, internalpb.ServiceType_SERVICE_TYPE_GATEWAY, "gateway-integration-error", nil)
	if err != nil {
		t.Fatalf("create streaming client: %v", err)
	}
	defer streamClient.Close()

	dispatcher := NewDispatcher(NewUserCenterAuthenticator(func() (*streaming.Client, bool) {
		return streamClient, true
	}), session.NewManager(session.NewMemoryStore(), "gateway-integration", time.Hour, time.Minute))
	router := gin.New()
	NewHandler(dispatcher).RegisterRoutes(router)
	httpServer := httptest.NewServer(router)
	defer httpServer.Close()
	wsURL := "ws" + httpServer.URL[len("http"):]
	ws, _, err := websocket.DefaultDialer.Dial(wsURL+"/ws", nil)
	if err != nil {
		t.Fatalf("dial gateway websocket: %v", err)
	}
	defer ws.Close()

	loginPayload, err := proto.Marshal(&gatewaypb.LoginRequest{Provider: gatewaypb.AuthProvider_AUTH_PROVIDER_PASSWORD, Username: "bad", Password: "correct-password", InstallId: "install-integration"})
	if err != nil {
		t.Fatal(err)
	}
	requestBytes, err := proto.Marshal(&gatewaypb.Envelope{MessageId: MessageLoginRequest, RequestId: 6, Payload: loginPayload})
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteMessage(websocket.BinaryMessage, requestBytes); err != nil {
		t.Fatalf("write login request: %v", err)
	}
	if _, _, err := ws.ReadMessage(); err != nil {
		t.Fatalf("read initial password login response: %v", err)
	}

	loginPayload, err = proto.Marshal(&gatewaypb.LoginRequest{Provider: gatewaypb.AuthProvider_AUTH_PROVIDER_PASSWORD, Username: "bad", Password: "wrong-password", InstallId: "install-integration"})
	if err != nil {
		t.Fatal(err)
	}
	requestBytes, err = proto.Marshal(&gatewaypb.Envelope{MessageId: MessageLoginRequest, RequestId: 7, Payload: loginPayload})
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteMessage(websocket.BinaryMessage, requestBytes); err != nil {
		t.Fatalf("write invalid password request: %v", err)
	}
	_, responseBytes, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("read error response: %v", err)
	}
	responseEnvelope := &gatewaypb.Envelope{}
	if err := proto.Unmarshal(responseBytes, responseEnvelope); err != nil {
		t.Fatalf("unmarshal error envelope: %v", err)
	}
	if responseEnvelope.MessageId != MessageErrorResponse || responseEnvelope.RequestId != 7 {
		t.Fatalf("unexpected error envelope: %s", responseEnvelope)
	}
	errorResponse := &gatewaypb.ErrorResponse{}
	if err := proto.Unmarshal(responseEnvelope.Payload, errorResponse); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if errorResponse.Code != ErrorAuthentication || errorResponse.Message != "authentication failed" {
		t.Fatalf("unexpected authentication error: %s", errorResponse)
	}
}

func startUserCenterForIntegration(t *testing.T) (*grpc.Server, string) {
	t.Helper()
	accounts := &integrationAccountRepository{tokens: make(map[string][]models.RefreshToken)}
	auth := components.NewAuthComponent(accounts, time.Hour)
	router := streaming.NewRouter()
	auth.RegisterInternal(router)
	grpcServer := grpc.NewServer()
	streaming.Register(grpcServer, router)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen user center: %v", err)
	}
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(func() { _ = listener.Close() })
	return grpcServer, listener.Addr().String()
}

type integrationAccountRepository struct {
	mu       sync.Mutex
	accounts []models.Account
	tokens   map[string][]models.RefreshToken
}

func (r *integrationAccountRepository) EnsureIndexes(context.Context) error { return nil }

func (r *integrationAccountRepository) Create(_ context.Context, account *models.Account) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.accounts {
		if existing.AccountID == account.AccountID {
			return errors.New("duplicate account")
		}
	}
	r.accounts = append(r.accounts, *account)
	return nil
}

func (r *integrationAccountRepository) FindByID(_ context.Context, accountID string) (models.Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, account := range r.accounts {
		if account.AccountID == accountID {
			return account, nil
		}
	}
	return models.Account{}, models.ErrAccountNotFound
}

func (r *integrationAccountRepository) FindByIdentity(_ context.Context, provider models.AuthProvider, subject string) (models.Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, account := range r.accounts {
		if account.Identities == nil || account.Identities.Data() == nil || account.Identities.Data().Rows == nil {
			continue
		}
		for _, identity := range account.Identities.Data().Rows.GetValueSlice() {
			if identity != nil && identity.Provider == provider && identity.Subject == subject {
				return account, nil
			}
		}
	}
	return models.Account{}, models.ErrAccountNotFound
}

func (r *integrationAccountRepository) FindByRefreshTokenHash(_ context.Context, tokenHash string, now time.Time) (models.Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, account := range r.accounts {
		for _, token := range r.tokens[account.AccountID] {
			if token.TokenHash == tokenHash && token.RevokedAt == nil && now.Before(token.ExpiresAt) {
				return account, nil
			}
		}
	}
	return models.Account{}, models.ErrAccountNotFound
}

func (r *integrationAccountRepository) LinkPlayer(context.Context, string, string, time.Time) error {
	return nil
}

func (r *integrationAccountRepository) StoreRefreshToken(_ context.Context, accountID string, token models.RefreshToken, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, account := range r.accounts {
		if account.AccountID == accountID {
			r.tokens[accountID] = append(r.tokens[accountID], token)
			return nil
		}
	}
	return models.ErrAccountNotFound
}

func (r *integrationAccountRepository) RevokeRefreshToken(_ context.Context, accountID, tokenHash string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for index := range r.tokens[accountID] {
		if r.tokens[accountID][index].TokenHash == tokenHash {
			r.tokens[accountID][index].RevokedAt = &now
			return nil
		}
	}
	return models.ErrAccountNotFound
}

func (r *integrationAccountRepository) RotateRefreshToken(_ context.Context, accountID, tokenHash, installID string, now time.Time, replacement models.RefreshToken) (models.Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, account := range r.accounts {
		if account.AccountID != accountID {
			continue
		}
		for index := range r.tokens[accountID] {
			token := &r.tokens[accountID][index]
			if token.TokenHash == tokenHash && token.InstallID == installID && token.RevokedAt == nil && now.Before(token.ExpiresAt) {
				token.RevokedAt = &now
				r.tokens[accountID] = append(r.tokens[accountID], replacement)
				return account, nil
			}
		}
	}
	return models.Account{}, models.ErrAccountNotFound
}

func gatewayLoginRequestGuest(installID string) gatewaypb.LoginRequest {
	return gatewaypb.LoginRequest{Provider: gatewaypb.AuthProvider_AUTH_PROVIDER_GUEST, InstallId: installID}
}

func gatewayEnvelopeLogin(payload []byte, requestID uint64) gatewaypb.Envelope {
	return gatewaypb.Envelope{MessageId: MessageLoginRequest, RequestId: requestID, Payload: payload}
}

var _ repository.IAccountRepository = (*integrationAccountRepository)(nil)
