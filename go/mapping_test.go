package main

import (
	"encoding/json"
	"strings"
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

func TestParseMappingsReadsContextLengthAndModalities(t *testing.T) {
	parsed, errParse := parseMappings([]byte("mappings:\n  - name: Port\n    target: gpt-5.5\n    context-length: 200000\n    modalities:\n      - image\n      - text\n"))
	if errParse != nil {
		t.Fatalf("parse mappings with context and modalities: %v", errParse)
	}
	if len(parsed) != 1 {
		t.Fatalf("parsed mappings = %#v", parsed)
	}
	if parsed[0].ContextLength != 200000 {
		t.Fatalf("context length = %d", parsed[0].ContextLength)
	}
	if got := strings.Join(parsed[0].Modalities, ","); got != "text,image" {
		t.Fatalf("modalities = %q, want canonical text,image", got)
	}
}

func TestNormalizeMappingsDefaultsAndClampsContextAndModalities(t *testing.T) {
	normalized := normalizeMappings([]modelMapping{
		{Name: "Port", Target: "gpt-5.5", Modalities: []string{"IMAGE", "text", "image", "", "bogus"}},
		{Name: "Small", Target: "gpt-5.5", ContextLength: -8},
		{Name: "Big", Target: "gpt-5.5", ContextLength: maxContextLength * 2, Modalities: []string{"audio"}},
	})
	if len(normalized) != 3 {
		t.Fatalf("normalized mappings = %#v", normalized)
	}
	// An omitted context length stays 0 so the built-in model table can apply.
	if normalized[0].ContextLength != 0 {
		t.Fatalf("unset context length = %d, want auto (0)", normalized[0].ContextLength)
	}
	if got := strings.Join(normalized[0].Modalities, ","); got != "text,image" {
		t.Fatalf("modalities = %q, want deduplicated canonical text,image", got)
	}
	if normalized[1].ContextLength != 0 {
		t.Fatalf("negative context length = %d, want auto (0)", normalized[1].ContextLength)
	}
	if got := strings.Join(normalized[1].Modalities, ","); got != "text" {
		t.Fatalf("default modalities = %q, want text", got)
	}
	if normalized[2].ContextLength != maxContextLength {
		t.Fatalf("clamped context length = %d, want %d", normalized[2].ContextLength, maxContextLength)
	}
	if got := strings.Join(normalized[2].Modalities, ","); got != "audio" {
		t.Fatalf("modalities = %q, want audio", got)
	}
}

func TestModelInfosAdvertiseConfiguredContextAndModalities(t *testing.T) {
	infos := modelInfos([]modelMapping{
		{Name: "Port", Target: "gpt-5.5", ContextLength: 200000, Modalities: []string{"text", "image", "video"}},
		{Name: "Default", Target: "gpt-5.5"},
		{Name: "Fallback", Target: "unknown-vendor-model"},
		{Name: "Tiny", Target: "gpt-5.5", ContextLength: 100},
	}, nil)
	if len(infos) != 4 {
		t.Fatalf("model infos = %#v", infos)
	}
	if infos[0].ContextLength != 200000 {
		t.Fatalf("context length = %d, want 200000", infos[0].ContextLength)
	}
	if infos[0].MaxCompletionTokens != defaultMaxCompletionTokens {
		t.Fatalf("max completion tokens = %d, want %d", infos[0].MaxCompletionTokens, defaultMaxCompletionTokens)
	}
	if got := strings.Join(infos[0].SupportedInputModalities, ","); got != "text,image,video" {
		t.Fatalf("modalities = %q", got)
	}
	// Without an explicit value the built-in table for the target model applies.
	if infos[1].ContextLength != 272000 {
		t.Fatalf("built-in table context length = %d, want 272000", infos[1].ContextLength)
	}
	if got := strings.Join(infos[1].SupportedInputModalities, ","); got != "text" {
		t.Fatalf("modalities = %q, want default text", got)
	}
	// Unknown names fall back to the plugin default.
	if infos[2].ContextLength != defaultContextLength {
		t.Fatalf("fallback context length = %d, want default %d", infos[2].ContextLength, defaultContextLength)
	}
	// The advertised completion cap never exceeds the advertised context window.
	if infos[3].ContextLength != 100 || infos[3].MaxCompletionTokens != 100 {
		t.Fatalf("tiny model infos = %d/%d, want clamped 100/100", infos[3].ContextLength, infos[3].MaxCompletionTokens)
	}
}

func TestEffectiveContextLengthPrefersExplicitThenBuiltInTable(t *testing.T) {
	cases := []struct {
		mapping modelMapping
		want    int
	}{
		{modelMapping{Name: "Port", Target: "gpt-5.5", ContextLength: 64000}, 64000},
		{modelMapping{Name: "Port", Target: "gpt-5.5"}, 272000},
		{modelMapping{Name: "Port", Target: "gemini-3.1-pro"}, 1048576},
		{modelMapping{Name: "Port", Target: "qwen3.8-flash"}, defaultContextLength},
	}
	for _, item := range cases {
		if got := effectiveContextLength(item.mapping, nil); got != item.want {
			t.Fatalf("effective context length for %q = %d, want %d", item.mapping.Target, got, item.want)
		}
	}
}

func TestSnapshotCopiesModalites(t *testing.T) {
	store := newMappingStore()
	if errReplace := store.replaceConfig([]byte("mappings:\n  - name: Port\n    target: gpt-5.5\n    modalities: [text, image]\n")); errReplace != nil {
		t.Fatalf("replace mappings: %v", errReplace)
	}
	snapshot := store.snapshot()
	if len(snapshot) != 1 || strings.Join(snapshot[0].Modalities, ",") != "text,image" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	snapshot[0].Modalities[0] = "mutated"
	if again := store.snapshot(); strings.Join(again[0].Modalities, ",") != "text,image" {
		t.Fatalf("snapshot modalities were shared: %#v", again[0].Modalities)
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

func TestModelRoutePinsCredentialThroughSelfExecutor(t *testing.T) {
	store := newMappingStore()
	if errReplace := store.replaceConfig([]byte("mappings:\n  - name: Port\n    target: gpt-5.5\n    provider: codex\n    auth: codex-account-a.json\n")); errReplace != nil {
		t.Fatalf("replace mappings: %v", errReplace)
	}
	previous := mappings
	mappings = store
	t.Cleanup(func() { mappings = previous })

	// The direct provider route cannot carry a credential pin, so a pinned mapping must
	// be handled by the plugin executor even when the provider is available.
	raw, errMarshal := json.Marshal(rpcModelRouteRequest{ModelRouteRequest: pluginapi.ModelRouteRequest{
		RequestedModel:     "Port",
		AvailableProviders: []string{"codex"},
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
	if !response.Handled || response.TargetKind != pluginapi.ModelRouteTargetSelf {
		t.Fatalf("unexpected pinned route response: %#v", response)
	}
}

func TestMappingsParsePinnedCredential(t *testing.T) {
	parsed, errParse := parseMappings([]byte("mappings:\n  - name: Port\n    target: gpt-5.5\n    provider: Codex\n    auth:  codex-a.json \n"))
	if errParse != nil {
		t.Fatalf("parse mappings with pinned credential: %v", errParse)
	}
	if len(parsed) != 1 {
		t.Fatalf("parsed mappings = %#v", parsed)
	}
	if parsed[0].Provider != "codex" || parsed[0].pinnedAuthID() != "codex-a.json" || parsed[0].forcedProvider() != "codex" {
		t.Fatalf("parsed pinned mapping = %#v", parsed[0])
	}
	// Without a pin the forced provider stays empty so the host routes natively.
	unpinned := modelMapping{Target: "gpt-5.5", Provider: "codex"}
	if unpinned.pinnedAuthID() != "" || unpinned.forcedProvider() != "" {
		t.Fatalf("unpinned mapping should not force a provider: %#v", unpinned)
	}
}

func TestHostModelRequestCarriesPinnedCredential(t *testing.T) {
	pinned := modelMapping{Name: "Port", Target: "gpt-5.5", Provider: "codex", Auth: "codex-a.json"}
	request := hostModelRequestFromExecutor(rpcExecutorRequest{ExecutorRequest: pluginapi.ExecutorRequest{SourceFormat: "chat-completions", Format: "chat-completions"}}, pinned, true)
	if request.AuthID != "codex-a.json" || request.ForcedProvider != "codex" {
		t.Fatalf("pinned host request = %#v", request.HostModelExecutionRequest)
	}
	if request.Model != "gpt-5.5" || !request.Stream {
		t.Fatalf("unexpected host request model/stream: %#v", request.HostModelExecutionRequest)
	}

	unpinned := modelMapping{Name: "Port", Target: "gpt-5.5", Provider: "codex"}
	request = hostModelRequestFromExecutor(rpcExecutorRequest{}, unpinned, false)
	if request.AuthID != "" || request.ForcedProvider != "" {
		t.Fatalf("unpinned host request must keep native routing: %#v", request.HostModelExecutionRequest)
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

func TestParseRuntimeConfigReadsEnhancedMode(t *testing.T) {
	store := newMappingStore()
	if store.snapshotEnhancedMode() {
		t.Fatal("enhanced mode should be off by default")
	}
	if errReplace := store.replaceConfig([]byte("mappings:\n  - name: Port\n    target: gpt-5.5\nenhanced-mode: true\n")); errReplace != nil {
		t.Fatalf("replace config with enhanced mode: %v", errReplace)
	}
	if !store.snapshotEnhancedMode() {
		t.Fatal("enhanced mode = false, want true")
	}
}

func TestFilterClientModelListingKeepsAliasesAcrossCatalogs(t *testing.T) {
	aliases := aliasNameSet([]modelMapping{{Name: "Port"}, {Name: "Flash"}})

	openai, changed := filterClientModelListing([]byte(`{"object":"list","data":[{"id":"gpt-5.5"},{"id":"Port"},{"id":"Flash(high)"}]}`), aliases)
	if !changed {
		t.Fatal("openai listing was not filtered")
	}
	var openaiBody map[string]any
	if errUnmarshal := json.Unmarshal(openai, &openaiBody); errUnmarshal != nil {
		t.Fatalf("decode openai listing: %v", errUnmarshal)
	}
	data, _ := openaiBody["data"].([]any)
	if len(data) != 2 {
		t.Fatalf("openai listing = %#v, want Port and Flash(high)", openaiBody)
	}

	claude, changedClaude := filterClientModelListing([]byte(`{"data":[{"id":"claude-sonnet-4"},{"id":"claude-fable-5-dd-troP"}],"has_more":true,"first_id":"claude-sonnet-4","last_id":"x"}`), aliases)
	if !changedClaude {
		t.Fatal("claude listing was not filtered")
	}
	var claudeBody map[string]any
	if errUnmarshal := json.Unmarshal(claude, &claudeBody); errUnmarshal != nil {
		t.Fatalf("decode claude listing: %v", errUnmarshal)
	}
	if claudeBody["has_more"] != false || claudeBody["first_id"] != "claude-fable-5-dd-troP" || claudeBody["last_id"] != "claude-fable-5-dd-troP" {
		t.Fatalf("claude listing cursors = %#v", claudeBody)
	}

	gemini, changedGemini := filterClientModelListing([]byte(`{"models":[{"name":"models/gemini-3.1-pro"},{"name":"models/Port"}]}`), aliases)
	if !changedGemini {
		t.Fatal("gemini listing was not filtered")
	}
	var geminiBody map[string]any
	if errUnmarshal := json.Unmarshal(gemini, &geminiBody); errUnmarshal != nil {
		t.Fatalf("decode gemini listing: %v", errUnmarshal)
	}
	models, _ := geminiBody["models"].([]any)
	if len(models) != 1 {
		t.Fatalf("gemini listing = %#v", geminiBody)
	}
}

func TestHandleResponseInterceptFiltersWhenEnhancedModeEnabled(t *testing.T) {
	store := newMappingStore()
	if errReplace := store.replaceConfig([]byte("enhanced-mode: true\nmappings:\n  - name: Port\n    target: gpt-5.5\n")); errReplace != nil {
		t.Fatalf("replace config: %v", errReplace)
	}
	previous := mappings
	mappings = store
	t.Cleanup(func() { mappings = previous })

	raw, errMarshal := json.Marshal(rpcResponseInterceptRequest{ResponseInterceptRequest: pluginapi.ResponseInterceptRequest{
		Body: []byte(`{"object":"list","data":[{"id":"gpt-5.5","owned_by":"openai"},{"id":"Port","owned_by":"unified-model"}]}`),
	}})
	if errMarshal != nil {
		t.Fatalf("marshal response intercept request: %v", errMarshal)
	}
	result, errIntercept := handleResponseIntercept(raw)
	if errIntercept != nil {
		t.Fatalf("handle response intercept: %v", errIntercept)
	}
	var env pluginabi.Envelope
	if errUnmarshal := json.Unmarshal(result, &env); errUnmarshal != nil {
		t.Fatalf("decode intercept envelope: %v", errUnmarshal)
	}
	var response pluginapi.ResponseInterceptResponse
	if errUnmarshal := json.Unmarshal(env.Result, &response); errUnmarshal != nil {
		t.Fatalf("decode intercept response: %v", errUnmarshal)
	}
	var body map[string]any
	if errUnmarshal := json.Unmarshal(response.Body, &body); errUnmarshal != nil {
		t.Fatalf("decode filtered listing: %v", errUnmarshal)
	}
	data, _ := body["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("filtered listing = %#v", body)
	}
	item, _ := data[0].(map[string]any)
	if item["id"] != "Port" {
		t.Fatalf("kept model = %#v, want Port", item)
	}
}

func TestHandleResponseInterceptLeavesListingsWhenEnhancedModeDisabled(t *testing.T) {
	store := newMappingStore()
	if errReplace := store.replaceConfig([]byte("mappings:\n  - name: Port\n    target: gpt-5.5\n")); errReplace != nil {
		t.Fatalf("replace config: %v", errReplace)
	}
	previous := mappings
	mappings = store
	t.Cleanup(func() { mappings = previous })

	raw, errMarshal := json.Marshal(rpcResponseInterceptRequest{ResponseInterceptRequest: pluginapi.ResponseInterceptRequest{
		Body: []byte(`{"object":"list","data":[{"id":"gpt-5.5"},{"id":"Port"}]}`),
	}})
	if errMarshal != nil {
		t.Fatalf("marshal response intercept request: %v", errMarshal)
	}
	result, errIntercept := handleResponseIntercept(raw)
	if errIntercept != nil {
		t.Fatalf("handle response intercept: %v", errIntercept)
	}
	var env pluginabi.Envelope
	if errUnmarshal := json.Unmarshal(result, &env); errUnmarshal != nil {
		t.Fatalf("decode intercept envelope: %v", errUnmarshal)
	}
	var response pluginapi.ResponseInterceptResponse
	if errUnmarshal := json.Unmarshal(env.Result, &response); errUnmarshal != nil {
		t.Fatalf("decode intercept response: %v", errUnmarshal)
	}
	if len(response.Body) != 0 {
		t.Fatalf("disabled enhanced mode still rewrote listing: %s", response.Body)
	}
}
