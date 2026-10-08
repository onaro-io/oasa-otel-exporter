# OASA exporter for the OpenTelemetry Collector

**Turns OpenTelemetry GenAI spans into [Open Agent Spend Attribution (OASA)](https://github.com/onaro-io/agent-spend-attribution) usage records, so model and tool calls can be attributed to the agent, task, and cost object that made them.**

The exporter receives traces. For every span carrying `gen_ai.*` attributes it emits one OASA `usage` record; spans without them are ignored. It writes records to a newline-delimited JSON file (no account or network needed) or POSTs them in batches to any HTTP endpoint that accepts OASA. It never prices anything: cost is joined downstream from billing data, for example through the [OASA ↔ FOCUS mapping](https://github.com/onaro-io/agent-spend-attribution/blob/main/docs/focus-mapping.md).

- Specification: [OASA SPEC.md](https://github.com/onaro-io/agent-spend-attribution/blob/main/SPEC.md)
- Record schema: [`oasa-record.schema.json`](https://github.com/onaro-io/agent-spend-attribution/blob/main/schema/oasa-record.schema.json) (every record this exporter writes validates against it)
- Component type: `oasa` · Stability: alpha (traces) · License: Apache-2.0

## Quick start (about five minutes)

You need [Go](https://go.dev/dl/) 1.26 or later, `git`, and `curl`.

1. Build a collector that includes the exporter:

   ```sh
   git clone https://github.com/onaro-io/oasa-otel-exporter.git
   cd oasa-otel-exporter
   go install go.opentelemetry.io/collector/cmd/builder@v0.162.0
   builder --config builder-config.yaml
   ```

   The binary is `_build/otelcol-oasa` (`_build\otelcol-oasa.exe` on Windows). Once `v0.1.0` is released, you can download a prebuilt binary from the release page instead.

2. Run it with the quick-start config, which listens for OTLP on `127.0.0.1:4318` and writes to `./oasa-usage.ndjson`:

   ```sh
   ./_build/otelcol-oasa --config examples/config.yaml
   ```

3. In a second terminal, send the sample trace (one OpenAI chat span and one plain HTTP span):

   ```sh
   curl -s -X POST -H "Content-Type: application/json" \
     --data @examples/trace.json http://127.0.0.1:4318/v1/traces
   ```

4. Open the output. There is one record; the HTTP span was dropped because it has no `gen_ai.*` attributes:

   ```sh
   cat oasa-usage.ndjson
   ```

   ```json
   {"record_id":"01a05d7f-b288-7532-8207-8cb53896d9e1","record_type":"usage","schema_version":"0.1","occurred_at":"2026-09-01T15:04:05Z","recorded_at":"…","source_system":"otel-collector","source_record_id":"4bf92f3577b34da6a3ce929d0e0e4736/00f067aa0ba902b7","currency":"USD","agent_id":"agent-support-47","agent_name":"Support Triage","session_id":"sess-2f91","trace_id":"4bf92f3577b34da6a3ce929d0e0e4736","span_id":"00f067aa0ba902b7","provider":"openai","model":"gpt-4.1","operation":"chat","input_tokens":1200,"output_tokens":340,"cached_tokens":800,"request_count":1,"cost_center":"Customer Success","project_id":"proj_support","environment":"prod"}
   ```

To use the exporter in your own collector build, add it to your `ocb` manifest:

```yaml
exporters:
  - gomod: github.com/onaro-io/oasa-otel-exporter v0.1.0
```

## Configuration

```yaml
exporters:
  oasa:
    sink: file                      # file | http
    file:
      path: /var/log/oasa/usage.ndjson
      max_megabytes: 100            # rotate at this size
      max_backups: 10               # rotated files to keep; 0 keeps all
    attribution:
      cost_center: resource.attributes["cost.center"]
      project_id: resource.attributes["project.id"]
      customer_id: span.attributes["app.customer.id"]
      environment: resource.attributes["deployment.environment.name"]
      tags_from: ["team", "owner"]  # copied into tags, span attribute first
    currency: USD                   # envelope currency; usage records carry no money
    mapping_file: ""                # optional replacement for the built-in mapping.yaml
```

The http sink:

```yaml
exporters:
  oasa:
    sink: http
    http:
      endpoint: https://ingest.example.com/v1/oasa/events
      token: ${env:OASA_TOKEN}      # sent as Authorization: Bearer <token>
      max_batch_size: 1000          # records per request
      timeout: 30s
      # plus every confighttp client setting: headers, tls, proxy_url, compression, auth
```

The standard exporter settings `timeout`, `retry_on_failure`, and `sending_queue` apply as in any collector exporter. See [`examples/config-http.yaml`](examples/config-http.yaml).

Attribution expressions are `span.attributes["key"]` or `resource.attributes["key"]`. They replace the default sources for that field.

## Mapping

The mapping lives in [`mapping.yaml`](mapping.yaml), not in code. It is embedded as the default; copy it and set `mapping_file` to change sources or translation tables. The exporter validates the file at startup and refuses to start if a field is not an OASA 0.1 field or a translation targets a value outside the OASA enum.

| OASA field | Source |
| --- | --- |
| `record_id` | Deterministic UUIDv7 (see below) |
| `record_type` | `usage` |
| `schema_version` | `0.1` |
| `occurred_at` / `recorded_at` | Span end time / export time, RFC 3339 UTC |
| `source_system` | `otel-collector` |
| `source_record_id` | `<trace_id>/<span_id>` |
| `currency` | `currency` setting (default `USD`) |
| `provider` | `gen_ai.provider.name`, else the deprecated `gen_ai.system` |
| `model` | `gen_ai.response.model`, else `gen_ai.request.model` |
| `operation` | `gen_ai.operation.name`, translated (table below); `tool_call` whenever `gen_ai.tool.name` is present |
| `input_tokens`, `output_tokens` | `gen_ai.usage.input_tokens`, `gen_ai.usage.output_tokens` |
| `cached_tokens`, `reasoning_tokens` | `gen_ai.usage.cache_read.input_tokens`, `gen_ai.usage.reasoning.output_tokens` |
| `tool_name` | `gen_ai.tool.name` |
| `agent_id`, `agent_name` | `gen_ai.agent.id`, `gen_ai.agent.name`; else resource `service.name` as `agent_name` with `agent_id` omitted |
| `session_id` | `gen_ai.conversation.id` |
| `trace_id`, `span_id` | From the span |
| `cost_center`, `project_id`, `customer_id` | `attribution` settings |
| `environment` | Resource `deployment.environment.name` (or deprecated `deployment.environment`), translated (table below) |
| `tags` | Attributes named in `attribution.tags_from` |
| `request_count` | `1` |

Translations into OASA enum values:

| `gen_ai.operation.name` | `operation` |
| --- | --- |
| `chat`, `generate_content` | `chat` |
| `text_completion` | `completion` |
| `embeddings` | `embeddings` |
| `execute_tool` | `tool_call` |
| `create_agent`, `invoke_agent`, anything else | `other` |

| `deployment.environment.name` (case-insensitive) | `environment` |
| --- | --- |
| `production`, `prod`, `live` | `prod` |
| `staging`, `stage`, `preprod` | `staging` |
| `development`, `dev`, `test`, `local` | `dev` |
| anything else | `other` |

A missing attribute leaves its field out of the record; the exporter never writes `null`. Token counts sent as integral doubles or numeric strings are accepted; anything else is omitted.

## Deterministic record IDs

`record_id` is derived from the record itself, so a retried export produces the same ID and the receiver can deduplicate it. The derivation is the one in [SPEC.md "Deterministic record IDs"](https://github.com/onaro-io/agent-spend-attribution/blob/main/SPEC.md#deterministic-record-ids):

1. `ms` = span end time in Unix milliseconds, truncated.
2. `h` = SHA-256 of `<trace_id>/<span_id>` (lowercase hex, the record's `source_record_id`).
3. Bytes 0–5 = `ms` big-endian; byte 6 = `0x70 | (h[0] & 0x0F)`; byte 7 = `h[1]`; byte 8 = `0x80 | (h[2] & 0x3F)`; bytes 9–15 = `h[3..9]`.
4. Format as a lowercase hyphenated UUID.

Test vector: span end `2026-09-01T15:04:05Z`, `source_record_id` `4bf92f3577b34da6a3ce929d0e0e4736/00f067aa0ba902b7` gives `01a05d7f-b288-7532-8207-8cb53896d9e1`. The Go implementation is `record.DeterministicID`.

## HTTP sink behaviour

Each request body is an [OASA batch](https://github.com/onaro-io/agent-spend-attribution/blob/main/SPEC.md#batch-envelope): `{"schema_version","source_system","emitted_at","count","records":[…]}`.

| Response | What the exporter does |
| --- | --- |
| 2xx | Done. If the body lists `rejected` records, logs a warning with the first reason; those records are not retried. |
| 429 with `X-Meridian-Limit-Type` | Billing limit reached. Drops the batch and the rest of the request **without retry**, logs an error with the limit type and reset date, and increments `otelcol_exporter_oasa_limit_rejected_records` (attribute `limit_type`). |
| 429 or 503 with `Retry-After` | Retries after the requested delay. |
| 408, 429, 5xx | Retries with the configured backoff. |
| Other 4xx | Drops the batch without retry (permanent error). |

Alert on `otelcol_exporter_oasa_limit_rejected_records > 0`: it means usage is being lost until the receiver's limit resets or is raised.

## Development

```sh
make test         # unit and golden tests, with the race detector
make lint         # gofmt and go vet
make integration  # builds a collector with ocb and runs a trace end to end
make sync-spec    # refresh testdata from a sibling checkout of the spec repository
```

The golden records in `testdata/golden/` and the schema in `testdata/schema/` are copies from the [spec repository](https://github.com/onaro-io/agent-spend-attribution) (`examples/` and `schema/`). The golden tests reproduce them exactly, normalizing only `recorded_at`.

The `record` package is importable on its own (`github.com/onaro-io/oasa-otel-exporter/record`) for anything that needs to write OASA 0.1 records.

See [CONTRIBUTING.md](CONTRIBUTING.md). Report security issues as described in [SECURITY.md](SECURITY.md).

## License

Apache-2.0. See [LICENSE](LICENSE). The OASA specification itself is published under CC BY 4.0.
