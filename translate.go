// Copyright Onaro (BrianOnAI LLC)
// SPDX-License-Identifier: Apache-2.0

package oasaexporter

import (
	"math"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/onaro-io/oasa-otel-exporter/record"
)

const sourceSystem = "otel-collector"

// translator turns GenAI spans into OASA usage records.
type translator struct {
	mapping  *mapping
	currency string
	tagsFrom []string
	now      func() time.Time
}

// translate emits one usage record per span that carries a GenAI attribute.
// Spans without one are skipped. Nothing is priced.
func (t *translator) translate(td ptrace.Traces) []*record.Record {
	recordedAt := record.FormatTime(t.now())
	var out []*record.Record
	rss := td.ResourceSpans()
	for i := 0; i < rss.Len(); i++ {
		rs := rss.At(i)
		resAttrs := rs.Resource().Attributes()
		sss := rs.ScopeSpans()
		for j := 0; j < sss.Len(); j++ {
			spans := sss.At(j).Spans()
			for k := 0; k < spans.Len(); k++ {
				span := spans.At(k)
				if !t.isGenAI(span.Attributes()) {
					continue
				}
				out = append(out, t.spanToRecord(span, resAttrs, recordedAt))
			}
		}
	}
	return out
}

func (t *translator) isGenAI(attrs pcommon.Map) bool {
	found := false
	attrs.Range(func(k string, _ pcommon.Value) bool {
		if strings.HasPrefix(k, t.mapping.genAIPrefix) {
			found = true
			return false
		}
		return true
	})
	return found
}

func (t *translator) spanToRecord(span ptrace.Span, resAttrs pcommon.Map, recordedAt string) *record.Record {
	end := span.EndTimestamp().AsTime()
	traceID := span.TraceID().String()
	spanID := span.SpanID().String()
	sourceRecordID := traceID + "/" + spanID

	r := &record.Record{
		RecordID:       record.DeterministicID(end, sourceRecordID),
		RecordType:     record.TypeUsage,
		SchemaVersion:  record.SchemaVersion,
		OccurredAt:     record.FormatTime(end),
		RecordedAt:     recordedAt,
		SourceSystem:   sourceSystem,
		SourceRecordID: sourceRecordID,
		Currency:       t.currency,
		TraceID:        traceID,
		SpanID:         spanID,
		RequestCount:   record.Int64(1),
	}

	spanAttrs := span.Attributes()
	for _, fm := range t.mapping.fields {
		v, ok := firstValue(fm.sources, spanAttrs, resAttrs)
		if !ok {
			continue
		}
		switch fm.kind {
		case record.KindInteger:
			if n, ok := asInt(v); ok {
				_ = r.SetInt(fm.field, n)
			}
		default:
			s := v.AsString()
			if s == "" {
				continue
			}
			if fm.translation != nil {
				if s, ok = fm.translation.apply(s); !ok {
					continue
				}
			}
			if !record.IsEnumValue(fm.field, s) {
				continue
			}
			_ = r.SetString(fm.field, s)
		}
	}

	if r.ToolName != "" && t.mapping.toolCallOperation != "" {
		r.Operation = t.mapping.toolCallOperation
	}

	for _, key := range t.tagsFrom {
		v, ok := spanAttrs.Get(key)
		if !ok {
			v, ok = resAttrs.Get(key)
		}
		if !ok {
			continue
		}
		if s := v.AsString(); s != "" {
			if r.Tags == nil {
				r.Tags = map[string]string{}
			}
			r.Tags[key] = s
		}
	}
	return r
}

func firstValue(sources []source, spanAttrs, resAttrs pcommon.Map) (pcommon.Value, bool) {
	for _, s := range sources {
		attrs := spanAttrs
		if s.scope == scopeResource {
			attrs = resAttrs
		}
		if v, ok := attrs.Get(s.key); ok && v.Type() != pcommon.ValueTypeEmpty {
			return v, true
		}
	}
	return pcommon.Value{}, false
}

func asInt(v pcommon.Value) (int64, bool) {
	switch v.Type() {
	case pcommon.ValueTypeInt:
		return v.Int(), true
	case pcommon.ValueTypeDouble:
		d := v.Double()
		if d == math.Trunc(d) && !math.IsInf(d, 0) {
			return int64(d), true
		}
	case pcommon.ValueTypeStr:
		if n, err := strconv.ParseInt(strings.TrimSpace(v.Str()), 10, 64); err == nil {
			return n, true
		}
	}
	return 0, false
}
