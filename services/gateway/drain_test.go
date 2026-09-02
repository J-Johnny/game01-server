package gateway

import (
	"context"
	"testing"
	"time"
)

func TestGatewayDrainDisablesReadinessAndCompletesWithoutConnections(t *testing.T) {
	module := &Module{
		handler:      NewHandler(nil),
		drainTimeout: 10 * time.Millisecond,
	}
	module.ready.Store(true)
	if !module.IsReady() {
		t.Fatal("gateway should initially be ready")
	}
	module.BeginDrain()
	if module.IsReady() {
		t.Fatal("gateway remained ready while draining")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := module.Drain(ctx); err != nil {
		t.Fatalf("drain gateway: %v", err)
	}
}
