// Copyright Onaro (BrianOnAI LLC)
// SPDX-License-Identifier: Apache-2.0

package oasaexporter

import (
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/onaro-io/oasa-otel-exporter/record"
)

var (
	testTraceID = pcommon.TraceID([16]byte{0x4b, 0xf9, 0x2f, 0x35, 0x77, 0xb3, 0x4d, 0xa6, 0xa3, 0xce, 0x92, 0x9d, 0x0e, 0x0e, 0x47, 0x36})
	testSpanID  = pcommon.SpanID([8]byte{0x00, 0xf0, 0x67, 0xaa, 0x0b, 0xa9, 0x02, 0xb7})
	testEnd     = time.Date(2026, 9, 1, 15, 4, 5, 250_000_000, time.UTC)
)

// oneSpan builds a trace with a single span. spanAttrs and resAttrs are
// applied with pcommon.Map.FromRaw.
func oneSpan(t *testing.T, spanAttrs, resAttrs map[string]any) ptrace.Traces {
	t.Helper()
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	if resAttrs != nil {
		if err := rs.Resource().Attributes().FromRaw(resAttrs); err != nil {
			t.Fatal(err)
		}
	}
	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetTraceID(testTraceID)
	span.SetSpanID(testSpanID)
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(testEnd.Add(-time.Second)))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(testEnd))
	if spanAttrs != nil {
		if err := span.Attributes().FromRaw(spanAttrs); err != nil {
			t.Fatal(err)
		}
	}
	return td
}

func translateOne(t *testing.T, tr *translator, spanAttrs, resAttrs map[string]any) *record.Record {
	t.Helper()
	records := tr.translate(oneSpan(t, spanAttrs, resAttrs))
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	marshalValid(t, records[0])
	return records[0]
}

var minimalGenAI = map[string]any{"gen_ai.operation.name": "chat"}

func TestMappingEnvelopeRows(t *testing.T) {
	tr := newTestTranslator(t, func(c *Config) { c.Currency = "EUR" })
	r := translateOne(t, tr, minimalGenAI, nil)

	const wantSource = "4bf92f3577b34da6a3ce929d0e0e4736/00f067aa0ba902b7"
	checks := []struct{ field, got, want string }{
		{"record_id", r.RecordID, record.DeterministicID(testEnd, wantSource)},
		{"record_type", r.RecordType, "usage"},
		{"schema_version", r.SchemaVersion, "0.1"},
		{"occurred_at (span end time)", r.OccurredAt, "2026-09-01T15:04:05.25Z"},
		{"recorded_at (export time)", r.RecordedAt, "2026-09-01T15:04:06Z"},
		{"source_system", r.SourceSystem, "otel-collector"},
		{"source_record_id", r.SourceRecordID, wantSource},
		{"currency (config)", r.Currency, "EUR"},
		{"trace_id", r.TraceID, "4bf92f3577b34da6a3ce929d0e0e4736"},
		{"span_id", r.SpanID, "00f067aa0ba902b7"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.field, c.got, c.want)
		}
	}
	if r.RequestCount == nil || *r.RequestCount != 1 {
		t.Errorf("request_count = %v, want 1", r.RequestCount)
	}
	if r.CostSource != "" || r.BilledCost != "" || r.ListCost != "" || r.EffectiveCost != "" {
		t.Errorf("exporter must not price records: %+v", r)
	}
}

func TestRecordIDIsStableAcrossExports(t *testing.T) {
	tr := newTestTranslator(t, nil)
	a := translateOne(t, tr, minimalGenAI, nil)
	tr.now = func() time.Time { return fixedNow.Add(time.Hour) }
	b := translateOne(t, tr, minimalGenAI, nil)
	if a.RecordID != b.RecordID {
		t.Fatalf("record_id changed between exports of the same span: %s vs %s", a.RecordID, b.RecordID)
	}
}

func TestMappingProvider(t *testing.T) {
	tr := newTestTranslator(t, nil)
	r := translateOne(t, tr, map[string]any{"gen_ai.provider.name": "openai", "gen_ai.system": "legacy"}, nil)
	if r.Provider != "openai" {
		t.Errorf("provider = %q, want gen_ai.provider.name", r.Provider)
	}
	r = translateOne(t, tr, map[string]any{"gen_ai.system": "anthropic"}, nil)
	if r.Provider != "anthropic" {
		t.Errorf("provider = %q, want fallback to gen_ai.system", r.Provider)
	}
}

func TestMappingModel(t *testing.T) {
	tr := newTestTranslator(t, nil)
	r := translateOne(t, tr, map[string]any{"gen_ai.request.model": "gpt-4.1", "gen_ai.response.model": "gpt-4.1-2025-04-14"}, nil)
	if r.Model != "gpt-4.1-2025-04-14" {
		t.Errorf("model = %q, want gen_ai.response.model", r.Model)
	}
	r = translateOne(t, tr, map[string]any{"gen_ai.request.model": "gpt-4.1"}, nil)
	if r.Model != "gpt-4.1" {
		t.Errorf("model = %q, want fallback to gen_ai.request.model", r.Model)
	}
}

func TestMappingOperationTranslation(t *testing.T) {
	tr := newTestTranslator(t, nil)
	cases := map[string]string{
		"chat":             "chat",
		"text_completion":  "completion",
		"generate_content": "chat",
		"embeddings":       "embeddings",
		"execute_tool":     "tool_call",
		"invoke_agent":     "other",
		"create_agent":     "other",
		"something_new":    "other",
	}
	for in, want := range cases {
		r := translateOne(t, tr, map[string]any{"gen_ai.operation.name": in}, nil)
		if r.Operation != want {
			t.Errorf("gen_ai.operation.name %q -> operation %q, want %q", in, r.Operation, want)
		}
	}
}

func TestMappingTokens(t *testing.T) {
	tr := newTestTranslator(t, nil)
	r := translateOne(t, tr, map[string]any{
		"gen_ai.usage.input_tokens":                int64(1200),
		"gen_ai.usage.output_tokens":               int64(340),
		"gen_ai.usage.cache_read.input_tokens":     int64(800),
		"gen_ai.usage.reasoning.output_tokens":     int64(120),
		"gen_ai.usage.cache_creation.input_tokens": int64(55),
	}, nil)
	want := map[string]*int64{"input_tokens": r.InputTokens, "output_tokens": r.OutputTokens, "cached_tokens": r.CachedTokens, "reasoning_tokens": r.ReasoningTokens}
	expected := map[string]int64{"input_tokens": 1200, "output_tokens": 340, "cached_tokens": 800, "reasoning_tokens": 120}
	for f, p := range want {
		if p == nil || *p != expected[f] {
			t.Errorf("%s = %v, want %d", f, p, expected[f])
		}
	}
}

func TestMappingTokensAcceptIntegralDoublesAndStrings(t *testing.T) {
	tr := newTestTranslator(t, nil)
	r := translateOne(t, tr, map[string]any{
		"gen_ai.usage.input_tokens":            float64(1200),
		"gen_ai.usage.output_tokens":           "340",
		"gen_ai.usage.cache_read.input_tokens": 1.5,
	}, nil)
	if r.InputTokens == nil || *r.InputTokens != 1200 || r.OutputTokens == nil || *r.OutputTokens != 340 {
		t.Errorf("tokens not converted: in=%v out=%v", r.InputTokens, r.OutputTokens)
	}
	if r.CachedTokens != nil {
		t.Errorf("non-integral token count should be omitted, got %d", *r.CachedTokens)
	}
}

func TestMappingToolNameForcesToolCall(t *testing.T) {
	tr := newTestTranslator(t, nil)
	r := translateOne(t, tr, map[string]any{"gen_ai.operation.name": "chat", "gen_ai.tool.name": "crm.lookup"}, nil)
	if r.ToolName != "crm.lookup" || r.Operation != "tool_call" {
		t.Errorf("tool_name=%q operation=%q, want crm.lookup / tool_call", r.ToolName, r.Operation)
	}
}

func TestMappingAgentIdentity(t *testing.T) {
	tr := newTestTranslator(t, nil)
	r := translateOne(t, tr,
		map[string]any{"gen_ai.operation.name": "chat", "gen_ai.agent.id": "agent-47", "gen_ai.agent.name": "Support Triage"},
		map[string]any{"service.name": "support-svc"})
	if r.AgentID != "agent-47" || r.AgentName != "Support Triage" {
		t.Errorf("agent_id=%q agent_name=%q", r.AgentID, r.AgentName)
	}

	r = translateOne(t, tr, minimalGenAI, map[string]any{"service.name": "support-svc"})
	if r.AgentName != "support-svc" {
		t.Errorf("agent_name = %q, want service.name fallback", r.AgentName)
	}
	if r.AgentID != "" {
		t.Errorf("agent_id = %q, want omitted when gen_ai.agent.id is absent", r.AgentID)
	}
}

func TestMappingSessionID(t *testing.T) {
	r := translateOne(t, newTestTranslator(t, nil), map[string]any{"gen_ai.conversation.id": "sess-2f91"}, nil)
	if r.SessionID != "sess-2f91" {
		t.Errorf("session_id = %q", r.SessionID)
	}
}

func TestMappingAttribution(t *testing.T) {
	tr := newTestTranslator(t, func(c *Config) {
		c.Attribution.CustomerID = `span.attributes["app.customer"]`
	})
	r := translateOne(t, tr,
		map[string]any{"gen_ai.operation.name": "chat", "app.customer": "acme"},
		map[string]any{"cost.center": "Customer Success", "project.id": "proj_support"})
	if r.CostCenter != "Customer Success" || r.ProjectID != "proj_support" || r.CustomerID != "acme" {
		t.Errorf("cost_center=%q project_id=%q customer_id=%q", r.CostCenter, r.ProjectID, r.CustomerID)
	}
}

func TestMappingEnvironment(t *testing.T) {
	tr := newTestTranslator(t, nil)
	cases := map[string]string{"production": "prod", "Production": "prod", "staging": "staging", "development": "dev", "qa-7": "other"}
	for in, want := range cases {
		r := translateOne(t, tr, minimalGenAI, map[string]any{"deployment.environment.name": in})
		if r.Environment != want {
			t.Errorf("deployment.environment.name %q -> environment %q, want %q", in, r.Environment, want)
		}
	}
	r := translateOne(t, tr, minimalGenAI, map[string]any{"deployment.environment": "staging"})
	if r.Environment != "staging" {
		t.Errorf("environment = %q, want fallback to deprecated deployment.environment", r.Environment)
	}

	tr = newTestTranslator(t, func(c *Config) { c.Attribution.Environment = `resource.attributes["env"]` })
	r = translateOne(t, tr, minimalGenAI, map[string]any{"env": "dev", "deployment.environment.name": "production"})
	if r.Environment != "dev" {
		t.Errorf("environment = %q, want attribution override", r.Environment)
	}
}

func TestMappingTagsFrom(t *testing.T) {
	tr := newTestTranslator(t, func(c *Config) { c.Attribution.TagsFrom = []string{"team", "owner", "absent"} })
	r := translateOne(t, tr,
		map[string]any{"gen_ai.operation.name": "chat", "team": "support-span"},
		map[string]any{"team": "support-resource", "owner": int64(42)})
	if len(r.Tags) != 2 || r.Tags["team"] != "support-span" || r.Tags["owner"] != "42" {
		t.Errorf("tags = %v, want span attribute first and values stringified", r.Tags)
	}
}

func TestSpansWithoutGenAIAreDropped(t *testing.T) {
	tr := newTestTranslator(t, nil)
	td := oneSpan(t, map[string]any{"http.request.method": "GET", "genai.lookalike": "x"}, map[string]any{"gen_ai.on_resource": "ignored"})
	if got := tr.translate(td); len(got) != 0 {
		t.Fatalf("got %d records from a span with no gen_ai.* span attributes, want 0", len(got))
	}
}

func TestMissingOptionalAttributesAreOmitted(t *testing.T) {
	r := translateOne(t, newTestTranslator(t, nil), minimalGenAI, nil)
	b := marshalValid(t, r)
	s := string(b)
	if strings.Contains(s, "null") {
		t.Fatalf("output contains null: %s", s)
	}
	for _, absent := range []string{"provider", "model", "input_tokens", "output_tokens", "cached_tokens", "reasoning_tokens",
		"tool_name", "agent_id", "agent_name", "session_id", "cost_center", "project_id", "customer_id", "environment", "tags"} {
		if strings.Contains(s, `"`+absent+`"`) {
			t.Errorf("%s present although its attribute is missing: %s", absent, s)
		}
	}
}
