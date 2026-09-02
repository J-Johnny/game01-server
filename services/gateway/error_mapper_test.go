package gateway

import (
	"context"
	"testing"

	"server/common/streaming"
	"server/services/gateway/session"
)

func TestErrorMapperDoesNotExposeInternalErrors(t *testing.T) {
	mapper := NewErrorMapper(nil)
	serviceError := mapper.Map(streaming.ErrRequestTimeout, ErrorAuthentication, "authentication failed")
	if serviceError.Code != ErrorService || !serviceError.Retryable || serviceError.Message == "authentication failed" {
		t.Fatalf("unexpected service error: %+v", serviceError)
	}
	sessionError := mapper.Map(session.ErrInvalidToken, ErrorInternal, "internal server error")
	if sessionError.Code != ErrorResume || sessionError.Retryable {
		t.Fatalf("unexpected session error: %+v", sessionError)
	}
	internalError := mapper.Map(context.Canceled, ErrorInternal, "internal server error")
	if internalError.Code != ErrorInternal || internalError.Message != "internal server error" {
		t.Fatalf("unexpected fallback error: %+v", internalError)
	}
}
