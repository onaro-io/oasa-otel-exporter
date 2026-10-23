# Changelog

## v0.2.0 - unreleased

Proposed. Not released.

### Changed
- `agent_id` is read from `oasa.agent.id`. `gen_ai.agent.id`, or else `gen_ai.main_agent.id`, is written to `external_identity_ref` with `identity_scheme` `other`. Neither hosted id is copied into `agent_id` or `parent_agent_id`.
- `cost_center`, `agent_owner`, and `project_id` are read from `oasa.cost_center`, `oasa.agent.owner`, and `oasa.project.id`. An `attribution` setting for one of those fields fills it only when the span left it empty.
- A set common field carries per-field provenance. An attribute read, including a configured attribute alias, is `declared`, and `identity_type` is the attribute that was read. A configured static default is `rule`, with `allocation_rule_id` and `identity_value` set to that rule id and `identity_type` `oasa.allocation_rule`.

### Added
- `attribution.legacy_gen_ai_agent_id`, deprecated and off by default. When true, `gen_ai.agent.id` is copied to `agent_id`, which is the v0.1.0 behavior, and is not copied to `external_identity_ref`.

## v0.1.0 - 2026-10-08

First release of the OASA exporter for the OpenTelemetry Collector.

### Added
- `oasa` traces exporter: one OASA 0.1 `usage` record per span with `gen_ai.*` attributes; other spans are dropped. Nothing is priced.
- `file` sink: newline-delimited JSON, rotated by size, no network needed.
- `http` sink: batches (default 1,000 records) POSTed with a bearer token. Retries transient failures with the collector's backoff and honours `Retry-After`. A 429 carrying `X-Meridian-Limit-Type` is permanent: no retry, an error log, and the `otelcol_exporter_oasa_limit_rejected_records` metric.
- Deterministic UUIDv7 `record_id` from span end time and `trace_id/span_id`, per OASA SPEC.md 0.1.2, so retries deduplicate.
- `mapping.yaml`: field sources plus operation and environment translation tables, embedded by default and replaceable with `mapping_file`.
- `record` package: OASA 0.1 record type and serializer, usable on its own.
- Golden tests against the OASA spec repository's examples and schema; an `ocb` integration test.
