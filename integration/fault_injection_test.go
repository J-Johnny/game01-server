package integration

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"
	gatewaypb "server/proto/gen/client"
	"server/services/gateway"
)

const (
	faultGatewayURL       = "ws://127.0.0.1:18081/ws"
	faultGatewayHealthURL = "http://127.0.0.1:18081/healthz"
	faultMetricsURL       = "http://127.0.0.1:18081/metrics"
)

func TestGatewayUserCenterFaultInjection(t *testing.T) {
	if os.Getenv("GAME_E2E_FAULT_INJECTION") != "1" {
		t.Skip("set GAME_E2E_FAULT_INJECTION=1 to run against the local Compose environment")
	}

	serverRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve server root: %v", err)
	}
	if err := waitForHTTPStatus(faultGatewayHealthURL, http.StatusNoContent, 30*time.Second); err != nil {
		t.Fatalf("Gateway is not ready: %v", err)
	}

	if err := faultLogin(); err != nil {
		t.Fatalf("baseline Gateway login failed: %v", err)
	}
	baselineRetries := metricValueOrZero(faultMetricsURL, "game01_reliability_request_retries_total")

	if err := compose(serverRoot, 30*time.Second, "kill", "-s", "KILL", "usercenter"); err != nil {
		t.Fatalf("stop UserCenter: %v", err)
	}
	t.Cleanup(func() {
		if err := compose(serverRoot, 90*time.Second, "up", "-d", "usercenter"); err != nil {
			t.Errorf("restore UserCenter: %v", err)
			return
		}
		if err := waitForHTTPStatus(faultGatewayHealthURL, http.StatusNoContent, 30*time.Second); err != nil {
			t.Errorf("Gateway did not recover after UserCenter restart: %v", err)
		}
	})

	time.Sleep(time.Second)
	results := make(chan error, 3)
	var group sync.WaitGroup
	for i := 0; i < 3; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			results <- faultLogin()
		}()
	}
	group.Wait()
	close(results)
	for err := range results {
		if err == nil {
			t.Fatal("Gateway login unexpectedly succeeded while UserCenter was unavailable")
		}
	}

	if err := waitForMetric(faultMetricsURL, "game01_reliability_circuit_breaker_state", 1, 15*time.Second); err != nil {
		t.Fatalf("circuit breaker did not open: %v", err)
	}
	retriesAfterFault, err := metricValue(faultMetricsURL, "game01_reliability_request_retries_total")
	if err != nil {
		t.Fatalf("read retry metric after fault: %v", err)
	}
	if retriesAfterFault <= baselineRetries {
		t.Fatalf("retry metric did not increase: before=%v after=%v", baselineRetries, retriesAfterFault)
	}

	if err := compose(serverRoot, 90*time.Second, "up", "-d", "usercenter"); err != nil {
		t.Fatalf("restart UserCenter: %v", err)
	}
	if err := waitForHTTPStatus(faultGatewayHealthURL, http.StatusNoContent, 30*time.Second); err != nil {
		t.Fatalf("Gateway did not recover after UserCenter restart: %v", err)
	}
	time.Sleep(6 * time.Second)
	if err := faultLogin(); err != nil {
		t.Fatalf("Gateway login did not recover after UserCenter restart: %v", err)
	}
	if err := waitForMetric(faultMetricsURL, "game01_reliability_circuit_breaker_state", 0, 10*time.Second); err != nil {
		t.Fatalf("circuit breaker did not close after recovery: %v", err)
	}
}

func faultLogin() error {
	connection, _, err := websocket.DefaultDialer.Dial(faultGatewayURL, nil)
	if err != nil {
		return fmt.Errorf("dial Gateway: %w", err)
	}
	defer connection.Close()

	payload, err := proto.Marshal(&gatewaypb.LoginRequest{
		Provider:       gatewaypb.AuthProvider_AUTH_PROVIDER_GUEST,
		InstallId:      "fault-injection-" + uuid.NewString(),
		IdempotencyKey: uuid.NewString(),
	})
	if err != nil {
		return fmt.Errorf("marshal login request: %w", err)
	}
	envelope, err := proto.Marshal(&gatewaypb.Envelope{
		MessageId: gateway.MessageLoginRequest,
		RequestId: 1,
		Payload:   payload,
	})
	if err != nil {
		return fmt.Errorf("marshal login envelope: %w", err)
	}
	if err := connection.WriteMessage(websocket.BinaryMessage, envelope); err != nil {
		return fmt.Errorf("write login request: %w", err)
	}
	_ = connection.SetReadDeadline(time.Now().Add(10 * time.Second))
	for {
		_, responseBytes, err := connection.ReadMessage()
		if err != nil {
			return fmt.Errorf("read login response: %w", err)
		}
		response := &gatewaypb.Envelope{}
		if err := proto.Unmarshal(responseBytes, response); err != nil {
			return fmt.Errorf("unmarshal response envelope: %w", err)
		}
		if response.RequestId != 1 {
			continue
		}
		if response.MessageId == gateway.MessageLoginResponse {
			return nil
		}
		if response.MessageId != gateway.MessageErrorResponse {
			return fmt.Errorf("unexpected Gateway message: %s", response.MessageId)
		}
		errorResponse := &gatewaypb.ErrorResponse{}
		if err := proto.Unmarshal(response.Payload, errorResponse); err != nil {
			return fmt.Errorf("unmarshal error response: %w", err)
		}
		return fmt.Errorf("Gateway login failed: code=%d message=%s", errorResponse.Code, errorResponse.Message)
	}
}

func compose(serverRoot string, timeout time.Duration, args ...string) error {
	runContext, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	command := exec.CommandContext(runContext, "docker", append([]string{"compose", "-f", filepath.Join(serverRoot, "docker-compose.yml")}, args...)...)
	command.Dir = serverRoot
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker compose %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func metricValue(metricsURL string, metricName string) (float64, error) {
	response, err := http.Get(metricsURL)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("metrics endpoint status=%d", response.StatusCode)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return 0, err
	}
	var total float64
	found := false
	for _, line := range bytes.Split(body, []byte("\n")) {
		fields := strings.Fields(string(line))
		if len(fields) != 2 || !strings.HasPrefix(fields[0], metricName) {
			continue
		}
		value, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			return 0, err
		}
		total += value
		found = true
	}
	if !found {
		return 0, fmt.Errorf("metric %s not found", metricName)
	}
	return total, nil
}

func metricValueOrZero(metricsURL string, metricName string) float64 {
	value, err := metricValue(metricsURL, metricName)
	if err != nil {
		return 0
	}
	return value
}

func waitForMetric(metricsURL string, metricName string, expected float64, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		value, err := metricValue(metricsURL, metricName)
		if err == nil && value == expected {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("metric %s did not become %v", metricName, expected)
}

func waitForHTTPStatus(url string, expected int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		response, err := http.Get(url)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == expected {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("%s did not return status %d", url, expected)
}
