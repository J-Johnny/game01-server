package observability

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"server/common/reliability"
)

type Metrics struct {
	Registry *prometheus.Registry

	requestAttempts       *prometheus.CounterVec
	requestRetries        *prometheus.CounterVec
	requestRetryExhausted *prometheus.CounterVec
	requestOutcomes       *prometheus.CounterVec
	requestDuration       *prometheus.HistogramVec
	rateLimitDecisions    *prometheus.CounterVec
	circuitCalls          *prometheus.CounterVec
	circuitState          *prometheus.GaugeVec
	circuitTransitions    *prometheus.CounterVec
	sessionLifecycle      *prometheus.CounterVec
	sessionLifecycleQueue prometheus.Gauge
	gatewayErrors         *prometheus.CounterVec
	gatewayConnections    prometheus.Gauge
}

func NewMetrics() *Metrics {
	registry := prometheus.NewRegistry()
	metrics := &Metrics{
		Registry: registry,
		requestAttempts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "game01",
			Subsystem: "reliability",
			Name:      "request_attempts_total",
			Help:      "Total number of individual internal request attempts.",
		}, []string{"caller_service", "target_service", "operation"}),
		requestRetries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "game01",
			Subsystem: "reliability",
			Name:      "request_retries_total",
			Help:      "Total number of internal request retries scheduled.",
		}, []string{"caller_service", "target_service", "operation", "reason"}),
		requestRetryExhausted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "game01",
			Subsystem: "reliability",
			Name:      "request_retry_exhausted_total",
			Help:      "Total number of internal requests that exhausted retry attempts.",
		}, []string{"caller_service", "target_service", "operation", "reason"}),
		requestOutcomes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "game01",
			Subsystem: "reliability",
			Name:      "request_outcomes_total",
			Help:      "Total number of completed internal requests by outcome.",
		}, []string{"caller_service", "target_service", "operation", "result", "reason"}),
		requestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "game01",
			Subsystem: "reliability",
			Name:      "request_duration_seconds",
			Help:      "Internal request duration including retries.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"caller_service", "target_service", "operation"}),
		rateLimitDecisions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "game01",
			Subsystem: "gateway",
			Name:      "rate_limit_decisions_total",
			Help:      "Total number of rate limiter decisions.",
		}, []string{"scope", "decision"}),
		circuitCalls: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "game01",
			Subsystem: "reliability",
			Name:      "circuit_breaker_calls_total",
			Help:      "Total number of circuit breaker calls by result.",
		}, []string{"caller_service", "target_service", "operation", "result"}),
		circuitState: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "game01",
			Subsystem: "reliability",
			Name:      "circuit_breaker_state",
			Help:      "Current circuit breaker state: 0 closed, 1 open, 2 half-open.",
		}, []string{"caller_service", "target_service", "operation"}),
		circuitTransitions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "game01",
			Subsystem: "reliability",
			Name:      "circuit_breaker_transitions_total",
			Help:      "Total number of circuit breaker state transitions.",
		}, []string{"caller_service", "target_service", "operation", "from", "to"}),
		sessionLifecycle: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "game01",
			Subsystem: "gateway",
			Name:      "session_lifecycle_events_total",
			Help:      "Gateway session lifecycle event delivery attempts by target and result.",
		}, []string{"target_service", "event_type", "result"}),
		sessionLifecycleQueue: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "game01",
			Subsystem: "gateway",
			Name:      "session_lifecycle_queue_depth",
			Help:      "Current queued Gateway session lifecycle events.",
		}),
		gatewayErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "game01",
			Subsystem: "gateway",
			Name:      "public_errors_total",
			Help:      "Gateway public errors sent to clients by stable code.",
		}, []string{"code", "retryable"}),
		gatewayConnections: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "game01",
			Subsystem: "gateway",
			Name:      "websocket_connections",
			Help:      "Current Gateway WebSocket connections.",
		}),
	}
	registry.MustRegister(
		metrics.requestAttempts,
		metrics.requestRetries,
		metrics.requestRetryExhausted,
		metrics.requestOutcomes,
		metrics.requestDuration,
		metrics.rateLimitDecisions,
		metrics.circuitCalls,
		metrics.circuitState,
		metrics.circuitTransitions,
		metrics.sessionLifecycle,
		metrics.sessionLifecycleQueue,
		metrics.gatewayErrors,
		metrics.gatewayConnections,
	)
	return metrics
}

func (m *Metrics) ObserveSessionLifecycle(targetService, eventType, result string) {
	if m != nil {
		m.sessionLifecycle.WithLabelValues(targetService, eventType, result).Inc()
	}
}

func (m *Metrics) SetSessionLifecycleQueueDepth(depth int) {
	if m != nil {
		m.sessionLifecycleQueue.Set(float64(depth))
	}
}

func (m *Metrics) ObserveGatewayError(code string, retryable bool) {
	if m != nil {
		value := "false"
		if retryable {
			value = "true"
		}
		m.gatewayErrors.WithLabelValues(code, value).Inc()
	}
}

func (m *Metrics) SetGatewayConnections(count int) {
	if m != nil {
		m.gatewayConnections.Set(float64(count))
	}
}

func (m *Metrics) Handler() http.Handler {
	if m == nil || m.Registry == nil {
		return promhttp.Handler()
	}
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{})
}

func (m *Metrics) RetryObserver(callerService, targetService, operation string, classify func(error) string) reliability.RetryObserver {
	if m == nil {
		return nil
	}
	return &retryObserver{metrics: m, callerService: callerService, targetService: targetService, operation: operation, classify: classify}
}

func (m *Metrics) RateLimitObserver(scope string) reliability.RateLimitObserver {
	if m == nil {
		return nil
	}
	return &rateLimitObserver{metrics: m, scope: scope}
}

func (m *Metrics) CircuitObserver(callerService, targetService, operation string) reliability.CircuitObserver {
	if m == nil {
		return nil
	}
	observer := &circuitObserver{metrics: m, callerService: callerService, targetService: targetService, operation: operation}
	m.circuitState.WithLabelValues(callerService, targetService, operation).Set(float64(reliability.CircuitStateClosed))
	return observer
}

type retryObserver struct {
	metrics       *Metrics
	callerService string
	targetService string
	operation     string
	classify      func(error) string
}

func (o *retryObserver) OnAttempt(_ int) {
	o.metrics.requestAttempts.WithLabelValues(o.callerService, o.targetService, o.operation).Inc()
}

func (o *retryObserver) OnRetry(_ int, err error) {
	o.metrics.requestRetries.WithLabelValues(o.callerService, o.targetService, o.operation, errorReason(err, o.classify)).Inc()
}

func (o *retryObserver) OnComplete(_ int, exhausted bool, err error, duration time.Duration) {
	reason := errorReason(err, o.classify)
	result := "success"
	if err != nil {
		result = "failure"
	}
	o.metrics.requestOutcomes.WithLabelValues(o.callerService, o.targetService, o.operation, result, reason).Inc()
	o.metrics.requestDuration.WithLabelValues(o.callerService, o.targetService, o.operation).Observe(duration.Seconds())
	if exhausted {
		o.metrics.requestRetryExhausted.WithLabelValues(o.callerService, o.targetService, o.operation, reason).Inc()
	}
}

type rateLimitObserver struct {
	metrics *Metrics
	scope   string
}

func (o *rateLimitObserver) OnDecision(allowed bool) {
	decision := "rejected"
	if allowed {
		decision = "allowed"
	}
	o.metrics.rateLimitDecisions.WithLabelValues(o.scope, decision).Inc()
}

type circuitObserver struct {
	metrics       *Metrics
	callerService string
	targetService string
	operation     string
}

func (o *circuitObserver) OnCall(result string) {
	o.metrics.circuitCalls.WithLabelValues(o.callerService, o.targetService, o.operation, result).Inc()
}

func (o *circuitObserver) OnStateChange(from, to reliability.CircuitState) {
	o.metrics.circuitState.WithLabelValues(o.callerService, o.targetService, o.operation).Set(float64(to))
	if from != to {
		o.metrics.circuitTransitions.WithLabelValues(o.callerService, o.targetService, o.operation, circuitStateName(from), circuitStateName(to)).Inc()
	}
}

func errorReason(err error, classify func(error) string) string {
	if err == nil {
		return "none"
	}
	if classify != nil {
		if reason := classify(err); reason != "" {
			return reason
		}
	}
	if errors.Is(err, context.Canceled) {
		return "context_canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "context_deadline_exceeded"
	}
	return "error"
}

func circuitStateName(state reliability.CircuitState) string {
	switch state {
	case reliability.CircuitStateOpen:
		return "open"
	case reliability.CircuitStateHalfOpen:
		return "half_open"
	default:
		return "closed"
	}
}
