// Copyright Onaro (BrianOnAI LLC)
// SPDX-License-Identifier: Apache-2.0

package oasaexporter

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"go.opentelemetry.io/collector/pdata/ptrace"
)

// TestGolden feeds OTLP JSON fixtures through the translator and compares the
// output with the golden records from the OASA spec repository (examples/).
// Only recorded_at is normalized: record_id is deterministic and must match.
func TestGolden(t *testing.T) {
	for _, name := range []string{"usage-openai-chat", "usage-tool-call", "usage-bedrock-inference-profile"} {
		t.Run(name, func(t *testing.T) {
			fixture, err := os.ReadFile(filepath.Join("testdata", "otlp", name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			td, err := (&ptrace.JSONUnmarshaler{}).UnmarshalTraces(fixture)
			if err != nil {
				t.Fatalf("parsing OTLP fixture: %v", err)
			}
			golden, err := os.ReadFile(filepath.Join("testdata", "golden", name+".json"))
			if err != nil {
				t.Fatal(err)
			}

			records := newTestTranslator(t, nil).translate(td)
			if len(records) != 1 {
				t.Fatalf("got %d records, want exactly 1 (non-GenAI spans must be dropped)", len(records))
			}
			got := normalize(t, marshalValid(t, records[0]))
			want := normalize(t, golden)
			if !bytes.Equal(got, want) {
				t.Fatalf("output differs from golden examples/%s.json\n--- got\n%s\n--- want\n%s", name, got, want)
			}
		})
	}
}
