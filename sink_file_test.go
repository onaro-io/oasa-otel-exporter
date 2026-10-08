// Copyright Onaro (BrianOnAI LLC)
// SPDX-License-Identifier: Apache-2.0

package oasaexporter

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onaro-io/oasa-otel-exporter/record"
)

func sampleRecords(n int) []*record.Record {
	out := make([]*record.Record, n)
	for i := range out {
		src := fmt.Sprintf("4bf92f3577b34da6a3ce929d0e0e4736/%016x", i)
		out[i] = &record.Record{
			RecordID:       record.DeterministicID(testEnd, src),
			RecordType:     record.TypeUsage,
			SchemaVersion:  record.SchemaVersion,
			OccurredAt:     record.FormatTime(testEnd),
			RecordedAt:     record.FormatTime(fixedNow),
			SourceSystem:   sourceSystem,
			SourceRecordID: src,
			Currency:       "USD",
			Operation:      "chat",
			RequestCount:   record.Int64(1),
		}
	}
	return out
}

func TestFileSinkWritesNDJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "usage.ndjson")
	s := newFileSink(FileConfig{Path: path, MaxMegabytes: 1})
	ctx := context.Background()
	if err := s.write(ctx, sampleRecords(3)); err != nil {
		t.Fatal(err)
	}
	if err := s.write(ctx, sampleRecords(2)); err != nil {
		t.Fatal(err)
	}
	if err := s.shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	lines := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines++
		assertValidOASA(t, sc.Bytes())
	}
	if lines != 5 {
		t.Fatalf("got %d lines, want 5", lines)
	}
}

func TestFileSinkRotatesBySize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.ndjson")
	s := newFileSink(FileConfig{Path: path, MaxMegabytes: 1, MaxBackups: 3})
	ctx := context.Background()
	for i := 0; i < 8; i++ {
		if err := s.write(ctx, sampleRecords(1000)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	rotated := 0
	for _, e := range entries {
		if e.Name() != "usage.ndjson" && strings.HasPrefix(e.Name(), "usage-") {
			rotated++
		}
	}
	if rotated == 0 {
		t.Fatalf("no rotated files after writing past max_megabytes; dir has %d entries", len(entries))
	}
}
