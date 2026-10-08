// Copyright Onaro (BrianOnAI LLC)
// SPDX-License-Identifier: Apache-2.0

package oasaexporter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/config/configoptional"
	"go.opentelemetry.io/collector/consumer/consumererror"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
	"go.opentelemetry.io/collector/exporter/exportertest"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

type receivedRequest struct {
	header http.Header
	body   batchEnvelope
}

// fakeReceiver records every request and answers with the next scripted response.
type fakeReceiver struct {
	mu        sync.Mutex
	requests  []receivedRequest
	responses []func(w http.ResponseWriter)
}

func (f *fakeReceiver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var env batchEnvelope
	_ = json.Unmarshal(b, &env)
	f.mu.Lock()
	n := len(f.requests)
	f.requests = append(f.requests, receivedRequest{header: r.Header.Clone(), body: env})
	var respond func(http.ResponseWriter)
	if n < len(f.responses) {
		respond = f.responses[n]
	}
	f.mu.Unlock()
	if respond == nil {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"accepted":1,"duplicates":0,"rejected":[]}`))
		return
	}
	respond(w)
}

func (f *fakeReceiver) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func status(code int, headers ...string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		for i := 0; i+1 < len(headers); i += 2 {
			w.Header().Set(headers[i], headers[i+1])
		}
		w.WriteHeader(code)
		_, _ = w.Write([]byte(`{"error":"scripted"}`))
	}
}

type harness struct {
	exp    exporter.Traces
	recv   *fakeReceiver
	tel    *componenttest.Telemetry
	logs   *observer.ObservedLogs
	traces ptrace.Traces
}

// newHTTPHarness builds the real exporter (factory, exporterhelper retry) against
// a fake receiver, with the queue disabled so ConsumeTraces returns the final result.
func newHTTPHarness(t *testing.T, recv *fakeReceiver) *harness {
	t.Helper()
	srv := httptest.NewServer(recv)
	t.Cleanup(srv.Close)

	cfg := createDefaultConfig().(*Config)
	cfg.Sink = sinkHTTP
	cfg.HTTP.ClientConfig.Endpoint = srv.URL
	cfg.HTTP.Token = "test-token"
	cfg.QueueConfig = configoptional.None[exporterhelper.QueueBatchConfig]()
	cfg.RetryConfig.InitialInterval = 5 * time.Millisecond
	cfg.RetryConfig.MaxInterval = 20 * time.Millisecond
	cfg.RetryConfig.MaxElapsedTime = 10 * time.Second
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}

	tel := componenttest.NewTelemetry()
	t.Cleanup(func() { _ = tel.Shutdown(context.Background()) })
	core, logs := observer.New(zapcore.InfoLevel)
	set := exportertest.NewNopSettings(componentType)
	set.TelemetrySettings = tel.NewTelemetrySettings()
	set.Logger = zap.New(core)

	exp, err := NewFactory().CreateTraces(context.Background(), set, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := exp.Start(context.Background(), componenttest.NewNopHost()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = exp.Shutdown(context.Background()) })

	fixture, err := os.ReadFile(filepath.Join("testdata", "otlp", "usage-openai-chat.json"))
	if err != nil {
		t.Fatal(err)
	}
	td, err := (&ptrace.JSONUnmarshaler{}).UnmarshalTraces(fixture)
	if err != nil {
		t.Fatal(err)
	}
	return &harness{exp: exp, recv: recv, tel: tel, logs: logs, traces: td}
}

func recordIDs(env batchEnvelope) []string {
	ids := make([]string, 0, len(env.Records))
	for _, raw := range env.Records {
		var r struct {
			RecordID string `json:"record_id"`
		}
		_ = json.Unmarshal(raw, &r)
		ids = append(ids, r.RecordID)
	}
	return ids
}

func TestHTTPSinkRetriesWithSameRecordID(t *testing.T) {
	recv := &fakeReceiver{responses: []func(http.ResponseWriter){
		status(http.StatusServiceUnavailable),
		status(http.StatusBadGateway),
	}}
	h := newHTTPHarness(t, recv)
	if err := h.exp.ConsumeTraces(context.Background(), h.traces); err != nil {
		t.Fatalf("ConsumeTraces after transient failures = %v, want success", err)
	}
	if recv.count() != 3 {
		t.Fatalf("got %d requests, want 3 (two retries)", recv.count())
	}
	first := recordIDs(recv.requests[0].body)
	for i, req := range recv.requests {
		ids := recordIDs(req.body)
		if len(ids) != 1 || ids[0] != first[0] {
			t.Fatalf("attempt %d sent record_id %v, want %v on every attempt", i+1, ids, first)
		}
		if got := req.header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := req.header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q", got)
		}
	}
}

func TestHTTPSinkHonoursRetryAfter(t *testing.T) {
	recv := &fakeReceiver{responses: []func(http.ResponseWriter){
		status(http.StatusTooManyRequests, "Retry-After", "1"),
	}}
	h := newHTTPHarness(t, recv)
	start := time.Now()
	if err := h.exp.ConsumeTraces(context.Background(), h.traces); err != nil {
		t.Fatalf("ConsumeTraces = %v, want success after throttled retry", err)
	}
	if recv.count() != 2 {
		t.Fatalf("got %d requests, want 2", recv.count())
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Fatalf("retried after %v, want to wait for Retry-After: 1", elapsed)
	}
}

func TestHTTPSinkDoesNotRetryClientErrors(t *testing.T) {
	recv := &fakeReceiver{responses: []func(http.ResponseWriter){status(http.StatusUnauthorized)}}
	h := newHTTPHarness(t, recv)
	err := h.exp.ConsumeTraces(context.Background(), h.traces)
	if err == nil || !consumererror.IsPermanent(err) {
		t.Fatalf("ConsumeTraces = %v, want a permanent error", err)
	}
	if recv.count() != 1 {
		t.Fatalf("got %d requests, want 1 (no retry on 401)", recv.count())
	}
}

func TestHTTPSinkLimitIsPermanentWithMetricAndLog(t *testing.T) {
	recv := &fakeReceiver{responses: []func(http.ResponseWriter){
		status(http.StatusTooManyRequests,
			"X-Meridian-Limit-Type", "monthly_events",
			"X-Meridian-Reset-Date", "2026-11-01",
			"Retry-After", "86400"),
	}}
	h := newHTTPHarness(t, recv)
	err := h.exp.ConsumeTraces(context.Background(), h.traces)
	if err == nil || !consumererror.IsPermanent(err) {
		t.Fatalf("ConsumeTraces = %v, want a permanent error", err)
	}
	time.Sleep(50 * time.Millisecond)
	if recv.count() != 1 {
		t.Fatalf("got %d requests, want 1 (no retry on a billing limit)", recv.count())
	}

	m, err := h.tel.GetMetric(limitRejectedMetric)
	if err != nil {
		t.Fatalf("metric %s not recorded: %v", limitRejectedMetric, err)
	}
	sum, ok := m.Data.(metricdata.Sum[int64])
	if !ok || len(sum.DataPoints) != 1 {
		t.Fatalf("unexpected metric data: %#v", m.Data)
	}
	dp := sum.DataPoints[0]
	if dp.Value != 1 {
		t.Errorf("%s = %d, want 1 dropped record", limitRejectedMetric, dp.Value)
	}
	if v, _ := dp.Attributes.Value("limit_type"); v.AsString() != "monthly_events" {
		t.Errorf("limit_type attribute = %q", v.AsString())
	}

	entries := h.logs.FilterMessageSnippet("billing limit").All()
	if len(entries) != 1 || entries[0].Level != zapcore.ErrorLevel {
		t.Fatalf("want one error log about the billing limit, got %v", h.logs.All())
	}
	fields := entries[0].ContextMap()
	if fields["limit_type"] != "monthly_events" || fields["reset_date"] != "2026-11-01" || fields["dropped_records"] != int64(1) {
		t.Errorf("log fields = %v", fields)
	}
}

func TestHTTPSinkBatches(t *testing.T) {
	recv := &fakeReceiver{}
	srv := httptest.NewServer(recv)
	defer srv.Close()

	cfg := createDefaultConfig().(*Config).HTTP
	cfg.ClientConfig.Endpoint = srv.URL
	cfg.MaxBatchSize = 2
	s, err := newHTTPSink(cfg, exportertest.NewNopSettings(componentType))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.start(context.Background(), componenttest.NewNopHost()); err != nil {
		t.Fatal(err)
	}
	if err := s.write(context.Background(), sampleRecords(5)); err != nil {
		t.Fatal(err)
	}
	if recv.count() != 3 {
		t.Fatalf("got %d requests, want 3 batches for 5 records at max_batch_size 2", recv.count())
	}
	seen := map[string]bool{}
	for i, want := range []int{2, 2, 1} {
		env := recv.requests[i].body
		if env.Count != want || len(env.Records) != want {
			t.Errorf("batch %d: count=%d records=%d, want %d", i, env.Count, len(env.Records), want)
		}
		if env.SchemaVersion != "0.1" || env.SourceSystem != "otel-collector" || env.EmittedAt == "" {
			t.Errorf("batch %d header = %+v", i, env)
		}
		for _, raw := range env.Records {
			assertValidOASA(t, raw)
		}
		for _, id := range recordIDs(env) {
			if seen[id] {
				t.Errorf("record_id %s sent twice", id)
			}
			seen[id] = true
		}
	}
	if got := recv.requests[0].header.Get("Authorization"); got != "" {
		t.Errorf("Authorization sent without a token: %q", got)
	}
}

func TestHTTPSinkLogsRejectedRecords(t *testing.T) {
	recv := &fakeReceiver{responses: []func(http.ResponseWriter){
		func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"accepted":0,"duplicates":0,"rejected":[{"index":0,"record_id":"r1","field":"currency","reason":"must be ISO 4217"}]}`))
		},
	}}
	h := newHTTPHarness(t, recv)
	if err := h.exp.ConsumeTraces(context.Background(), h.traces); err != nil {
		t.Fatal(err)
	}
	entries := h.logs.FilterMessage("Receiver rejected OASA records").All()
	if len(entries) != 1 || entries[0].ContextMap()["first_reason"] != "must be ISO 4217" {
		t.Fatalf("rejection not logged: %v", h.logs.All())
	}
	if recv.count() != 1 {
		t.Fatalf("rejected records were retried: %d requests", recv.count())
	}
}

func TestParseRetryAfter(t *testing.T) {
	if d, ok := parseRetryAfter("3"); !ok || d != 3*time.Second {
		t.Errorf("seconds form = %v %v", d, ok)
	}
	future := time.Now().Add(time.Minute).UTC().Format(http.TimeFormat)
	if d, ok := parseRetryAfter(future); !ok || d <= 0 {
		t.Errorf("date form = %v %v", d, ok)
	}
	if _, ok := parseRetryAfter("soon"); ok {
		t.Error("accepted an invalid Retry-After")
	}
}
