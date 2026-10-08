// Copyright Onaro (BrianOnAI LLC)
// SPDX-License-Identifier: Apache-2.0

package oasaexporter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/confmap/confmaptest"
)

func loadConfig(t *testing.T, name string) *Config {
	t.Helper()
	cm, err := confmaptest.LoadConf(filepath.Join("testdata", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	id := component.NewID(componentType)
	if name != "" {
		id = component.NewIDWithName(componentType, name)
	}
	sub, err := cm.Sub(id.String())
	if err != nil {
		t.Fatal(err)
	}
	cfg := createDefaultConfig().(*Config)
	if err := sub.Unmarshal(cfg); err != nil {
		t.Fatalf("unmarshal %s: %v", id, err)
	}
	return cfg
}

func TestConfigStruct(t *testing.T) {
	if err := componenttest.CheckConfigStruct(createDefaultConfig()); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultConfigIsValid(t *testing.T) {
	cfg := loadConfig(t, "")
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config invalid: %v", err)
	}
	if cfg.Sink != "file" || cfg.Currency != "USD" || cfg.HTTP.MaxBatchSize != 1000 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadFileConfig(t *testing.T) {
	cfg := loadConfig(t, "file")
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.File.Path != "/var/log/oasa/usage.ndjson" || cfg.File.MaxMegabytes != 100 {
		t.Errorf("file config = %+v", cfg.File)
	}
	if cfg.Attribution.CostCenter != `resource.attributes["cost.center"]` || len(cfg.Attribution.TagsFrom) != 2 {
		t.Errorf("attribution = %+v", cfg.Attribution)
	}
}

func TestLoadHTTPConfig(t *testing.T) {
	cfg := loadConfig(t, "http")
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP.ClientConfig.Endpoint != "https://api.example.com/v1/meridian/events" || cfg.HTTP.ClientConfig.Timeout != 5*time.Second {
		t.Errorf("client config = %+v", cfg.HTTP.ClientConfig)
	}
	if string(cfg.HTTP.Token) != "secret-token" || cfg.HTTP.MaxBatchSize != 500 {
		t.Errorf("token or max_batch_size not decoded alongside the embedded client config: %+v", cfg.HTTP)
	}
	if cfg.QueueConfig.HasValue() {
		t.Error("sending_queue.enabled: false not applied")
	}
	if cfg.RetryConfig.InitialInterval != time.Second {
		t.Errorf("retry initial_interval = %v", cfg.RetryConfig.InitialInterval)
	}
}

func TestExampleConfigsAreValid(t *testing.T) {
	for _, name := range []string{"config.yaml", "config-http.yaml"} {
		t.Run(name, func(t *testing.T) {
			cm, err := confmaptest.LoadConf(filepath.Join("examples", name))
			if err != nil {
				t.Fatal(err)
			}
			sub, err := cm.Sub("exporters::oasa")
			if err != nil {
				t.Fatal(err)
			}
			cfg := createDefaultConfig().(*Config)
			if err := sub.Unmarshal(cfg); err != nil {
				t.Fatal(err)
			}
			if err := cfg.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestConfigValidateErrors(t *testing.T) {
	cases := map[string]struct {
		mutate func(*Config)
		want   string
	}{
		"unknown sink":         {func(c *Config) { c.Sink = "kafka" }, "sink must be"},
		"file without path":    {func(c *Config) { c.File.Path = "" }, "file.path is required"},
		"http without url":     {func(c *Config) { c.Sink = "http"; c.HTTP.ClientConfig.Endpoint = "" }, "http.endpoint is required"},
		"http zero batch":      {func(c *Config) { c.Sink = "http"; c.HTTP.ClientConfig.Endpoint = "http://x"; c.HTTP.MaxBatchSize = 0 }, "max_batch_size must be positive"},
		"empty currency":       {func(c *Config) { c.Currency = " " }, "currency must not be empty"},
		"bad attribution expr": {func(c *Config) { c.Attribution.CostCenter = "cost.center" }, "fields.cost_center"},
		"empty tag key":        {func(c *Config) { c.Attribution.TagsFrom = []string{""} }, "tags_from[0] is empty"},
		"missing mapping file": {func(c *Config) { c.MappingFile = filepath.Join(t.TempDir(), "nope.yaml") }, "reading mapping file"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := createDefaultConfig().(*Config)
			tc.mutate(cfg)
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tc.want)
			}
		})
	}
}

func TestCustomMappingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mapping.yaml")
	custom := `
genai_attribute_prefix: "gen_ai."
fields:
  model: ['span.attributes["gen_ai.request.model"]']
  task_type: ['span.attributes["app.task"]']
`
	if err := os.WriteFile(path, []byte(custom), 0o600); err != nil {
		t.Fatal(err)
	}
	tr := newTestTranslator(t, func(c *Config) {
		c.MappingFile = path
		c.Attribution = AttributionConfig{}
	})
	r := translateOne(t, tr, map[string]any{"gen_ai.request.model": "m", "gen_ai.provider.name": "openai", "app.task": "ticket_resolve"}, nil)
	if r.Model != "m" || r.TaskType != "ticket_resolve" || r.Provider != "" {
		t.Fatalf("custom mapping not applied: %+v", r)
	}
}
