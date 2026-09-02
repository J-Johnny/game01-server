package common

import (
	"context"

	"server/common/app"
	"server/common/streaming"
	internalpb "server/proto/gen/internalpb"

	"google.golang.org/protobuf/proto"
)

type Module struct {
	name        string
	serviceType internalpb.ServiceType
	deps        app.Dependencies
	streaming   *streaming.ClientManager
}

func NewModule(name string, serviceType internalpb.ServiceType, deps app.Dependencies) *Module {
	return &Module{
		name:        name,
		serviceType: serviceType,
		deps:        deps,
		streaming:   streaming.NewClientManager(deps.Registry, serviceType, name, deps.Config.App.InstanceID, nil),
	}
}

func (m *Module) Name() string {
	return m.name
}

func (m *Module) Start(ctx context.Context) error {
	m.streaming.Start(ctx, InternalTargets(m.name))
	return nil
}

func (m *Module) Stop(context.Context) error {
	m.streaming.Close()
	return nil
}

func (m *Module) Client(service string) (*streaming.Client, bool) {
	return m.streaming.AnyClient(service)
}

func (m *Module) RegisterInternal(router *streaming.Router) {
	router.Register(m.serviceType, uint32(internalpb.InternalMessageId_INTERNAL_MESSAGE_ID_SERVICE_STATUS_REQUEST), streaming.MessageHandlerFunc(m.serviceStatus))
}

func (m *Module) serviceStatus(_ context.Context, _ streaming.Peer, envelope *internalpb.InternalEnvelope) (*streaming.MessageResult, error) {
	request := &internalpb.ServiceStatusRequest{}
	if err := proto.Unmarshal(envelope.Payload, request); err != nil {
		return nil, err
	}
	return &streaming.MessageResult{
		MessageID: uint32(internalpb.InternalMessageId_INTERNAL_MESSAGE_ID_SERVICE_STATUS_RESPONSE),
		Message: &internalpb.ServiceStatusResponse{
			ServiceType: m.serviceType,
			InstanceId:  m.deps.Config.App.InstanceID,
			Available:   true,
		},
	}, nil
}

func InternalTargets(source string) map[string]internalpb.ServiceType {
	targets := map[string]internalpb.ServiceType{
		"gateway":    internalpb.ServiceType_SERVICE_TYPE_GATEWAY,
		"lobby":      internalpb.ServiceType_SERVICE_TYPE_LOBBY,
		"usercenter": internalpb.ServiceType_SERVICE_TYPE_USERCENTER,
		"battle":     internalpb.ServiceType_SERVICE_TYPE_BATTLE,
	}
	delete(targets, source)
	return targets
}
