package streaming

import (
	"errors"
	"log/slog"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	internalpb "server/proto/gen/internalpb"
)

type Server struct {
	internalpb.UnimplementedServiceStreamServer
	handler Handler
	mu      sync.RWMutex
	peers   map[string]Peer
}

func NewServer(handler Handler) *Server {
	return &Server{
		handler: handler,
		peers:   make(map[string]Peer),
	}
}

func Register(registrar grpc.ServiceRegistrar, handler Handler) *Server {
	server := NewServer(handler)
	internalpb.RegisterServiceStreamServer(registrar, server)
	return server
}

func (s *Server) Connect(stream internalpb.ServiceStream_ConnectServer) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	peer, err := parseHello(first)
	if err != nil {
		return status.Error(codes.Unauthenticated, err.Error())
	}
	connectedAt := time.Now()
	slog.Info("grpc stream connected", "protocol", "grpc", "peer_service", peer.ServiceType.String(), "peer_instance_id", peer.InstanceID)
	connection := NewConnection(stream.Send)
	peer.Connection = connection
	key := peerKey(peer.ServiceType, peer.InstanceID)
	s.mu.Lock()
	s.peers[key] = peer
	s.mu.Unlock()
	defer func() {
		connection.Close()
		s.mu.Lock()
		delete(s.peers, key)
		s.mu.Unlock()
		slog.Info("grpc stream disconnected", "protocol", "grpc", "peer_service", peer.ServiceType.String(), "peer_instance_id", peer.InstanceID, "duration_ms", time.Since(connectedAt).Milliseconds())
	}()

	for {
		envelope, err := stream.Recv()
		if err != nil {
			return err
		}
		if envelope.Kind == internalpb.EnvelopeKind_ENVELOPE_KIND_HELLO {
			return status.Error(codes.InvalidArgument, "hello is only valid as the first message")
		}
		slog.Info("grpc stream message received", "protocol", "grpc", "peer_service", peer.ServiceType.String(), "peer_instance_id", peer.InstanceID, "target_service", envelope.TargetService.String(), "message_id", envelope.MessageId, "request_id", envelope.RequestId, "kind", envelope.Kind.String())
		if s.handler != nil {
			if err := s.handler.Handle(stream.Context(), peer, envelope); err != nil {
				return err
			}
		}
	}
}

func (s *Server) Peer(serviceType internalpb.ServiceType, instanceID string) (Peer, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	peer, ok := s.peers[peerKey(serviceType, instanceID)]
	return peer, ok
}

func parseHello(envelope *internalpb.InternalEnvelope) (Peer, error) {
	if envelope.Kind != internalpb.EnvelopeKind_ENVELOPE_KIND_HELLO {
		return Peer{}, errors.New("first stream message must be hello")
	}
	hello := &internalpb.Hello{}
	if err := proto.Unmarshal(envelope.Payload, hello); err != nil {
		return Peer{}, errors.New("invalid hello payload")
	}
	if hello.ServiceType == internalpb.ServiceType_SERVICE_TYPE_UNSPECIFIED || hello.InstanceId == "" {
		return Peer{}, errors.New("hello service type and instance id are required")
	}
	return Peer{ServiceType: hello.ServiceType, InstanceID: hello.InstanceId}, nil
}

func peerKey(serviceType internalpb.ServiceType, instanceID string) string {
	return serviceType.String() + "/" + instanceID
}
