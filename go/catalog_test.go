package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
)

func TestKnownContextLengthMatchesModelNameFamilies(t *testing.T) {
	cases := []struct {
		model string
		want  int
	}{
		{"gpt-5.5", 272000},
		{"GPT-5.6-Sol", 921000},
		{"gpt-5-6-terra", 921000},
		{"openai/gpt-5.5", 272000},
		{"gpt-5.5(high)", 272000},
		{"gpt-5.4-mini", 400000},
		{"gemini-3.1-pro-preview", 1048576},
		{"claude-sonnet-4-6", 200000},
		{"claude-fable-5-1", 1000000},
		{"claude-opus-4-6", 200000},
		{"claude-opus-4-7", 1000000},
		{"kimi-k2-thinking", 131072},
		{"kimi-k2.7-code-highspeed", 262144},
		{"grok-4.20-multi-agent-0309", 2000000},
		{"glm-5-3", 1048576},
		{"glm-5-3-flash", 1000000},
		{"qwen3.8-flash", 0},
		{"", 0},
		{"   ", 0},
	}
	for _, item := range cases {
		if got := knownContextLength(item.model); got != item.want {
			t.Fatalf("knownContextLength(%q) = %d, want %d", item.model, got, item.want)
		}
	}
}

func TestModelCatalogPayloadIsSortedAndComplete(t *testing.T) {
	catalog := modelCatalog(nil)
	if catalog.DefaultContextLength != defaultContextLength || catalog.MaxContextLength != maxContextLength {
		t.Fatalf("catalog defaults = %d/%d", catalog.DefaultContextLength, catalog.MaxContextLength)
	}
	if len(catalog.Rules) != len(builtInContextLengths) {
		t.Fatalf("catalog rules = %d, want %d", len(catalog.Rules), len(builtInContextLengths))
	}
	for index := 1; index < len(catalog.Rules); index++ {
		if catalog.Rules[index-1].Pattern > catalog.Rules[index].Pattern {
			t.Fatalf("catalog rules are not sorted: %q before %q", catalog.Rules[index-1].Pattern, catalog.Rules[index].Pattern)
		}
	}
	for _, rule := range catalog.Rules {
		if strings.TrimSpace(rule.Pattern) == "" || rule.ContextLength <= 0 {
			t.Fatalf("invalid catalog rule: %#v", rule)
		}
		if rule.Source != contextLengthSourceBuiltIn {
			t.Fatalf("unexpected source %q for built-in rule %q", rule.Source, rule.Pattern)
		}
	}
}

func TestContextLengthOverridesReplaceAndExtendTheBuiltInTable(t *testing.T) {
	overrides := normalizeContextLengths(map[string]int{
		" GPT-5.5 ":     300000,
		"qwen3.8-flash": 1048576,
		"blank":         0,
		"":              5,
		"dropme":        -1,
	})

	if got := overrides.lookup("gpt-5.5"); got != 300000 {
		t.Fatalf("override of a built-in entry = %d, want 300000", got)
	}
	if got := overrides.lookup("openai/gpt-5.5(high)"); got != 300000 {
		t.Fatalf("override lookup with prefix and suffix = %d, want 300000", got)
	}
	if got := overrides.lookup("qwen3.8-flash"); got != 1048576 {
		t.Fatalf("new entry = %d, want 1048576", got)
	}
	// Untouched entries keep their built-in values.
	if got := overrides.lookup("gpt-5.6-sol"); got != 921000 {
		t.Fatalf("untouched entry = %d, want built-in 921000", got)
	}
	if _, present := overrides[""]; present {
		t.Fatalf("blank pattern should be dropped: %#v", overrides)
	}
	for _, pattern := range []string{"blank", "dropme"} {
		if _, present := overrides[pattern]; present {
			t.Fatalf("non-positive value for %q should be dropped", pattern)
		}
	}

	catalog := modelCatalog(overrides)
	seen := make(map[string]catalogRule, len(catalog.Rules))
	for _, rule := range catalog.Rules {
		seen[rule.Pattern] = rule
	}
	if rule, ok := seen["gpt-5.5"]; !ok || rule.ContextLength != 300000 || rule.Source != contextLengthSourceCustom {
		t.Fatalf("overridden entry = %#v", seen["gpt-5.5"])
	}
	if rule, ok := seen["qwen3.8-flash"]; !ok || rule.Source != contextLengthSourceCustom {
		t.Fatalf("added entry = %#v", seen["qwen3.8-flash"])
	}
	if len(catalog.Rules) != len(builtInContextLengths)+1 {
		t.Fatalf("catalog rules = %d, want %d", len(catalog.Rules), len(builtInContextLengths)+1)
	}
}

func TestMappingStoreAppliesContextLengthOverrides(t *testing.T) {
	store := newMappingStore()
	raw := []byte("mappings:\n  - name: Port\n    target: qwen3.8-flash\ncontext-lengths:\n  qwen3.8-flash: 262144\n")
	if errReplace := store.replaceConfig(raw); errReplace != nil {
		t.Fatalf("replace config with table: %v", errReplace)
	}
	infos := modelInfos(store.snapshot(), store.snapshotContextLengths())
	if len(infos) != 1 || infos[0].ContextLength != 262144 {
		t.Fatalf("model infos = %#v", infos)
	}

	// An explicit per-alias value still wins over the table.
	if errReplace := store.replaceConfig([]byte("mappings:\n  - name: Port\n    target: qwen3.8-flash\n    context-length: 64000\ncontext-lengths:\n  qwen3.8-flash: 262144\n")); errReplace != nil {
		t.Fatalf("replace config with explicit value: %v", errReplace)
	}
	infos = modelInfos(store.snapshot(), store.snapshotContextLengths())
	if len(infos) != 1 || infos[0].ContextLength != 64000 {
		t.Fatalf("explicit context length = %#v", infos)
	}
}

func TestParseRuntimeConfigReadsContextLengthOverrides(t *testing.T) {
	config, errParse := parseRuntimeConfig([]byte("mappings: []\ncontext-lengths:\n  gpt-5.5: 300000\n  gemini: 500000\n"))
	if errParse != nil {
		t.Fatalf("parse runtime config: %v", errParse)
	}
	if len(config.mappings) != 0 {
		t.Fatalf("explicit empty mappings lost: %#v", config.mappings)
	}
	if got := config.contextLengths.lookup("gpt-5.5"); got != 300000 {
		t.Fatalf("override = %d, want 300000", got)
	}
	if got := config.contextLengths.lookup("gemini-3.1-pro"); got != 500000 {
		t.Fatalf("family override = %d, want 500000", got)
	}
}

func TestManagementCatalogViewReturnsJSON(t *testing.T) {
	raw, errMarshal := json.Marshal(managementRequest{
		Method: "GET",
		Path:   "/v0/resource/plugins/unified-model/status",
		Query:  map[string][]string{"view": {"catalog"}},
	})
	if errMarshal != nil {
		t.Fatalf("marshal catalog request: %v", errMarshal)
	}
	result, errHandle := handleManagement(raw)
	if errHandle != nil {
		t.Fatalf("handle catalog request: %v", errHandle)
	}
	var env pluginabi.Envelope
	if errUnmarshal := json.Unmarshal(result, &env); errUnmarshal != nil {
		t.Fatalf("decode envelope: %v", errUnmarshal)
	}
	var response managementResponse
	if errUnmarshal := json.Unmarshal(env.Result, &response); errUnmarshal != nil {
		t.Fatalf("decode management response: %v", errUnmarshal)
	}
	if response.StatusCode != 200 || response.Headers.Get("Content-Type") != "application/json" {
		t.Fatalf("unexpected catalog response: %d %#v", response.StatusCode, response.Headers)
	}
	var catalog modelCatalogResponse
	if errUnmarshal := json.Unmarshal(response.Body, &catalog); errUnmarshal != nil {
		t.Fatalf("decode catalog body: %v", errUnmarshal)
	}
	if len(catalog.Rules) == 0 {
		t.Fatalf("catalog body has no rules")
	}
	for _, rule := range catalog.Rules {
		if rule.Source != contextLengthSourceBuiltIn && rule.Source != contextLengthSourceCustom {
			t.Fatalf("unexpected rule source %q for %q", rule.Source, rule.Pattern)
		}
	}
}

func TestManagementDefaultViewStillServesHTML(t *testing.T) {
	result, errHandle := handleManagement([]byte(`{"method":"GET","path":"/status"}`))
	if errHandle != nil {
		t.Fatalf("handle default request: %v", errHandle)
	}
	var env pluginabi.Envelope
	if errUnmarshal := json.Unmarshal(result, &env); errUnmarshal != nil {
		t.Fatalf("decode envelope: %v", errUnmarshal)
	}
	var response managementResponse
	if errUnmarshal := json.Unmarshal(env.Result, &response); errUnmarshal != nil {
		t.Fatalf("decode management response: %v", errUnmarshal)
	}
	if got := response.Headers.Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("unexpected content type %q", got)
	}
	if len(response.Body) == 0 {
		t.Fatalf("expected the management page body")
	}
}
