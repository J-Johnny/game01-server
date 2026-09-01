package common

import (
	"context"
	"testing"

	"google.golang.org/protobuf/proto"
	"server/common/app"
	"server/common/config"
	"server/common/discovery/static"
	"server/common/streaming"
	internalpb "server/proto/gen/internalpb"
)

func TestInternalStatusRoutesToEachServiceModule(t *testing.T) {
	deps := app.Dependencies{Config: config.Defaults(), Registry: static.New(nil)}
	router := streaming.NewRouter()
	modules := []*Module{
		NewModule("gateway", internalpb.ServiceType_SERVICE_TYPE_GATEWAY, deps),
		NewModule("lobby", internalpb.ServiceType_SERVICE_TYPE_LOBBY, deps),
		NewModule("usercenter", internalpb.ServiceType_SERVICE_TYPE_USERCENTER, deps),
		NewModule("battle", internalpb.ServiceType_SERVICE_TYPE_BATTLE, deps),
	}
	for _, module := range modules {
		module.RegisterInternal(router)
	}
	for _, module := range modules {
		response := make(chan *internalpb.InternalEnvelope, 1)
		peer := streaming.Peer{ServiceType: internalpb.ServiceType_SERVICE_TYPE_GATEWAY, Connection: streaming.NewConnection(func(envelope *internalpb.InternalEnvelope) error { response <- envelope; return nil })}
		payload, err := proto.Marshal(&internalpb.ServiceStatusRequest{})
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		err = router.Handle(context.Background(), peer, &internalpb.InternalEnvelope{Kind: internalpb.EnvelopeKind_ENVELOPE_KIND_REQUEST, RequestId: 1, TargetService: module.serviceType, MessageId: uint32(internalpb.InternalMessageId_INTERNAL_MESSAGE_ID_SERVICE_STATUS_REQUEST), Payload: payload})
		if err != nil {
			t.Fatalf("route %s: %v", module.Name(), err)
		}
		envelope := <-response
		status := &internalpb.ServiceStatusResponse{}
		if err := proto.Unmarshal(envelope.Payload, status); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if status.ServiceType != module.serviceType {
			t.Fatalf("service type = %s, want %s", status.ServiceType, module.serviceType)
		}
	}
}
