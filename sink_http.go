// Copyright Onaro (BrianOnAI LLC)
// SPDX-License-Identifier: Apache-2.0

package oasaexporter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer/consumererror"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	"github.com/onaro-io/oasa-otel-exporter/record"
)

const (
	userAgent = "oasa-otel-exporter/0.1.0"

	// Meridian sets this header on a 429 caused by a billing limit (monthly
	// event ceiling or hard cap). Retrying cannot succeed until the limit resets.
	limitTypeHeader  = "X-Meridian-Limit-Type"
	limitResetHeader = "X-Meridian-Reset-Date"

	limitRejectedMetric = "otelcol_exporter_oasa_limit_rejected_records"
	maxResponseBody     = 1 << 20
)

// httpSink POSTs batches of records as JSON to a configured URL.
type httpSink struct {
	cfg           HTTPConfig
	settings      exporter.Settings
	logger        *zap.Logger
	client        *http.Client
	limitRejected metric.Int64Counter
}

func newHTTPSink(cfg HTTPConfig, set exporter.Settings) (*httpSink, error) {
	meter := set.MeterProvider.Meter("github.com/onaro-io/oasa-otel-exporter")
	counter, err := meter.Int64Counter(
		limitRejectedMetric,
		metric.WithDescription("Records dropped without retry because the receiver reported a billing limit (HTTP 429 with "+limitTypeHeader+")."),
		metric.WithUnit("{records}"),
	)
	if err != nil {
		return nil, err
	}
	return &httpSink{cfg: cfg, settings: set, logger: set.Logger, limitRejected: counter}, nil
}

func (s *httpSink) start(ctx context.Context, host component.Host) error {
	client, err := s.cfg.ClientConfig.ToClient(ctx, host.GetExtensions(), s.settings.TelemetrySettings)
	if err != nil {
		return err
	}
	s.client = client
	return nil
}

func (s *httpSink) shutdown(context.Context) error {
	if s.client != nil {
		s.client.CloseIdleConnections()
	}
	return nil
}

type batchEnvelope struct {
	SchemaVersion string            `json:"schema_version"`
	SourceSystem  string            `json:"source_system"`
	EmittedAt     string            `json:"emitted_at"`
	Count         int               `json:"count"`
	Records       []json.RawMessage `json:"records"`
}

type ingestResponse struct {
	Accepted   int `json:"accepted"`
	Duplicates int `json:"duplicates"`
	Rejected   []struct {
		Index    int    `json:"index"`
		RecordID string `json:"record_id"`
		Field    string `json:"field"`
		Reason   string `json:"reason"`
	} `json:"rejected"`
}

// write sends records in batches of at most MaxBatchSize. Each record carries
// its deterministic record_id, so a retry after partial success is deduplicated
// by the receiver.
func (s *httpSink) write(ctx context.Context, records []*record.Record) error {
	size := s.cfg.MaxBatchSize
	for start := 0; start < len(records); start += size {
		end := min(start+size, len(records))
		err := s.send(ctx, records[start:end])
		if err == nil {
			continue
		}
		var le *limitError
		if errors.As(err, &le) {
			remaining := len(records) - start
			s.limitRejected.Add(ctx, int64(remaining), metric.WithAttributes(attribute.String("limit_type", le.limitType)))
			s.logger.Error("Receiver reported a billing limit; dropping records without retry",
				zap.String("limit_type", le.limitType),
				zap.String("reset_date", le.resetDate),
				zap.Int("dropped_records", remaining),
				zap.String("endpoint", s.cfg.ClientConfig.Endpoint))
			return consumererror.NewPermanent(err)
		}
		return err
	}
	return nil
}

type limitError struct {
	limitType string
	resetDate string
	body      string
}

func (e *limitError) Error() string {
	return fmt.Sprintf("HTTP 429 %s=%s: %s", limitTypeHeader, e.limitType, e.body)
}

func (s *httpSink) send(ctx context.Context, batch []*record.Record) error {
	env := batchEnvelope{
		SchemaVersion: record.SchemaVersion,
		SourceSystem:  sourceSystem,
		EmittedAt:     record.FormatTime(time.Now()),
		Count:         len(batch),
		Records:       make([]json.RawMessage, 0, len(batch)),
	}
	for _, r := range batch {
		b, err := record.Marshal(r)
		if err != nil {
			return consumererror.NewPermanent(err)
		}
		env.Records = append(env.Records, b)
	}
	body, err := json.Marshal(env)
	if err != nil {
		return consumererror.NewPermanent(err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.ClientConfig.Endpoint, bytes.NewReader(body))
	if err != nil {
		return consumererror.NewPermanent(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if s.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+string(s.cfg.Token))
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending OASA batch: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		s.logRejected(respBody)
		return nil
	case resp.StatusCode == http.StatusTooManyRequests && resp.Header.Get(limitTypeHeader) != "":
		return &limitError{
			limitType: resp.Header.Get(limitTypeHeader),
			resetDate: resp.Header.Get(limitResetHeader),
			body:      truncate(string(respBody), 200),
		}
	case isRetryableStatus(resp.StatusCode):
		err := fmt.Errorf("OASA receiver returned HTTP %d: %s", resp.StatusCode, truncate(string(respBody), 200))
		if delay, ok := parseRetryAfter(resp.Header.Get("Retry-After")); ok {
			return exporterhelper.NewThrottleRetry(err, delay)
		}
		return err
	default:
		return consumererror.NewPermanent(fmt.Errorf("OASA receiver returned HTTP %d: %s", resp.StatusCode, truncate(string(respBody), 200)))
	}
}

// logRejected reports records the receiver accepted the batch for but rejected
// individually. They fail validation, so retrying them cannot help.
func (s *httpSink) logRejected(body []byte) {
	var r ingestResponse
	if json.Unmarshal(body, &r) != nil || len(r.Rejected) == 0 {
		return
	}
	first := r.Rejected[0]
	s.logger.Warn("Receiver rejected OASA records",
		zap.Int("rejected", len(r.Rejected)),
		zap.Int("accepted", r.Accepted),
		zap.Int("duplicates", r.Duplicates),
		zap.String("first_record_id", first.RecordID),
		zap.String("first_field", first.Field),
		zap.String("first_reason", first.Reason))
}

func isRetryableStatus(code int) bool {
	switch code {
	case http.StatusRequestTimeout, http.StatusTooManyRequests,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return code >= 500
}

func parseRetryAfter(v string) (time.Duration, bool) {
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d, true
		}
	}
	return 0, false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
