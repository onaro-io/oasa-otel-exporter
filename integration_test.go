// Copyright Onaro (BrianOnAI LLC)
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package oasaexporter

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestCollectorEndToEnd builds a collector with ocb (builder-config.yaml),
// runs it with an OTLP/HTTP receiver and the oasa file sink, sends one trace,
// and checks the ndjson output against the golden record.
//
// Requires the ocb `builder` binary on PATH (make integration installs it).
func TestCollectorEndToEnd(t *testing.T) {
	builder, err := exec.LookPath("builder")
	if err != nil {
		t.Fatalf("ocb builder not found on PATH: %v", err)
	}
	build := exec.Command(builder, "--config", "builder-config.yaml")
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		t.Fatalf("ocb build failed: %v", err)
	}
	bin, err := filepath.Abs(filepath.Join("_build", "otelcol-oasa"))
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	out := filepath.Join(dir, "usage.ndjson")
	port := freePort(t)
	config := fmt.Sprintf(`
receivers:
  otlp:
    protocols:
      http:
        endpoint: 127.0.0.1:%d
exporters:
  oasa:
    sink: file
    file:
      path: %s
    attribution:
      cost_center: resource.attributes["cost.center"]
      project_id: resource.attributes["project.id"]
      tags_from: ["team"]
service:
  telemetry:
    metrics:
      level: none
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [oasa]
`, port, out)
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	col := exec.CommandContext(ctx, bin, "--config", cfgPath)
	col.Stdout, col.Stderr = os.Stdout, os.Stderr
	if err := col.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = col.Process.Signal(os.Interrupt)
		_ = col.Wait()
	}()
	waitForPort(t, port, 30*time.Second)

	fixture, err := os.ReadFile(filepath.Join("testdata", "otlp", "usage-openai-chat.json"))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/v1/traces", port), "application/json", bytes.NewReader(fixture))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("OTLP receiver returned HTTP %d", resp.StatusCode)
	}

	line := waitForLine(t, out, 30*time.Second)
	assertValidOASA(t, line)
	golden, err := os.ReadFile(filepath.Join("testdata", "golden", "usage-openai-chat.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := normalize(t, line), normalize(t, golden); !bytes.Equal(got, want) {
		t.Fatalf("collector output differs from golden\n--- got\n%s\n--- want\n%s", got, want)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func waitForPort(t *testing.T, port int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
		if err == nil {
			c.Close()
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("collector did not open port %d within %v", port, timeout)
}

func waitForLine(t *testing.T, path string, timeout time.Duration) []byte {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if f, err := os.Open(path); err == nil {
			sc := bufio.NewScanner(f)
			if sc.Scan() {
				line := append([]byte(nil), sc.Bytes()...)
				f.Close()
				return line
			}
			f.Close()
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("no record written to %s within %v", path, timeout)
	return nil
}
