// Copyright Onaro (BrianOnAI LLC)
// SPDX-License-Identifier: Apache-2.0

package oasaexporter

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/onaro-io/oasa-otel-exporter/record"
)

//go:embed mapping.yaml
var defaultMappingYAML []byte

// Fields the exporter sets itself; a mapping file may not override them.
var reservedFields = map[string]bool{
	"record_id": true, "record_type": true, "schema_version": true,
	"occurred_at": true, "recorded_at": true, "source_system": true,
	"source_record_id": true, "currency": true, "trace_id": true,
	"span_id": true, "request_count": true, "tags": true,
}

type attrScope int

const (
	scopeSpan attrScope = iota + 1
	scopeResource
)

// source is one parsed attribute reference, e.g. resource.attributes["service.name"].
type source struct {
	scope attrScope
	key   string
	text  string
}

var sourcePattern = regexp.MustCompile(`^(span\.|resource\.)?attributes\["([^"]+)"\]$`)

func parseSource(expr string) (source, error) {
	expr = strings.TrimSpace(expr)
	m := sourcePattern.FindStringSubmatch(expr)
	if m == nil {
		return source{}, fmt.Errorf(`%q: expected span.attributes["key"] or resource.attributes["key"]`, expr)
	}
	s := source{scope: scopeSpan, key: m[2], text: expr}
	if m[1] == "resource." {
		s.scope = scopeResource
	}
	return s, nil
}

type translation struct {
	Values  map[string]string `yaml:"values"`
	Default string            `yaml:"default"`
}

type mappingFile struct {
	GenAIAttributePrefix string                 `yaml:"genai_attribute_prefix"`
	Fields               map[string][]string    `yaml:"fields"`
	Translations         map[string]translation `yaml:"translations"`
	ToolCallOperation    string                 `yaml:"tool_call_operation"`
}

// fieldMapping is the resolved mapping for one OASA field.
type fieldMapping struct {
	field       string
	kind        record.FieldKind
	sources     []source
	translation *translation
}

// mapping is a validated, ready-to-use mapping file.
type mapping struct {
	genAIPrefix       string
	fields            []fieldMapping
	toolCallOperation string
}

func loadMapping(path string) (*mappingFile, error) {
	data := defaultMappingYAML
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading mapping file: %w", err)
		}
		data = b
	}
	var mf mappingFile
	if err := yaml.Unmarshal(data, &mf); err != nil {
		return nil, fmt.Errorf("parsing mapping file: %w", err)
	}
	return &mf, nil
}

// buildMapping validates a mapping file and applies attribution overrides,
// which replace the file's sources for the fields they name.
func buildMapping(mf *mappingFile, overrides map[string]string) (*mapping, error) {
	var errs []error
	m := &mapping{genAIPrefix: mf.GenAIAttributePrefix, toolCallOperation: mf.ToolCallOperation}
	if m.genAIPrefix == "" {
		errs = append(errs, errors.New("genai_attribute_prefix must not be empty"))
	}
	if m.toolCallOperation != "" && !record.IsEnumValue("operation", m.toolCallOperation) {
		errs = append(errs, fmt.Errorf("tool_call_operation %q is not an OASA operation", m.toolCallOperation))
	}

	exprs := map[string][]string{}
	for f, list := range mf.Fields {
		exprs[f] = list
	}
	for f, expr := range overrides {
		if expr != "" {
			exprs[f] = []string{expr}
		}
	}

	for _, name := range record.FieldNames() {
		list, ok := exprs[name]
		if !ok {
			continue
		}
		delete(exprs, name)
		if reservedFields[name] {
			errs = append(errs, fmt.Errorf("fields.%s: set by the exporter and cannot be mapped", name))
			continue
		}
		kind, _ := record.Kind(name)
		fm := fieldMapping{field: name, kind: kind}
		for _, e := range list {
			s, err := parseSource(e)
			if err != nil {
				errs = append(errs, fmt.Errorf("fields.%s: %w", name, err))
				continue
			}
			fm.sources = append(fm.sources, s)
		}
		if len(fm.sources) == 0 {
			errs = append(errs, fmt.Errorf("fields.%s: no sources", name))
		}
		if t, ok := mf.Translations[name]; ok {
			t := t
			fm.translation = &t
		}
		m.fields = append(m.fields, fm)
	}
	for name := range exprs {
		errs = append(errs, fmt.Errorf("fields.%s: not an OASA 0.1 field", name))
	}

	for name, t := range mf.Translations {
		if _, ok := record.Kind(name); !ok {
			errs = append(errs, fmt.Errorf("translations.%s: not an OASA 0.1 field", name))
			continue
		}
		for from, to := range t.Values {
			if !record.IsEnumValue(name, to) {
				errs = append(errs, fmt.Errorf("translations.%s.values.%s: %q is not a valid OASA value", name, from, to))
			}
		}
		if t.Default != "" && !record.IsEnumValue(name, t.Default) {
			errs = append(errs, fmt.Errorf("translations.%s.default: %q is not a valid OASA value", name, t.Default))
		}
	}

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return m, nil
}

// apply translates a raw source value. ok is false when the field should be omitted.
func (t *translation) apply(value string) (string, bool) {
	if to, ok := t.Values[value]; ok {
		return to, true
	}
	if to, ok := t.Values[strings.ToLower(value)]; ok {
		return to, true
	}
	if t.Default != "" {
		return t.Default, true
	}
	return "", false
}
