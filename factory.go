// Copyright Onaro (BrianOnAI LLC)
// SPDX-License-Identifier: Apache-2.0

package oasaexporter

import (
	"context"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/confighttp"
	"go.opentelemetry.io/collector/config/configoptional"
	"go.opentelemetry.io/collector/config/configretry"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
)

const typeStr = "oasa"

var componentType = component.MustNewType(typeStr)

// NewFactory returns the factory for the oasa exporter.
func NewFactory() exporter.Factory {
	return exporter.NewFactory(
		componentType,
		createDefaultConfig,
		exporter.WithTraces(createTraces, component.StabilityLevelAlpha),
	)
}

func createDefaultConfig() component.Config {
	httpClient := confighttp.NewDefaultClientConfig()
	return &Config{
		TimeoutConfig: exporterhelper.NewDefaultTimeoutConfig(),
		QueueConfig:   configoptional.Some(exporterhelper.NewDefaultQueueConfig()),
		RetryConfig:   configretry.NewDefaultBackOffConfig(),
		Sink:          sinkFile,
		File: FileConfig{
			Path:         "oasa-usage.ndjson",
			MaxMegabytes: 100,
			MaxBackups:   10,
		},
		HTTP: HTTPConfig{
			ClientConfig: httpClient,
			MaxBatchSize: defaultMaxBatchSize,
		},
		Currency: "USD",
	}
}

func createTraces(ctx context.Context, set exporter.Settings, cfg component.Config) (exporter.Traces, error) {
	c := cfg.(*Config)
	exp, err := newOASAExporter(c, set)
	if err != nil {
		return nil, err
	}
	return exporterhelper.NewTraces(
		ctx,
		set,
		cfg,
		exp.pushTraces,
		exporterhelper.WithStart(exp.start),
		exporterhelper.WithShutdown(exp.shutdown),
		exporterhelper.WithTimeout(c.TimeoutConfig),
		exporterhelper.WithQueue(c.QueueConfig),
		exporterhelper.WithRetry(c.RetryConfig),
		exporterhelper.WithCapabilities(consumer.Capabilities{MutatesData: false}),
	)
}
