package components

import (
	"context"

	"server/common/streaming"
	internalpb "server/proto/gen/internalpb"
)

// ClientAuthComponent owns the UserCenter API consumed by Gateway for public
// login and refresh requests.
type ClientAuthComponent struct {
	domainAuth *DomainAuthComponent
}

func NewClientAuthComponent(domainAuth *DomainAuthComponent) *ClientAuthComponent {
	return &ClientAuthComponent{
		domainAuth: domainAuth,
	}
}

func (c *ClientAuthComponent) RegisterInternal(router *streaming.Router) {
	router.Register(internalpb.ServiceType_SERVICE_TYPE_USERCENTER, uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_CLIENT_LOGIN_AUTHENTICATE_REQUEST), streaming.MessageHandlerFunc(c.clientLoginAuthenticate))
	router.Register(internalpb.ServiceType_SERVICE_TYPE_USERCENTER, uint32(internalpb.UserCenterMessageId_USER_CENTER_MESSAGE_ID_CLIENT_REFRESH_AUTHENTICATE_REQUEST), streaming.MessageHandlerFunc(c.clientRefreshAuthenticate))
}

func (c *ClientAuthComponent) clientLoginAuthenticate(ctx context.Context, peer streaming.Peer, envelope *internalpb.InternalEnvelope) (*streaming.MessageResult, error) {
	return c.domainAuth.clientLoginAuthenticate(ctx, peer, envelope)
}

func (c *ClientAuthComponent) clientRefreshAuthenticate(ctx context.Context, peer streaming.Peer, envelope *internalpb.InternalEnvelope) (*streaming.MessageResult, error) {
	return c.domainAuth.clientRefreshAuthenticate(ctx, peer, envelope)
}
