// Copyright Onaro (BrianOnAI LLC)
// SPDX-License-Identifier: Apache-2.0

// Package record defines the OASA 0.1 record and its JSON serialization.
//
// Field order follows the SPEC.md group tables. Absent optional fields are
// omitted from the JSON output, never written as null.
package record

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"time"
)

// SchemaVersion is the OASA schema version this package emits.
const SchemaVersion = "0.1"

// Record types (SPEC.md Group 1).
const (
	TypeUsage      = "usage"
	TypeCharge     = "charge"
	TypeSettlement = "settlement"
	TypeAllocation = "allocation"
	TypeOutcome    = "outcome"
)

// Decimal is an exact decimal quantity serialized as a JSON number.
// It is stored as its decimal string so money values never pass through float64.
type Decimal string

var decimalPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

// MarshalJSON writes the decimal as a bare JSON number.
func (d Decimal) MarshalJSON() ([]byte, error) {
	if !decimalPattern.MatchString(string(d)) {
		return nil, fmt.Errorf("record: %q is not a decimal number", string(d))
	}
	return []byte(d), nil
}

// UnmarshalJSON accepts a JSON number and keeps its exact text.
func (d *Decimal) UnmarshalJSON(b []byte) error {
	s := string(bytes.TrimSpace(b))
	if !decimalPattern.MatchString(s) {
		return fmt.Errorf("record: decimal must be a JSON number, got %s", s)
	}
	*d = Decimal(s)
	return nil
}

// Record is one OASA 0.1 record. Envelope fields are always serialized;
// every other field is omitted when empty.
type Record struct {
	// Group 1 — Envelope
	RecordID       string `json:"record_id"`
	RecordType     string `json:"record_type"`
	SchemaVersion  string `json:"schema_version"`
	OccurredAt     string `json:"occurred_at"`
	RecordedAt     string `json:"recorded_at"`
	SourceSystem   string `json:"source_system"`
	SourceRecordID string `json:"source_record_id"`
	Currency       string `json:"currency"`

	// Group 2 — Agent identity
	AgentID             string `json:"agent_id,omitempty"`
	AgentName           string `json:"agent_name,omitempty"`
	AgentVersion        string `json:"agent_version,omitempty"`
	AgentOwner          string `json:"agent_owner,omitempty"`
	ParentAgentID       string `json:"parent_agent_id,omitempty"`
	AgentFramework      string `json:"agent_framework,omitempty"`
	IdentityScheme      string `json:"identity_scheme,omitempty"`
	ExternalIdentityRef string `json:"external_identity_ref,omitempty"`

	// Group 3 — Task & session context
	SessionID           string `json:"session_id,omitempty"`
	TraceID             string `json:"trace_id,omitempty"`
	SpanID              string `json:"span_id,omitempty"`
	TaskID              string `json:"task_id,omitempty"`
	TaskType            string `json:"task_type,omitempty"`
	WorkflowID          string `json:"workflow_id,omitempty"`
	Trigger             string `json:"trigger,omitempty"`
	InitiatingPrincipal string `json:"initiating_principal,omitempty"`

	// Group 4 — Consumption
	Provider        string  `json:"provider,omitempty"`
	Service         string  `json:"service,omitempty"`
	Model           string  `json:"model,omitempty"`
	Operation       string  `json:"operation,omitempty"`
	InputTokens     *int64  `json:"input_tokens,omitempty"`
	OutputTokens    *int64  `json:"output_tokens,omitempty"`
	CachedTokens    *int64  `json:"cached_tokens,omitempty"`
	ReasoningTokens *int64  `json:"reasoning_tokens,omitempty"`
	UnitType        string  `json:"unit_type,omitempty"`
	UnitsConsumed   Decimal `json:"units_consumed,omitempty"`
	ToolName        string  `json:"tool_name,omitempty"`
	RequestCount    *int64  `json:"request_count,omitempty"`

	// Group 5 — Cost
	ListCost       Decimal `json:"list_cost,omitempty"`
	BilledCost     Decimal `json:"billed_cost,omitempty"`
	EffectiveCost  Decimal `json:"effective_cost,omitempty"`
	CostSource     string  `json:"cost_source,omitempty"`
	PricingRef     string  `json:"pricing_ref,omitempty"`
	InvoiceID      string  `json:"invoice_id,omitempty"`
	FocusChargeRef string  `json:"focus_charge_ref,omitempty"`

	// Group 6 — Allocation
	CostCenter       string            `json:"cost_center,omitempty"`
	ProjectID        string            `json:"project_id,omitempty"`
	CustomerID       string            `json:"customer_id,omitempty"`
	ProductLine      string            `json:"product_line,omitempty"`
	Environment      string            `json:"environment,omitempty"`
	Tags             map[string]string `json:"tags,omitempty"`
	AllocationMethod string            `json:"allocation_method,omitempty"`
	AllocationRuleID string            `json:"allocation_rule_id,omitempty"`
	SplitFraction    Decimal           `json:"split_fraction,omitempty"`

	// Group 7 — Settlement join
	SettlementRail       string  `json:"settlement_rail,omitempty"`
	SettlementNetwork    string  `json:"settlement_network,omitempty"`
	SettlementAsset      string  `json:"settlement_asset,omitempty"`
	SettlementAmount     Decimal `json:"settlement_amount,omitempty"`
	SettlementRef        string  `json:"settlement_ref,omitempty"`
	SettledAt            string  `json:"settled_at,omitempty"`
	ReconciliationStatus string  `json:"reconciliation_status,omitempty"`

	// Group 8 — Control & governance
	BudgetID         string `json:"budget_id,omitempty"`
	PolicyID         string `json:"policy_id,omitempty"`
	AuthorizationRef string `json:"authorization_ref,omitempty"`
	ApprovalType     string `json:"approval_type,omitempty"`

	// Group 9 — Outcome
	OutcomeEventID    string  `json:"outcome_event_id,omitempty"`
	OutcomeType       string  `json:"outcome_type,omitempty"`
	OutcomeValue      Decimal `json:"outcome_value,omitempty"`
	OutcomeUnit       string  `json:"outcome_unit,omitempty"`
	BusinessMetricRef string  `json:"business_metric_ref,omitempty"`
}

// Enums lists the allowed values of every OASA 0.1 enum field, keyed by JSON field name.
var Enums = map[string][]string{
	"record_type":           {"usage", "charge", "settlement", "allocation", "outcome"},
	"identity_scheme":       {"internal", "erc8004", "other"},
	"trigger":               {"human", "scheduled", "agent", "event"},
	"operation":             {"chat", "completion", "embeddings", "tool_call", "inference", "storage", "api_call", "other"},
	"unit_type":             {"tokens", "requests", "seconds", "gb", "custom"},
	"cost_source":           {"measured", "rated", "invoiced", "allocated"},
	"environment":           {"prod", "staging", "dev", "other"},
	"allocation_method":     {"direct", "rule", "split"},
	"settlement_rail":       {"invoice", "card", "ach", "wire", "x402", "other_onchain"},
	"reconciliation_status": {"unmatched", "matched", "partial", "disputed"},
	"approval_type":         {"policy_auto", "pre_authorized", "human_approved"},
}

// IsEnumValue reports whether value is allowed for the enum field.
// Fields that are not enums accept any value.
func IsEnumValue(field, value string) bool {
	allowed, ok := Enums[field]
	if !ok {
		return true
	}
	for _, v := range allowed {
		if v == value {
			return true
		}
	}
	return false
}

// FormatTime renders t as RFC 3339 in UTC, trimming trailing zero fractions.
func FormatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// Int64 returns a pointer to v, for the optional integer fields.
func Int64(v int64) *int64 { return &v }

// FieldKind is the value kind of a settable record field.
type FieldKind int

const (
	KindString FieldKind = iota + 1
	KindInteger
	KindDecimal
)

type fieldInfo struct {
	index []int
	kind  FieldKind
}

var fieldsByName = func() map[string]fieldInfo {
	out := map[string]fieldInfo{}
	t := reflect.TypeOf(Record{})
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := f.Tag.Get("json")
		if j := bytes.IndexByte([]byte(name), ','); j >= 0 {
			name = name[:j]
		}
		var kind FieldKind
		switch {
		case f.Type == reflect.TypeOf(Decimal("")):
			kind = KindDecimal
		case f.Type.Kind() == reflect.String:
			kind = KindString
		case f.Type == reflect.TypeOf((*int64)(nil)):
			kind = KindInteger
		default:
			continue
		}
		out[name] = fieldInfo{index: f.Index, kind: kind}
	}
	return out
}()

// FieldNames returns every settable field name in sorted order.
func FieldNames() []string {
	names := make([]string, 0, len(fieldsByName))
	for n := range fieldsByName {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Kind returns the value kind of a field, or false if the field does not exist
// or cannot be set by name (tags).
func Kind(field string) (FieldKind, bool) {
	fi, ok := fieldsByName[field]
	return fi.kind, ok
}

// SetString sets a string or decimal field by its JSON name.
func (r *Record) SetString(field, value string) error {
	fi, ok := fieldsByName[field]
	if !ok || fi.kind == KindInteger {
		return fmt.Errorf("record: %q is not a string field", field)
	}
	reflect.ValueOf(r).Elem().FieldByIndex(fi.index).SetString(value)
	return nil
}

// SetInt sets an integer field by its JSON name.
func (r *Record) SetInt(field string, value int64) error {
	fi, ok := fieldsByName[field]
	if !ok || fi.kind != KindInteger {
		return fmt.Errorf("record: %q is not an integer field", field)
	}
	reflect.ValueOf(r).Elem().FieldByIndex(fi.index).Set(reflect.ValueOf(&value))
	return nil
}

// GetString returns a string or decimal field by its JSON name.
func (r *Record) GetString(field string) string {
	fi, ok := fieldsByName[field]
	if !ok || fi.kind == KindInteger {
		return ""
	}
	return reflect.ValueOf(r).Elem().FieldByIndex(fi.index).String()
}

// Marshal serializes the record as compact JSON with no trailing newline.
func Marshal(r *Record) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(r); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
