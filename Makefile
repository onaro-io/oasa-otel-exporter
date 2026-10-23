OCB_VERSION ?= v0.162.0
SPEC_DIR    ?= ../oasa-spec

.PHONY: test lint integration otelcol builder sync-spec clean

test:
	go test -race -count=1 ./...

lint:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	go vet ./...
	go vet -tags integration ./...

builder:
	go install go.opentelemetry.io/collector/cmd/builder@$(OCB_VERSION)

otelcol: builder
	builder --config builder-config.yaml

integration: builder
	go test -tags integration -run TestCollectorEndToEnd -count=1 -v .

sync-spec:
	cp $(SPEC_DIR)/schema/0.1.3/oasa-record.schema.json testdata/schema/oasa-record.schema.json

clean:
	rm -rf _build
