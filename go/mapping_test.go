package main

import (
	"encoding/json"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func TestParseMappingsDefaultsAndPreservesExplicitEmpty(t *testing.T) {
	defaults, errDefaults := parseMappings([]byte("priority: 100\n"))
	if errDefaults != nil {
		t.Fatalf("parse default mappings: %v", errDefaults)
	}
	if len(defaults) != 1 || defaults[0].Name != "Port" || defaults[0].Target != "gpt-5.5" {
		t.Fatalf("unexpected defaults: %#v", defaults)
	}

	empty, errEmpty := parseMappings([]byte("mappings: []\n"))
	if errEmpty != nil {
		t.Fatalf("parse explicit empty mappings: %v", errEmpty)
	}
	if len(empty) != 0 {
		t.Fatalf("explicit empty mappings = %#v", empty)
	}
}

func TestResolveSupportsCaseInsensitiveAliasesAndThinkingSuffix(t *testing.T) {
	store := newMappingStore()
	if errReplace := store.replaceConfig([]byte("mappings:\n  - name: Port\n    target: claude-sonnet-4\n")); errReplace != nil {
		t.Fatalf("replace mappings: %v", errReplace)
	}

	if target, ok := store.resolve("port"); !ok || target != "claude-sonnet-4" {
		t.Fatalf("resolve port = %q, %v", target, ok)
	}
	if target, ok := store.resolve("Port(high)"); !ok || target != "claude-sonnet-4(high)" {
		t.Fatalf("resolve Port(high) = %q, %v", target, ok)
	}
}

func TestRewriteRequestBody(t *testing.T) {
	body := rewriteRequestBody([]byte(`{"model":"Port","stream":false,"messages":[]}`), "gpt-5.5", true)
	var decoded map[string]any
	if errUnmarshal := json.Unmarshal(body, &decoded); errUnmarshal != nil {
		t.Fatalf("decode rewritten body: %v", errUnmarshal)
	}
	if decoded["model"] != "gpt-5.5" || decoded["stream"] != true {
		t.Fatalf("rewritten body = %#v", decoded)
	}
}

func TestModelRouteUsesDirectProviderWhenAvailable(t *testing.T) {
	store := newMappingStore()
	if errReplace := store.replaceConfig([]byte("mappings:\n  - name: Port\n    target: gpt-5.5\n    provider: codex\n")); errReplace != nil {
		t.Fatalf("replace mappings: %v", errReplace)
	}
	previous := mappings
	mappings = store
	t.Cleanup(func() { mappings = previous })

	raw, errMarshal := json.Marshal(rpcModelRouteRequest{ModelRouteRequest: pluginapi.ModelRouteRequest{
		RequestedModel:     "Port",
		AvailableProviders: []string{"claude", "codex"},
	}})
	if errMarshal != nil {
		t.Fatalf("marshal model route request: %v", errMarshal)
	}
	result, errRoute := handleModelRoute(raw)
	if errRoute != nil {
		t.Fatalf("handle model route: %v", errRoute)
	}
	var env pluginabi.Envelope
	if errUnmarshal := json.Unmarshal(result, &env); errUnmarshal != nil {
		t.Fatalf("decode model route envelope: %v", errUnmarshal)
	}
	var response pluginapi.ModelRouteResponse
	if errUnmarshal := json.Unmarshal(env.Result, &response); errUnmarshal != nil {
		t.Fatalf("decode model route response: %v", errUnmarshal)
	}
	if !response.Handled || response.TargetKind != pluginapi.ModelRouteTargetProvider || response.Target != "codex" || response.TargetModel != "gpt-5.5" {
		t.Fatalf("unexpected direct route response: %#v", response)
	}
}

func TestRequestInterceptRewritesDirectRouteModel(t *testing.T) {
	store := newMappingStore()
	if errReplace := store.replaceConfig([]byte("mappings:\n  - name: Port\n    target: claude-sonnet-4\n    provider: claude\n")); errReplace != nil {
		t.Fatalf("replace mappings: %v", errReplace)
	}
	previous := mappings
	mappings = store
	t.Cleanup(func() { mappings = previous })

	raw, errMarshal := json.Marshal(rpcRequestInterceptRequest{RequestInterceptRequest: pluginapi.RequestInterceptRequest{
		RequestedModel: "Port",
		Model:          "claude-sonnet-4",
		Stream:         true,
		Body:           []byte(`{"model":"Port","stream":true,"messages":[]}`),
	}})
	if errMarshal != nil {
		t.Fatalf("marshal interceptor request: %v", errMarshal)
	}
	result, errIntercept := handleRequestIntercept(raw)
	if errIntercept != nil {
		t.Fatalf("handle request intercept: %v", errIntercept)
	}
	var env pluginabi.Envelope
	if errUnmarshal := json.Unmarshal(result, &env); errUnmarshal != nil {
		t.Fatalf("decode interceptor envelope: %v", errUnmarshal)
	}
	var response pluginapi.RequestInterceptResponse
	if errUnmarshal := json.Unmarshal(env.Result, &response); errUnmarshal != nil {
		t.Fatalf("decode interceptor response: %v", errUnmarshal)
	}
	var body map[string]any
	if errUnmarshal := json.Unmarshal(response.Body, &body); errUnmarshal != nil {
		t.Fatalf("decode rewritten interceptor body: %v", errUnmarshal)
	}
	if body["model"] != "claude-sonnet-4" || body["stream"] != true {
		t.Fatalf("unexpected rewritten interceptor body: %#v", body)
	}
}
