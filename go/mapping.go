package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"gopkg.in/yaml.v3"
)

const (
	pluginID           = "unified-model"
	providerID         = pluginID
	defaultModelAlias  = "Port"
	defaultModelTarget = "gpt-5.5"
	// defaultContextLength is advertised when neither the mapping nor the built-in model
	// table provides a context window.
	defaultContextLength = 128000
	// defaultMaxCompletionTokens is advertised when a mapping context window is still larger.
	defaultMaxCompletionTokens = 32768
	// maxContextLength clamps configured values to a sane metadata range.
	maxContextLength = 100_000_000
)

// supportedModalities lists the selectable input modality names in canonical order.
var supportedModalities = []string{"text", "image", "audio", "video"}

// defaultModalities applies when a mapping lists no modality at all.
var defaultModalities = []string{"text"}

type modelMapping struct {
	Name     string `yaml:"name" json:"name"`
	Target   string `yaml:"target" json:"target"`
	Provider string `yaml:"provider,omitempty" json:"provider,omitempty"`
	// Auth optionally pins execution to one credential of the provider. The value is the
	// credential's auth ID, which is the auth file name for file-backed credentials. A
	// pinned credential runs through the plugin executor with a forced provider and auth
	// ID; without a pin the host keeps its native provider routing (direct provider
	// route or model-based fallback).
	Auth string `yaml:"auth,omitempty" json:"auth,omitempty"`
	// ContextLength is the context window advertised to clients for this alias.
	ContextLength int `yaml:"context-length,omitempty" json:"context-length,omitempty"`
	// Modalities lists the input modalities advertised to clients for this alias.
	Modalities []string `yaml:"modalities,omitempty" json:"modalities,omitempty"`
}

// clone returns a deep copy so callers never share the modality slice.
func (m modelMapping) clone() modelMapping {
	clone := m
	clone.Modalities = append([]string(nil), m.Modalities...)
	return clone
}

// pinnedAuthID returns the credential ID that locks execution to one auth record.
func (m modelMapping) pinnedAuthID() string {
	return strings.TrimSpace(m.Auth)
}

// forcedProvider reports the provider key to force on the host model execution path. It
// is only set together with a pinned credential: without a pin the host resolves the
// provider natively, so forcing it would needlessly disable the provider fallback.
func (m modelMapping) forcedProvider() string {
	if m.pinnedAuthID() == "" {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(m.Provider))
}

type mappingConfig struct {
	Mappings []modelMapping `yaml:"mappings" json:"mappings"`
	// ContextLengths overrides the plugin's built-in model name -> context window table.
	ContextLengths map[string]int `yaml:"context-lengths,omitempty" json:"context-lengths,omitempty"`
	// EnhancedMode hides native CPA models from client-facing listings so only plugin aliases remain.
	EnhancedMode bool `yaml:"enhanced-mode,omitempty" json:"enhanced-mode,omitempty"`
}

// runtimeConfig is the parsed plugin configuration used by the mapping store.
type runtimeConfig struct {
	mappings       []modelMapping
	contextLengths contextLengths
	enhancedMode   bool
}

type mappingStore struct {
	mu             sync.RWMutex
	mappings       []modelMapping
	contextLengths contextLengths
	enhancedMode   bool
}

func newMappingStore() *mappingStore {
	config := defaultRuntimeConfig()
	return &mappingStore{mappings: config.mappings, contextLengths: config.contextLengths}
}

func defaultRuntimeConfig() runtimeConfig {
	return runtimeConfig{mappings: normalizeMappings([]modelMapping{{Name: defaultModelAlias, Target: defaultModelTarget}})}
}

func defaultMappings() []modelMapping {
	return defaultRuntimeConfig().mappings
}

func (s *mappingStore) replaceConfig(raw []byte) error {
	config, errParse := parseRuntimeConfig(raw)
	if errParse != nil {
		return errParse
	}
	s.mu.Lock()
	s.mappings = config.mappings
	s.contextLengths = config.contextLengths
	s.enhancedMode = config.enhancedMode
	s.mu.Unlock()
	return nil
}

// snapshotEnhancedMode reports whether client-facing listings should only include plugin aliases.
func (s *mappingStore) snapshotEnhancedMode() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enhancedMode
}

// snapshotContextLengths returns a copy of the user override table.
func (s *mappingStore) snapshotContextLengths() contextLengths {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.contextLengths) == 0 {
		return nil
	}
	output := make(contextLengths, len(s.contextLengths))
	for pattern, value := range s.contextLengths {
		output[pattern] = value
	}
	return output
}

func (s *mappingStore) snapshot() []modelMapping {
	s.mu.RLock()
	defer s.mu.RUnlock()
	output := make([]modelMapping, 0, len(s.mappings))
	for _, mapping := range s.mappings {
		output = append(output, mapping.clone())
	}
	return output
}

func (s *mappingStore) resolve(requested string) (string, bool) {
	mapping, matched := s.resolveMapping(requested)
	if !matched {
		return "", false
	}
	return mapping.Target, true
}

func (s *mappingStore) resolveMapping(requested string) (modelMapping, bool) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return modelMapping{}, false
	}

	for _, mapping := range s.snapshot() {
		if strings.EqualFold(mapping.Name, requested) {
			return mapping.clone(), true
		}
	}

	// Preserve CPA's optional thinking suffix, for example Port(high).
	if suffixStart := strings.IndexByte(requested, '('); suffixStart > 0 && strings.HasSuffix(requested, ")") {
		base := strings.TrimSpace(requested[:suffixStart])
		suffix := requested[suffixStart:]
		for _, mapping := range s.snapshot() {
			if strings.EqualFold(mapping.Name, base) {
				mapping.Target = strings.TrimSpace(mapping.Target) + suffix
				return mapping.clone(), true
			}
		}
	}
	return modelMapping{}, false
}

func parseMappings(raw []byte) ([]modelMapping, error) {
	config, errParse := parseRuntimeConfig(raw)
	if errParse != nil {
		return nil, errParse
	}
	return config.mappings, nil
}

// parseRuntimeConfig decodes the plugin config: the alias mappings and the optional
// context length table overrides.
func parseRuntimeConfig(raw []byte) (runtimeConfig, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return defaultRuntimeConfig(), nil
	}

	var cfg mappingConfig
	if errUnmarshal := yaml.Unmarshal(raw, &cfg); errUnmarshal != nil {
		return runtimeConfig{}, fmt.Errorf("decode unified-model config: %w", errUnmarshal)
	}
	var fields map[string]any
	if errUnmarshal := yaml.Unmarshal(raw, &fields); errUnmarshal != nil {
		return runtimeConfig{}, fmt.Errorf("inspect unified-model config: %w", errUnmarshal)
	}

	output := runtimeConfig{
		contextLengths: normalizeContextLengths(cfg.ContextLengths),
		enhancedMode:   cfg.EnhancedMode,
	}
	if _, present := fields["mappings"]; !present {
		output.mappings = defaultMappings()
		return output, nil
	}
	output.mappings = normalizeMappings(cfg.Mappings)
	return output, nil
}

func normalizeMappings(input []modelMapping) []modelMapping {
	output := make([]modelMapping, 0, len(input))
	indexes := make(map[string]int, len(input))
	for _, item := range input {
		item.Name = strings.TrimSpace(item.Name)
		item.Target = strings.TrimSpace(item.Target)
		item.Provider = strings.ToLower(strings.TrimSpace(item.Provider))
		item.Auth = strings.TrimSpace(item.Auth)
		item.ContextLength = normalizeContextLength(item.ContextLength)
		item.Modalities = normalizeModalities(item.Modalities)
		if item.Name == "" {
			continue
		}
		key := strings.ToLower(item.Name)
		if index, exists := indexes[key]; exists {
			output[index] = item
			continue
		}
		indexes[key] = len(output)
		output = append(output, item)
	}
	return output
}

// normalizeContextLength keeps an explicit value, clamps it, and preserves 0 as "auto":
// the built-in model table decides the advertised context window in that case.
func normalizeContextLength(value int) int {
	if value <= 0 {
		return 0
	}
	if value > maxContextLength {
		return maxContextLength
	}
	return value
}

// effectiveContextLength resolves the advertised context window: explicit value first,
// then the context length table (user overrides merged over the built-in entries) for the
// target model, then the plugin default.
func effectiveContextLength(mapping modelMapping, lengths contextLengths) int {
	if value := normalizeContextLength(mapping.ContextLength); value > 0 {
		return value
	}
	if value := lengths.lookup(mapping.Target); value > 0 {
		return value
	}
	return defaultContextLength
}

// normalizeModalities keeps only known modality names, in canonical order, without duplicates.
func normalizeModalities(input []string) []string {
	selected := make(map[string]bool, len(input))
	for _, raw := range input {
		selected[strings.ToLower(strings.TrimSpace(raw))] = true
	}
	output := make([]string, 0, len(supportedModalities))
	for _, modality := range supportedModalities {
		if selected[modality] {
			output = append(output, modality)
		}
	}
	if len(output) == 0 {
		return append([]string(nil), defaultModalities...)
	}
	return output
}

func modelInfos(mappings []modelMapping, lengths contextLengths) []pluginapi.ModelInfo {
	models := make([]pluginapi.ModelInfo, 0, len(mappings))
	for _, mapping := range mappings {
		if strings.TrimSpace(mapping.Name) == "" {
			continue
		}
		contextLength := effectiveContextLength(mapping, lengths)
		maxCompletionTokens := defaultMaxCompletionTokens
		if maxCompletionTokens > contextLength {
			maxCompletionTokens = contextLength
		}
		models = append(models, pluginapi.ModelInfo{
			ID:                         mapping.Name,
			Object:                     "model",
			OwnedBy:                    providerID,
			DisplayName:                mapping.Name,
			Name:                       mapping.Name,
			Description:                "Unified model alias routed by the Unified Model plugin.",
			SupportedGenerationMethods: []string{"chat"},
			ContextLength:              int64(contextLength),
			MaxCompletionTokens:        int64(maxCompletionTokens),
			SupportedInputModalities:   normalizeModalities(mapping.Modalities),
			UserDefined:                true,
		})
	}
	return models
}

// aliasNameSet is the case-insensitive set of configured alias names.
func aliasNameSet(mappings []modelMapping) map[string]struct{} {
	names := make(map[string]struct{}, len(mappings))
	for _, mapping := range mappings {
		name := strings.ToLower(strings.TrimSpace(mapping.Name))
		if name == "" {
			continue
		}
		names[name] = struct{}{}
	}
	return names
}

func listingID(item map[string]any) string {
	for _, key := range []string{"id", "slug", "name", "model"} {
		raw, ok := item[key]
		if !ok || raw == nil {
			continue
		}
		switch value := raw.(type) {
		case string:
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func listingBaseName(id string) string {
	name := strings.TrimSpace(id)
	if name == "" {
		return ""
	}
	if start := strings.IndexByte(name, '('); start > 0 && strings.HasSuffix(name, ")") {
		name = strings.TrimSpace(name[:start])
	}
	const claudeCloakPrefix = "claude-fable-5-dd-"
	if strings.HasPrefix(name, claudeCloakPrefix) {
		encoded := name[len(claudeCloakPrefix):]
		runes := []rune(encoded)
		for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
			runes[i], runes[j] = runes[j], runes[i]
		}
		name = string(runes)
	}
	if slash := strings.LastIndexByte(name, '/'); slash >= 0 {
		name = strings.TrimSpace(name[slash+1:])
	}
	return name
}

func keepListedModel(item map[string]any, aliases map[string]struct{}) bool {
	id := listingBaseName(listingID(item))
	if id == "" {
		return false
	}
	_, ok := aliases[strings.ToLower(id)]
	return ok
}

func filterModelSlice(raw any, aliases map[string]struct{}) []any {
	slice, ok := raw.([]any)
	if !ok {
		return nil
	}
	filtered := make([]any, 0, len(slice))
	for _, item := range slice {
		object, okObject := item.(map[string]any)
		if !okObject || !keepListedModel(object, aliases) {
			continue
		}
		filtered = append(filtered, object)
	}
	return filtered
}

// filterClientModelListing keeps only plugin aliases in a client-facing model catalog.
// OpenAI/Grok use {data: [...]}, Gemini/Codex-client use {models: [...]}. Claude listings
// also carry first_id/last_id pagination cursors that must track the filtered slice.
func filterClientModelListing(body []byte, aliases map[string]struct{}) ([]byte, bool) {
	if len(body) == 0 {
		return nil, false
	}
	var object map[string]any
	if errUnmarshal := json.Unmarshal(body, &object); errUnmarshal != nil || object == nil {
		return nil, false
	}
	changed := false
	for _, key := range []string{"data", "models"} {
		raw, present := object[key]
		if !present {
			continue
		}
		filtered := filterModelSlice(raw, aliases)
		if filtered == nil {
			continue
		}
		object[key] = filtered
		changed = true
		if key != "data" {
			continue
		}
		if _, hasMore := object["has_more"]; hasMore {
			object["has_more"] = false
		}
		if _, hasFirst := object["first_id"]; hasFirst || len(filtered) > 0 {
			firstID := ""
			lastID := ""
			if len(filtered) > 0 {
				if first, ok := filtered[0].(map[string]any); ok {
					firstID = listingID(first)
				}
				if last, ok := filtered[len(filtered)-1].(map[string]any); ok {
					lastID = listingID(last)
				}
			}
			object["first_id"] = firstID
			object["last_id"] = lastID
		}
	}
	if !changed {
		return nil, false
	}
	encoded, errMarshal := json.Marshal(object)
	if errMarshal != nil {
		return nil, false
	}
	return encoded, true
}

func rewriteRequestBody(raw []byte, target string, stream bool) []byte {
	if len(raw) == 0 {
		body, errMarshal := json.Marshal(map[string]any{"model": target, "stream": stream})
		if errMarshal == nil {
			return body
		}
		return raw
	}
	var object map[string]any
	if errUnmarshal := json.Unmarshal(raw, &object); errUnmarshal != nil || object == nil {
		return append([]byte(nil), raw...)
	}
	object["model"] = target
	if _, hasStream := object["stream"]; hasStream {
		object["stream"] = stream
	}
	body, errMarshal := json.Marshal(object)
	if errMarshal != nil {
		return append([]byte(nil), raw...)
	}
	return body
}

func estimateTokens(raw []byte) int {
	if len(raw) == 0 {
		return 0
	}
	// This is only a fallback for clients that probe token counts; actual request
	// usage is still reported by the host model execution path.
	count := (len(raw) + 3) / 4
	if count < 1 {
		return 1
	}
	return count
}
