// Copyright Onaro (BrianOnAI LLC)
// SPDX-License-Identifier: Apache-2.0

package oasaexporter

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/onaro-io/oasa-otel-exporter/record"
)

var fixedNow = time.Date(2026, 9, 1, 15, 4, 6, 0, time.UTC)

// newTestTranslator builds a translator from the embedded mapping plus the
// attribution used by the golden fixtures.
func newTestTranslator(t *testing.T, mutate func(*Config)) *translator {
	t.Helper()
	cfg := createDefaultConfig().(*Config)
	cfg.Attribution = AttributionConfig{
		CostCenter: `resource.attributes["cost.center"]`,
		ProjectID:  `resource.attributes["project.id"]`,
		TagsFrom:   []string{"team"},
	}
	if mutate != nil {
		mutate(cfg)
	}
	m, err := cfg.buildMapping()
	if err != nil {
		t.Fatalf("buildMapping: %v", err)
	}
	return &translator{mapping: m, currency: cfg.Currency, tagsFrom: cfg.Attribution.TagsFrom, now: func() time.Time { return fixedNow }}
}

var (
	schemaOnce sync.Once
	schema     *jsonschema.Schema
	schemaErr  error
)

// assertValidOASA fails the test if b is not a valid OASA 0.1 record
// according to the schema generated in the spec repository.
func assertValidOASA(t *testing.T, b []byte) {
	t.Helper()
	schemaOnce.Do(func() {
		f, err := os.Open(filepath.Join("testdata", "schema", "oasa-record.schema.json"))
		if err != nil {
			schemaErr = err
			return
		}
		defer f.Close()
		doc, err := jsonschema.UnmarshalJSON(f)
		if err != nil {
			schemaErr = err
			return
		}
		id := doc.(map[string]any)["$id"].(string)
		c := jsonschema.NewCompiler()
		c.AssertFormat()
		if err := c.AddResource(id, doc); err != nil {
			schemaErr = err
			return
		}
		schema, schemaErr = c.Compile(id)
	})
	if schemaErr != nil {
		t.Fatalf("loading OASA schema: %v", schemaErr)
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	if err := schema.Validate(inst); err != nil {
		t.Fatalf("output fails the OASA schema: %v\n%s", err, b)
	}
}

func marshalValid(t *testing.T, r *record.Record) []byte {
	t.Helper()
	b, err := record.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	assertValidOASA(t, b)
	return b
}

// normalize re-serializes a record with sorted keys and recorded_at replaced,
// so outputs can be compared byte for byte.
func normalize(t *testing.T, b []byte) []byte {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("decoding record: %v", err)
	}
	if _, ok := m["recorded_at"]; ok {
		m["recorded_at"] = "<normalized>"
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return out
}
