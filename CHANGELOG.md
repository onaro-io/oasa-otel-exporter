# Changelog

## Unreleased (v0.1.0)

First release of the OASA exporter for the OpenTelemetry Collector.

### Added
- `oasa` traces exporter: one OASA 0.1 `usage` record per span with `gen_ai.*` attributes; other spans are dropped. Nothing is priced.
- `file` sink: newline-delimited JSON, rotated by size, no network needed.
- `http` sink: batches (default 1,000 records) POSTed with a bearer token. Retries transient failures with the collector's backoff and honours `Retry-After`. A 429 carrying `X-Meridian-Limit-Type` is permanent: no retry, an error log, and the `otelcol_exporter_oasa_limit_rejected_records` metric.
- Deterministic UUIDv7 `record_id` from span end time and `trace_id/span_id`, per OASA SPEC.md 0.1.2, so retries deduplicate.
- `mapping.yaml`: field sources plus operation and environment translation tables, embedded by default and replaceable with `mapping_file`.
- `record` package: OASA 0.1 record type and serializer, usable on its own.
- Golden tests against the OASA spec repository's examples and schema; an `ocb` integration test.
