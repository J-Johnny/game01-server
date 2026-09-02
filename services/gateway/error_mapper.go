package gateway

import (
	"context"
	"errors"
	"time"

	"server/common/observability"
	"server/common/streaming"
	gatewaypb "server/proto/gen/client"
	"server/services/gateway/session"
)

const (
	ErrorInvalidEnvelope gatewaypb.GatewayErrorCode = gatewaypb.GatewayErrorCode_GATEWAY_ERROR_CODE_INVALID_REQUEST
	ErrorAuthentication  gatewaypb.GatewayErrorCode = gatewaypb.GatewayErrorCode_GATEWAY_ERROR_CODE_AUTHENTICATION_FAILED
	ErrorResume          gatewaypb.GatewayErrorCode = gatewaypb.GatewayErrorCode_GATEWAY_ERROR_CODE_SESSION_INVALID
	ErrorUnsupported     gatewaypb.GatewayErrorCode = gatewaypb.GatewayErrorCode_GATEWAY_ERROR_CODE_UNSUPPORTED_MESSAGE
	ErrorRefresh         gatewaypb.GatewayErrorCode = gatewaypb.GatewayErrorCode_GATEWAY_ERROR_CODE_REFRESH_FAILED
	ErrorPlayer          gatewaypb.GatewayErrorCode = gatewaypb.GatewayErrorCode_GATEWAY_ERROR_CODE_PLAYER_UNAVAILABLE
	ErrorService         gatewaypb.GatewayErrorCode = gatewaypb.GatewayErrorCode_GATEWAY_ERROR_CODE_SERVICE_UNAVAILABLE
	ErrorRateLimited     gatewaypb.GatewayErrorCode = gatewaypb.GatewayErrorCode_GATEWAY_ERROR_CODE_RATE_LIMITED
	ErrorDraining        gatewaypb.GatewayErrorCode = gatewaypb.GatewayErrorCode_GATEWAY_ERROR_CODE_GATEWAY_DRAINING
	ErrorInternal        gatewaypb.GatewayErrorCode = gatewaypb.GatewayErrorCode_GATEWAY_ERROR_CODE_INTERNAL
)

type PublicError struct {
	Code       gatewaypb.GatewayErrorCode
	Message    string
	Retryable  bool
	RetryAfter time.Duration
}

func (e PublicError) Response() *gatewaypb.ErrorResponse {
	return &gatewaypb.ErrorResponse{
		Code:             e.Code,
		Message:          e.Message,
		Retryable:        e.Retryable,
		RetryAfterMillis: uint64(e.RetryAfter.Milliseconds()),
	}
}

type ErrorMapper struct {
	metrics           *observability.Metrics
	serviceRetryAfter time.Duration
}

func NewErrorMapper(metrics *observability.Metrics) *ErrorMapper {
	return &ErrorMapper{
		metrics:           metrics,
		serviceRetryAfter: time.Second,
	}
}

func (m *ErrorMapper) Known(code gatewaypb.GatewayErrorCode, message string) PublicError {
	result := PublicError{Code: code, Message: message}
	switch code {
	case ErrorService:
		result.Retryable = true
		result.RetryAfter = m.serviceRetryAfter
	case ErrorRateLimited:
		result.Retryable = true
	}
	return result
}

func (m *ErrorMapper) Map(err error, fallback gatewaypb.GatewayErrorCode, fallbackMessage string) PublicError {
	if errors.Is(err, ErrUserCenterUnavailable) || errors.Is(err, streaming.ErrConnectionClosed) || errors.Is(err, streaming.ErrRequestTimeout) || errors.Is(err, context.DeadlineExceeded) {
		return m.Known(ErrorService, "required service is temporarily unavailable")
	}
	if errors.Is(err, session.ErrNotFound) || errors.Is(err, session.ErrInvalidToken) || errors.Is(err, session.ErrSessionExpiry) || errors.Is(err, session.ErrSessionConflict) {
		return m.Known(ErrorResume, "session is invalid or expired")
	}
	if fallback == ErrorInternal {
		return m.Known(ErrorInternal, "internal server error")
	}
	return m.Known(fallback, fallbackMessage)
}

func (m *ErrorMapper) Observe(publicError PublicError) {
	if m != nil && m.metrics != nil {
		m.metrics.ObserveGatewayError(publicError.Code.String(), publicError.Retryable)
	}
}
