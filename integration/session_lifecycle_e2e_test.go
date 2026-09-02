package integration

import (
	"os"
	"testing"
	"time"
)

func TestGatewaySessionLifecycleDelivery(t *testing.T) {
	if os.Getenv("GAME_E2E_GATEWAY_LIFECYCLE") != "1" {
		t.Skip("set GAME_E2E_GATEWAY_LIFECYCLE=1 to run against the local Compose environment")
	}
	if err := waitForHTTPStatus(faultGatewayHealthURL, 204, 30*time.Second); err != nil {
		t.Fatalf("Gateway is not ready: %v", err)
	}
	baseline := metricValueOrZero(faultMetricsURL, "game01_gateway_session_lifecycle_events_total")
	if err := faultLogin(); err != nil {
		t.Fatalf("Gateway login failed: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if delivered := metricValueOrZero(faultMetricsURL, "game01_gateway_session_lifecycle_events_total"); delivered >= baseline+2 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("Gateway did not deliver connected lifecycle events to Lobby and Battle")
}
