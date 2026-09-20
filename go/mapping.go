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
)

type modelMapping struct {
	Name     string `yaml:"name" json:"name"`
	Target   string `yaml:"target" json:"target"`
	Provider string `yaml:"provider,omitempty" json:"provider,omitempty"`
}

type mappingConfig struct {
	Mappings []modelMapping `yaml:"mappings" json:"mappings"`
}

type mappingStore struct {
	mu       sync.RWMutex
	mappings []modelMapping
}

func newMappingStore() *mappingStore {
	return &mappingStore{mappings: defaultMappings()}
}

func defaultMappings() []modelMapping {
	return []modelMapping{{Name: defaultModelAlias, Target: defaultModelTarget}}
}

func (s *mappingStore) replaceConfig(raw []byte) error {
	mappings, errParse := parseMappings(raw)
	if errParse != nil {
		return errParse
	}
	s.mu.Lock()
	s.mappings = mappings
	s.mu.Unlock()
	return nil
}

func (s *mappingStore) snapshot() []modelMapping {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]modelMapping(nil), s.mappings...)
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
			return mapping, true
		}
	}

	// Preserve CPA's optional thinking suffix, for example Port(high).
	if suffixStart := strings.IndexByte(requested, '('); suffixStart > 0 && strings.HasSuffix(requested, ")") {
		base := strings.TrimSpace(requested[:suffixStart])
		suffix := requested[suffixStart:]
		for _, mapping := range s.snapshot() {
			if strings.EqualFold(mapping.Name, base) {
				mapping.Target = strings.TrimSpace(mapping.Target) + suffix
				return mapping, true
			}
		}
	}
	return modelMapping{}, false
}

func parseMappings(raw []byte) ([]modelMapping, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return defaultMappings(), nil
	}

	var cfg mappingConfig
	if errUnmarshal := yaml.Unmarshal(raw, &cfg); errUnmarshal != nil {
		return nil, fmt.Errorf("decode unified-model config: %w", errUnmarshal)
	}
	var fields map[string]any
	if errUnmarshal := yaml.Unmarshal(raw, &fields); errUnmarshal != nil {
		return nil, fmt.Errorf("inspect unified-model config: %w", errUnmarshal)
	}
	if _, present := fields["mappings"]; !present {
		return defaultMappings(), nil
	}
	return normalizeMappings(cfg.Mappings), nil
}

func normalizeMappings(input []modelMapping) []modelMapping {
	output := make([]modelMapping, 0, len(input))
	indexes := make(map[string]int, len(input))
	for _, item := range input {
		item.Name = strings.TrimSpace(item.Name)
		item.Target = strings.TrimSpace(item.Target)
		item.Provider = strings.ToLower(strings.TrimSpace(item.Provider))
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

func modelInfos(mappings []modelMapping) []pluginapi.ModelInfo {
	models := make([]pluginapi.ModelInfo, 0, len(mappings))
	for _, mapping := range mappings {
		if strings.TrimSpace(mapping.Name) == "" {
			continue
		}
		models = append(models, pluginapi.ModelInfo{
			ID:                         mapping.Name,
			Object:                     "model",
			OwnedBy:                    providerID,
			DisplayName:                mapping.Name,
			Name:                       mapping.Name,
			Description:                "Unified model alias routed by the Unified Model plugin.",
			SupportedGenerationMethods: []string{"chat"},
			ContextLength:              128000,
			MaxCompletionTokens:        32768,
			SupportedInputModalities:   []string{"text"},
			UserDefined:                true,
		})
	}
	return models
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
