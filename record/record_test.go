// Copyright Onaro (BrianOnAI LLC)
// SPDX-License-Identifier: Apache-2.0

package record

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDeterministicIDMatchesSpecTestVector(t *testing.T) {
	occurred := time.Date(2026, 9, 1, 15, 4, 5, 0, time.UTC)
	got := DeterministicID(occurred, "4bf92f3577b34da6a3ce929d0e0e4736/00f067aa0ba902b7")
	const want = "01a05d7f-b288-7532-8207-8cb53896d9e1"
	if got != want {
		t.Fatalf("DeterministicID = %s, want SPEC.md test vector %s", got, want)
	}
}

func TestDeterministicIDTruncatesToMillisecond(t *testing.T) {
	base := time.Date(2026, 9, 1, 15, 4, 5, 123_000_000, time.UTC)
	a := DeterministicID(base, "k")
	b := DeterministicID(base.Add(999_999*time.Nanosecond), "k")
	if a != b {
		t.Fatalf("IDs differ within one millisecond: %s vs %s", a, b)
	}
	if c := DeterministicID(base.Add(time.Millisecond), "k"); c == a {
		t.Fatalf("IDs equal across milliseconds: %s", c)
	}
}

func TestDeterministicIDIsUUIDv7(t *testing.T) {
	id := DeterministicID(time.Now(), "trace/span")
	if len(id) != 36 || id[14] != '7' || !strings.ContainsRune("89ab", rune(id[19])) {
		t.Fatalf("not a UUIDv7 with RFC 9562 variant: %s", id)
	}
	if DeterministicID(time.Unix(0, 0), "a") == DeterministicID(time.Unix(0, 0), "b") {
		t.Fatal("different source_record_id produced the same ID")
	}
}

func TestMarshalOmitsAbsentFieldsAndKeepsZeroIntegers(t *testing.T) {
	r := &Record{
		RecordID: "id", RecordType: TypeUsage, SchemaVersion: SchemaVersion,
		OccurredAt: "t", RecordedAt: "t", SourceSystem: "s", SourceRecordID: "x", Currency: "USD",
		OutputTokens: Int64(0),
	}
	b, err := Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(s, "null") {
		t.Fatalf("output contains null: %s", s)
	}
	if strings.Contains(s, "input_tokens") || strings.Contains(s, "agent_id") || strings.Contains(s, "tags") {
		t.Fatalf("absent fields serialized: %s", s)
	}
	if !strings.Contains(s, `"output_tokens":0`) {
		t.Fatalf("explicit zero dropped: %s", s)
	}
	if !strings.HasPrefix(s, `{"record_id":"id","record_type":"usage"`) {
		t.Fatalf("envelope not first in spec order: %s", s)
	}
}

func TestDecimalRoundTripsExactly(t *testing.T) {
	r := &Record{BilledCost: "0.0011", SplitFraction: "0.4"}
	b, err := Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"billed_cost":0.0011`) {
		t.Fatalf("decimal not written as a JSON number: %s", b)
	}
	var back Record
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.BilledCost != "0.0011" || back.SplitFraction != "0.4" {
		t.Fatalf("decimal changed in round trip: %+v", back)
	}
	if _, err := Marshal(&Record{BilledCost: "abc"}); err == nil {
		t.Fatal("invalid decimal marshalled without error")
	}
}

func TestSetByName(t *testing.T) {
	var r Record
	if err := r.SetString("cost_center", "Customer Success"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetInt("input_tokens", 1200); err != nil {
		t.Fatal(err)
	}
	if r.CostCenter != "Customer Success" || r.InputTokens == nil || *r.InputTokens != 1200 {
		t.Fatalf("set by name failed: %+v", r)
	}
	if err := r.SetInt("cost_center", 1); err == nil {
		t.Fatal("SetInt accepted a string field")
	}
	if err := r.SetString("no_such_field", "x"); err == nil {
		t.Fatal("SetString accepted an unknown field")
	}
	if _, ok := Kind("tags"); ok {
		t.Fatal("tags should not be settable by name")
	}
}

func TestEnumsMatchSpec(t *testing.T) {
	if !IsEnumValue("cost_source", "allocated") || IsEnumValue("operation", "execute_tool") {
		t.Fatal("enum table out of date with SPEC.md")
	}
	if !IsEnumValue("model", "anything") {
		t.Fatal("non-enum field rejected a value")
	}
}
