// Copyright Onaro (BrianOnAI LLC)
// SPDX-License-Identifier: Apache-2.0

package oasaexporter

import (
	"errors"
	"fmt"
	"strings"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/confighttp"
	"go.opentelemetry.io/collector/config/configopaque"
	"go.opentelemetry.io/collector/config/configoptional"
	"go.opentelemetry.io/collector/config/configretry"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
)

const (
	sinkFile = "file"
	sinkHTTP = "http"

	defaultMaxBatchSize = 1000
)

// Config is the configuration for the oasa exporter.
type Config struct {
	exporterhelper.TimeoutConfig `mapstructure:",squash"`
	QueueConfig                  configoptional.Optional[exporterhelper.QueueBatchConfig] `mapstructure:"sending_queue"`
	RetryConfig                  configretry.BackOffConfig                                `mapstructure:"retry_on_failure"`

	// Sink is where records go: "file" (newline-delimited JSON) or "http".
	Sink string `mapstructure:"sink"`
	// File configures the file sink.
	File FileConfig `mapstructure:"file"`
	// HTTP configures the http sink.
	HTTP HTTPConfig `mapstructure:"http"`

	// Attribution maps OASA allocation fields to span or resource attributes.
	Attribution AttributionConfig `mapstructure:"attribution"`
	// Currency is the envelope currency. Usage records carry no money.
	Currency string `mapstructure:"currency"`
	// MappingFile replaces the embedded mapping.yaml when set.
	MappingFile string `mapstructure:"mapping_file"`

	_ struct{}
}

// FileConfig configures the newline-delimited JSON file sink.
type FileConfig struct {
	// Path of the active file. Rotated files are written beside it.
	Path string `mapstructure:"path"`
	// MaxMegabytes is the size at which the file rotates.
	MaxMegabytes int `mapstructure:"max_megabytes"`
	// MaxBackups is how many rotated files to keep; 0 keeps all.
	MaxBackups int `mapstructure:"max_backups"`

	_ struct{}
}

// HTTPConfig configures the http sink.
type HTTPConfig struct {
	// A named field, not an embedded one: embedding would promote
	// ClientConfig.Unmarshal and skip decoding of the fields below.
	ClientConfig confighttp.ClientConfig `mapstructure:",squash"`
	// Token is sent as "Authorization: Bearer <token>".
	Token configopaque.String `mapstructure:"token"`
	// MaxBatchSize is the most records sent in one request.
	MaxBatchSize int `mapstructure:"max_batch_size"`

	_ struct{}
}

// AttributionConfig maps allocation fields to attributes or static defaults.
// A string value is an attribute alias, such as resource.attributes["cost.center"].
// For cost_center, agent_owner, and project_id that alias fills the field only
// when the span left it empty, and the fill is declared. For customer_id and
// environment it replaces the mapping file's sources. Defaults are static
// values applied after attributes, recorded as rule.
type AttributionConfig struct {
	CostCenter  string `mapstructure:"cost_center"`
	AgentOwner  string `mapstructure:"agent_owner"`
	ProjectID   string `mapstructure:"project_id"`
	CustomerID  string `mapstructure:"customer_id"`
	Environment string `mapstructure:"environment"`
	// Defaults are static values for common fields the span and the aliases left empty.
	Defaults AttributionDefaults `mapstructure:"defaults"`
	// LegacyGenAIAgentID is deprecated and off by default. When true,
	// gen_ai.agent.id is copied to agent_id, which is the v0.1.0 behavior,
	// and is not copied to external_identity_ref.
	LegacyGenAIAgentID bool `mapstructure:"legacy_gen_ai_agent_id"`
	// TagsFrom lists attribute keys copied into tags, span attributes first.
	TagsFrom []string `mapstructure:"tags_from"`

	_ struct{}
}

// AttributionDefaults holds static values for common fields. RuleID is the
// allocation_rule_id recorded when any default fills a field.
type AttributionDefaults struct {
	RuleID     string `mapstructure:"rule_id"`
	AgentID    string `mapstructure:"agent_id"`
	AgentOwner string `mapstructure:"agent_owner"`
	CostCenter string `mapstructure:"cost_center"`
	ProjectID  string `mapstructure:"project_id"`

	_ struct{}
}

func (d AttributionDefaults) values() map[string]string {
	return map[string]string{
		"agent_id":    strings.TrimSpace(d.AgentID),
		"agent_owner": strings.TrimSpace(d.AgentOwner),
		"cost_center": strings.TrimSpace(d.CostCenter),
		"project_id":  strings.TrimSpace(d.ProjectID),
	}
}

func (d AttributionDefaults) validate() error {
	n := 0
	for _, v := range d.values() {
		if v != "" {
			n++
		}
	}
	ruleID := strings.TrimSpace(d.RuleID)
	if n == 0 && ruleID == "" {
		return nil
	}
	if ruleID == "" {
		return errors.New("attribution.defaults.rule_id is required when a static default is set")
	}
	if n == 0 {
		return errors.New("attribution.defaults.rule_id is set but no default value is configured")
	}
	return nil
}

func (a AttributionConfig) overrides() map[string]string {
	return map[string]string{
		"cost_center": a.CostCenter,
		"agent_owner": a.AgentOwner,
		"project_id":  a.ProjectID,
		"customer_id": a.CustomerID,
		"environment": a.Environment,
	}
}

var _ component.Config = (*Config)(nil)

// Validate checks the configuration, including the mapping file.
func (cfg *Config) Validate() error {
	var errs []error
	switch cfg.Sink {
	case sinkFile:
		if cfg.File.Path == "" {
			errs = append(errs, errors.New("file.path is required when sink is file"))
		}
		if cfg.File.MaxMegabytes < 0 || cfg.File.MaxBackups < 0 {
			errs = append(errs, errors.New("file.max_megabytes and file.max_backups must not be negative"))
		}
	case sinkHTTP:
		if cfg.HTTP.ClientConfig.Endpoint == "" {
			errs = append(errs, errors.New("http.endpoint is required when sink is http"))
		}
		if cfg.HTTP.MaxBatchSize <= 0 {
			errs = append(errs, errors.New("http.max_batch_size must be positive"))
		}
	default:
		errs = append(errs, fmt.Errorf("sink must be %q or %q, got %q", sinkFile, sinkHTTP, cfg.Sink))
	}
	if strings.TrimSpace(cfg.Currency) == "" {
		errs = append(errs, errors.New("currency must not be empty"))
	}
	for i, k := range cfg.Attribution.TagsFrom {
		if strings.TrimSpace(k) == "" {
			errs = append(errs, fmt.Errorf("attribution.tags_from[%d] is empty", i))
		}
	}
	if err := cfg.Attribution.Defaults.validate(); err != nil {
		errs = append(errs, err)
	}
	if _, err := cfg.buildMapping(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (cfg *Config) buildMapping() (*mapping, error) {
	mf, err := loadMapping(cfg.MappingFile)
	if err != nil {
		return nil, err
	}
	return buildMapping(mf, cfg.Attribution.overrides(), cfg.Attribution.LegacyGenAIAgentID)
}
