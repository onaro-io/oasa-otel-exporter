// Copyright Onaro (BrianOnAI LLC)
// SPDX-License-Identifier: Apache-2.0

package oasaexporter

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/config/configoptional"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
	"go.opentelemetry.io/collector/exporter/exportertest"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

func TestFactoryType(t *testing.T) {
	if got := NewFactory().Type().String(); got != "oasa" {
		t.Fatalf("component type = %q, want oasa", got)
	}
}

// TestFileSinkEndToEnd runs the exporter as the collector would, with the file
// sink and no network, and checks the written line against the golden record.
func TestFileSinkEndToEnd(t *testing.T) {
	out := filepath.Join(t.TempDir(), "usage.ndjson")
	cfg := createDefaultConfig().(*Config)
	cfg.File.Path = out
	cfg.QueueConfig = configoptional.None[exporterhelper.QueueBatchConfig]()
	cfg.Attribution = AttributionConfig{
		CostCenter: `resource.attributes["cost.center"]`,
		ProjectID:  `resource.attributes["project.id"]`,
		TagsFrom:   []string{"team"},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	exp, err := NewFactory().CreateTraces(ctx, exportertest.NewNopSettings(componentType), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := exp.Start(ctx, componenttest.NewNopHost()); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile(filepath.Join("testdata", "otlp", "usage-openai-chat.json"))
	if err != nil {
		t.Fatal(err)
	}
	td, err := (&ptrace.JSONUnmarshaler{}).UnmarshalTraces(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := exp.ConsumeTraces(ctx, td); err != nil {
		t.Fatal(err)
	}
	if err := exp.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	written, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimRight(written, "\n"), []byte("\n"))
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	assertValidOASA(t, lines[0])
	golden, err := os.ReadFile(filepath.Join("testdata", "golden", "usage-openai-chat.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := normalize(t, lines[0]), normalize(t, golden); !bytes.Equal(got, want) {
		t.Fatalf("file output differs from golden\n--- got\n%s\n--- want\n%s", got, want)
	}
}
