// Copyright Onaro (BrianOnAI LLC)
// SPDX-License-Identifier: Apache-2.0

package oasaexporter

import (
	"context"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/onaro-io/oasa-otel-exporter/record"
)

// sink delivers records somewhere. Errors returned from write follow the
// collector's convention: permanent errors are dropped, others are retried.
type sink interface {
	start(ctx context.Context, host component.Host) error
	write(ctx context.Context, records []*record.Record) error
	shutdown(ctx context.Context) error
}

type oasaExporter struct {
	translator *translator
	sink       sink
}

func newOASAExporter(cfg *Config, set exporter.Settings) (*oasaExporter, error) {
	m, err := cfg.buildMapping()
	if err != nil {
		return nil, err
	}
	var s sink
	switch cfg.Sink {
	case sinkHTTP:
		s, err = newHTTPSink(cfg.HTTP, set)
	default:
		s = newFileSink(cfg.File)
	}
	if err != nil {
		return nil, err
	}
	return &oasaExporter{
		translator: &translator{
			mapping:  m,
			currency: cfg.Currency,
			tagsFrom: cfg.Attribution.TagsFrom,
			now:      time.Now,
		},
		sink: s,
	}, nil
}

func (e *oasaExporter) start(ctx context.Context, host component.Host) error {
	return e.sink.start(ctx, host)
}

func (e *oasaExporter) shutdown(ctx context.Context) error {
	return e.sink.shutdown(ctx)
}

func (e *oasaExporter) pushTraces(ctx context.Context, td ptrace.Traces) error {
	records := e.translator.translate(td)
	if len(records) == 0 {
		return nil
	}
	return e.sink.write(ctx, records)
}
