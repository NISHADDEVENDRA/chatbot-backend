package telemetry

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Metrics holds all application metrics
type Metrics struct {
	RequestCounter      metric.Int64Counter
	RequestDuration     metric.Float64Histogram
	TokensUsed          metric.Int64Counter
	PDFProcessingTime   metric.Float64Histogram
	CircuitBreakerState metric.Int64Counter
	AuditEventsLogged   metric.Int64Counter
	DatabaseOperations  metric.Int64Counter
}

// InitMetrics initializes all application metrics
func InitMetrics() (*Metrics, error) {
	meter := otel.Meter("saas-chatbot-platform")

	requestCounter, err := meter.Int64Counter(
		"http.requests.total",
		metric.WithDescription("Total HTTP requests"),
	)
	if err != nil {
		return nil, err
	}

	requestDuration, err := meter.Float64Histogram(
		"http.request.duration",
		metric.WithDescription("HTTP request duration in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	tokensUsed, err := meter.Int64Counter(
		"gemini.tokens.used",
		metric.WithDescription("Total Gemini tokens used"),
	)
	if err != nil {
		return nil, err
	}

	pdfProcessingTime, err := meter.Float64Histogram(
		"pdf.processing.duration",
		metric.WithDescription("PDF processing duration in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	circuitBreakerState, err := meter.Int64Counter(
		"circuit_breaker.state_changes",
		metric.WithDescription("Circuit breaker state changes"),
	)
	if err != nil {
		return nil, err
	}

	auditEventsLogged, err := meter.Int64Counter(
		"audit.events.logged",
		metric.WithDescription("Total audit events logged"),
	)
	if err != nil {
		return nil, err
	}

	databaseOperations, err := meter.Int64Counter(
		"database.operations.total",
		metric.WithDescription("Total database operations"),
	)
	if err != nil {
		return nil, err
	}

	return &Metrics{
		RequestCounter:      requestCounter,
		RequestDuration:     requestDuration,
		TokensUsed:          tokensUsed,
		PDFProcessingTime:   pdfProcessingTime,
		CircuitBreakerState: circuitBreakerState,
		AuditEventsLogged:   auditEventsLogged,
		DatabaseOperations:  databaseOperations,
	}, nil
}

// RecordRequest records HTTP request metrics
func (m *Metrics) RecordRequest(method, path, status string, duration float64) {
	attrs := []attribute.KeyValue{
		attribute.String("http.method", method),
		attribute.String("http.path", path),
		attribute.String("http.status", status),
	}

	m.RequestCounter.Add(context.Background(), 1, metric.WithAttributes(attrs...))
	m.RequestDuration.Record(context.Background(), duration, metric.WithAttributes(attrs...))
}

// RecordTokensUsed records Gemini token usage
func (m *Metrics) RecordTokensUsed(tokens int64, model string) {
	attrs := []attribute.KeyValue{
		attribute.String("gemini.model", model),
		attribute.String("service", "gemini"),
	}

	m.TokensUsed.Add(context.Background(), tokens, metric.WithAttributes(attrs...))
}

// RecordPDFProcessing records PDF processing metrics
func (m *Metrics) RecordPDFProcessing(duration float64, status string) {
	attrs := []attribute.KeyValue{
		attribute.String("pdf.status", status),
		attribute.String("service", "pdf_processor"),
	}

	m.PDFProcessingTime.Record(context.Background(), duration, metric.WithAttributes(attrs...))
}

// RecordCircuitBreakerState records circuit breaker state changes
func (m *Metrics) RecordCircuitBreakerState(service, state string) {
	attrs := []attribute.KeyValue{
		attribute.String("service", service),
		attribute.String("state", state),
	}

	m.CircuitBreakerState.Add(context.Background(), 1, metric.WithAttributes(attrs...))
}

// RecordAuditEvent records audit event logging
func (m *Metrics) RecordAuditEvent(action, resource string) {
	attrs := []attribute.KeyValue{
		attribute.String("audit.action", action),
		attribute.String("audit.resource", resource),
	}

	m.AuditEventsLogged.Add(context.Background(), 1, metric.WithAttributes(attrs...))
}

// RecordDatabaseOperation records database operation metrics
func (m *Metrics) RecordDatabaseOperation(operation, collection string, success bool) {
	attrs := []attribute.KeyValue{
		attribute.String("db.operation", operation),
		attribute.String("db.collection", collection),
		attribute.Bool("db.success", success),
	}

	m.DatabaseOperations.Add(context.Background(), 1, metric.WithAttributes(attrs...))
}
