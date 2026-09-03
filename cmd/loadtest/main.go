package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"
	gatewaypb "server/proto/gen/client"
)

type counters struct {
	connected      atomic.Int64
	connectOK      atomic.Int64
	connectFailed  atomic.Int64
	requests       atomic.Int64
	success        atomic.Int64
	failures       atomic.Int64
	resumes        atomic.Int64
	resumeSuccess  atomic.Int64
	resumeFailures atomic.Int64
	mu             sync.Mutex
	latencies      []time.Duration
}

func (c *counters) observe(start time.Time, ok bool) {
	c.requests.Add(1)
	if ok {
		c.success.Add(1)
	} else {
		c.failures.Add(1)
	}
	c.mu.Lock()
	if len(c.latencies) < 100000 {
		c.latencies = append(c.latencies, time.Since(start))
	}
	c.mu.Unlock()
}

func main() {
	target := flag.String("target", "ws://127.0.0.1:8080/ws", "Gateway WebSocket URL")
	scenario := flag.String("scenario", "guest-login", "connect-hold, guest-login, password-login, resume-storm")
	connections := flag.Int("connections", 10, "target concurrent connections")
	ramp := flag.Int("ramp-per-second", 10, "new connections per second")
	duration := flag.Duration("duration", time.Minute, "test duration")
	usernamePrefix := flag.String("username-prefix", "perf", "password login username prefix")
	password := flag.String("password", "PerfPassword123!", "password login password")
	interval := flag.Duration("resume-interval", 5*time.Second, "resume storm reconnect interval")
	flag.Parse()
	if *connections <= 0 || *ramp <= 0 || *duration <= 0 {
		log.Fatal("connections, ramp-per-second and duration must be positive")
	}
	if !validScenario(*scenario) {
		log.Fatalf("unsupported scenario %q", *scenario)
	}

	var stats counters
	deadline := time.Now().Add(*duration)
	stop := make(chan struct{})
	go report(&stats, stop)

	var wg sync.WaitGroup
	for i := 0; i < *connections; i++ {
		wait := time.Duration(float64(time.Second) / float64(*ramp))
		if i > 0 {
			time.Sleep(wait)
		}
		if time.Now().After(deadline) {
			break
		}
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			runClient(*target, *scenario, index, *usernamePrefix, *password, *interval, deadline, &stats)
		}(i)
	}
	wg.Wait()
	close(stop)
	printSummary(*scenario, &stats)
}

func validScenario(scenario string) bool {
	switch scenario {
	case "connect-hold", "guest-login", "password-login", "resume-storm":
		return true
	default:
		return false
	}
}

func runClient(target, scenario string, index int, usernamePrefix, password string, resumeInterval time.Duration, deadline time.Time, stats *counters) {
	installID := fmt.Sprintf("%s-%d-%d", usernamePrefix, os.Getpid(), index)
	username := fmt.Sprintf("%s-%d-%d", usernamePrefix, os.Getpid(), index)
	var sessionID, resumeToken string
	for time.Now().Before(deadline) {
		connection, _, err := websocket.DefaultDialer.Dial(target, nil)
		if err != nil {
			stats.connectFailed.Add(1)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		stats.connected.Add(1)
		stats.connectOK.Add(1)

		switch scenario {
		case "connect-hold":
			holdConnection(connection, deadline)
			connection.Close()
			stats.connected.Add(-1)
			return
		case "guest-login", "password-login":
			provider := gatewaypb.AuthProvider_AUTH_PROVIDER_GUEST
			if scenario == "password-login" {
				provider = gatewaypb.AuthProvider_AUTH_PROVIDER_PASSWORD
			}
			start := time.Now()
			response, ok := login(connection, provider, installID, username, password)
			stats.observe(start, ok)
			if ok {
				sessionID, resumeToken = response.SessionId, response.ResumeToken
			}
			if !ok || scenario != "guest-login" && scenario != "password-login" {
				connection.Close()
				stats.connected.Add(-1)
				return
			}
			holdConnection(connection, deadline)
			connection.Close()
			stats.connected.Add(-1)
			return
		case "resume-storm":
			if sessionID == "" {
				start := time.Now()
				response, ok := login(connection, gatewaypb.AuthProvider_AUTH_PROVIDER_GUEST, installID, username, password)
				stats.observe(start, ok)
				if !ok {
					connection.Close()
					stats.connected.Add(-1)
					return
				}
				sessionID, resumeToken = response.SessionId, response.ResumeToken
			}
			connection.Close()
			stats.connected.Add(-1)
			time.Sleep(resumeInterval)
			connection, _, err = websocket.DefaultDialer.Dial(target, nil)
			if err != nil {
				stats.resumeFailures.Add(1)
				continue
			}
			stats.connected.Add(1)
			stats.resumes.Add(1)
			start := time.Now()
			nextToken, ok := resume(connection, sessionID, resumeToken)
			stats.observe(start, ok)
			if ok {
				stats.resumeSuccess.Add(1)
				resumeToken = nextToken
			} else {
				stats.resumeFailures.Add(1)
			}
			connection.Close()
			stats.connected.Add(-1)
		}
	}
}

func holdConnection(connection *websocket.Conn, deadline time.Time) {
	remaining := time.Until(deadline)
	if remaining > 0 {
		time.Sleep(remaining)
	}
}

func login(connection *websocket.Conn, provider gatewaypb.AuthProvider, installID, username, password string) (*gatewaypb.LoginResponse, bool) {
	payload, err := proto.Marshal(&gatewaypb.LoginRequest{Provider: provider, InstallId: installID, Username: username, Password: password, IdempotencyKey: fmt.Sprintf("loadtest-%s", installID)})
	if err != nil {
		return nil, false
	}
	envelope, err := proto.Marshal(&gatewaypb.Envelope{MessageId: gatewaypb.ClientMessageId_CLIENT_MESSAGE_ID_LOGIN_REQUEST, RequestId: 1, Payload: payload})
	if err != nil || connection.WriteMessage(websocket.BinaryMessage, envelope) != nil {
		return nil, false
	}
	responseEnvelope, err := readResponse(connection, time.Now().Add(15*time.Second))
	if err != nil || responseEnvelope.MessageId != gatewaypb.ClientMessageId_CLIENT_MESSAGE_ID_LOGIN_RESPONSE {
		return nil, false
	}
	response := &gatewaypb.LoginResponse{}
	if proto.Unmarshal(responseEnvelope.Payload, response) != nil || response.SessionId == "" || response.ResumeToken == "" {
		return nil, false
	}
	return response, true
}

func resume(connection *websocket.Conn, sessionID, resumeToken string) (string, bool) {
	payload, err := proto.Marshal(&gatewaypb.ResumeRequest{SessionId: sessionID, ResumeToken: resumeToken})
	if err != nil {
		return "", false
	}
	envelope, err := proto.Marshal(&gatewaypb.Envelope{MessageId: gatewaypb.ClientMessageId_CLIENT_MESSAGE_ID_RESUME_REQUEST, RequestId: 2, Payload: payload})
	if err != nil || connection.WriteMessage(websocket.BinaryMessage, envelope) != nil {
		return "", false
	}
	responseEnvelope, err := readResponse(connection, time.Now().Add(15*time.Second))
	if err != nil || responseEnvelope.MessageId != gatewaypb.ClientMessageId_CLIENT_MESSAGE_ID_RESUME_RESPONSE {
		return "", false
	}
	response := &gatewaypb.ResumeResponse{}
	if proto.Unmarshal(responseEnvelope.Payload, response) != nil || response.ResumeToken == "" {
		return "", false
	}
	return response.ResumeToken, true
}

func readResponse(connection *websocket.Conn, deadline time.Time) (*gatewaypb.Envelope, error) {
	if err := connection.SetReadDeadline(deadline); err != nil {
		return nil, err
	}
	for {
		messageType, payload, err := connection.ReadMessage()
		if err != nil {
			return nil, err
		}
		if messageType != websocket.BinaryMessage {
			continue
		}
		envelope := &gatewaypb.Envelope{}
		if err := proto.Unmarshal(payload, envelope); err != nil {
			continue
		}
		return envelope, nil
	}
}

func report(stats *counters, stop <-chan struct{}) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			fmt.Printf("active=%d connected=%d failed_connect=%d requests=%d success=%d failures=%d resumes=%d resume_success=%d resume_failures=%d\n", stats.connected.Load(), stats.connectOK.Load(), stats.connectFailed.Load(), stats.requests.Load(), stats.success.Load(), stats.failures.Load(), stats.resumes.Load(), stats.resumeSuccess.Load(), stats.resumeFailures.Load())
		case <-stop:
			return
		}
	}
}

func printSummary(scenario string, stats *counters) {
	stats.mu.Lock()
	latencies := append([]time.Duration(nil), stats.latencies...)
	stats.mu.Unlock()
	if len(latencies) > 0 {
		for i := 1; i < len(latencies); i++ {
			for j := i; j > 0 && latencies[j] < latencies[j-1]; j-- {
				latencies[j], latencies[j-1] = latencies[j-1], latencies[j]
			}
		}
	}
	percentile := func(p float64) time.Duration {
		if len(latencies) == 0 {
			return 0
		}
		index := int(float64(len(latencies)-1) * p)
		return latencies[index]
	}
	fmt.Printf("summary scenario=%s requests=%d success=%d failures=%d p50=%s p95=%s p99=%s\n", scenario, stats.requests.Load(), stats.success.Load(), stats.failures.Load(), percentile(.50), percentile(.95), percentile(.99))
}
