// Copyright Onaro (BrianOnAI LLC)
// SPDX-License-Identifier: Apache-2.0

package oasaexporter

import (
	"strings"
	"testing"
)

func TestDefaultMappingIsValid(t *testing.T) {
	mf, err := loadMapping("")
	if err != nil {
		t.Fatal(err)
	}
	m, err := buildMapping(mf, nil)
	if err != nil {
		t.Fatalf("embedded mapping.yaml is invalid: %v", err)
	}
	if m.genAIPrefix != "gen_ai." || m.toolCallOperation != "tool_call" {
		t.Fatalf("unexpected mapping header: %+v", m)
	}
}

func TestParseSource(t *testing.T) {
	ok := map[string]attrScope{
		`span.attributes["gen_ai.agent.id"]`:  scopeSpan,
		`attributes["team"]`:                  scopeSpan,
		`resource.attributes["service.name"]`: scopeResource,
	}
	for expr, scope := range ok {
		s, err := parseSource(expr)
		if err != nil || s.scope != scope {
			t.Errorf("parseSource(%s) = %+v, %v", expr, s, err)
		}
	}
	for _, bad := range []string{`service.name`, `resource.attributes[service.name]`, `log.attributes["x"]`, ``} {
		if _, err := parseSource(bad); err == nil {
			t.Errorf("parseSource(%q) accepted an invalid expression", bad)
		}
	}
}

func TestBuildMappingRejectsInvalidFiles(t *testing.T) {
	cases := map[string]struct {
		mf   mappingFile
		want string
	}{
		"reserved field": {
			mappingFile{GenAIAttributePrefix: "gen_ai.", Fields: map[string][]string{"record_id": {`attributes["x"]`}}},
			"fields.record_id: set by the exporter",
		},
		"unknown field": {
			mappingFile{GenAIAttributePrefix: "gen_ai.", Fields: map[string][]string{"agent_mood": {`attributes["x"]`}}},
			"fields.agent_mood: not an OASA 0.1 field",
		},
		"translation to a non-enum value": {
			mappingFile{GenAIAttributePrefix: "gen_ai.", Translations: map[string]translation{"operation": {Values: map[string]string{"execute_tool": "execute_tool"}}}},
			`"execute_tool" is not a valid OASA value`,
		},
		"bad default": {
			mappingFile{GenAIAttributePrefix: "gen_ai.", Translations: map[string]translation{"environment": {Default: "production"}}},
			"translations.environment.default",
		},
		"empty prefix": {
			mappingFile{},
			"genai_attribute_prefix must not be empty",
		},
		"bad tool operation": {
			mappingFile{GenAIAttributePrefix: "gen_ai.", ToolCallOperation: "tool"},
			"tool_call_operation",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := buildMapping(&tc.mf, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("buildMapping() = %v, want error containing %q", err, tc.want)
			}
		})
	}
}

func TestTranslationApply(t *testing.T) {
	tr := translation{Values: map[string]string{"production": "prod"}, Default: "other"}
	if v, ok := tr.apply("production"); !ok || v != "prod" {
		t.Errorf("exact match = %q %v", v, ok)
	}
	if v, ok := tr.apply("PRODUCTION"); !ok || v != "prod" {
		t.Errorf("case-insensitive match = %q %v", v, ok)
	}
	if v, ok := tr.apply("qa"); !ok || v != "other" {
		t.Errorf("default = %q %v", v, ok)
	}
	noDefault := translation{Values: map[string]string{"a": "b"}}
	if _, ok := noDefault.apply("z"); ok {
		t.Error("value with no entry and no default should be omitted")
	}
}
