// Copyright Onaro (BrianOnAI LLC)
// SPDX-License-Identifier: Apache-2.0

package oasaexporter

import (
	"bytes"
	"context"
	"sync"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer/consumererror"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/onaro-io/oasa-otel-exporter/record"
)

// fileSink writes newline-delimited JSON and rotates by size. It needs no network.
type fileSink struct {
	mu sync.Mutex
	w  *lumberjack.Logger
}

func newFileSink(cfg FileConfig) *fileSink {
	return &fileSink{w: &lumberjack.Logger{
		Filename:   cfg.Path,
		MaxSize:    cfg.MaxMegabytes,
		MaxBackups: cfg.MaxBackups,
	}}
}

func (s *fileSink) start(context.Context, component.Host) error { return nil }

func (s *fileSink) write(_ context.Context, records []*record.Record) error {
	var buf bytes.Buffer
	for _, r := range records {
		b, err := record.Marshal(r)
		if err != nil {
			return consumererror.NewPermanent(err)
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.w.Write(buf.Bytes())
	return err
}

func (s *fileSink) shutdown(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Close()
}
