package main

import (
	"sort"
	"strings"
)

// contextLengthRule maps a known model name (or name family) to the context window
// advertised for aliases that do not configure an explicit context length.
type contextLengthRule struct {
	// pattern is a readable model name prefix, for example "gpt-5.6" or "claude-sonnet-4".
	pattern string
	// contextLength is the advertised token budget for matching models.
	contextLength int
}

// builtInContextLengths is the plugin's built-in model name table. Values follow the
// CLIProxyAPI model registry so aliases agree with the built-in model catalogs.
// Matching ignores case, separators ("-", ".", "_"), provider prefixes and thinking
// suffixes, so "GPT-5.6-Sol", "gpt-5.6/sol" and "gpt_5_6_sol" all hit the same rule.
var builtInContextLengths = []contextLengthRule{
	{"codex-auto-review", 272000},
	{"gpt-4.1", 1047576},
	{"gpt-4o", 128000},
	{"gpt-5.3-codex", 400000},
	{"gpt-5.4-mini", 400000},
	{"gpt-5.4", 272000},
	{"gpt-5.5", 272000},
	{"gpt-5.6", 921000},
	{"gpt-6", 272000},
	{"gpt-oss-120b", 114000},
	{"o3", 200000},
	{"o4-mini", 200000},
	{"claude-3-5-haiku", 128000},
	{"claude-3-5-sonnet", 200000},
	{"claude-3-7-sonnet", 128000},
	{"claude-haiku-4", 200000},
	{"claude-opus-4-6", 200000},
	{"claude-opus-4-7", 1000000},
	{"claude-opus-4-8", 1000000},
	{"claude-opus-4", 200000},
	{"claude-opus-5", 1000000},
	{"claude-sonnet-4", 200000},
	{"claude-sonnet-5", 1000000},
	{"claude-fable-5", 1000000},
	{"claude-5-fable", 1000000},
	{"gemini", 1048576},
	{"grok-3", 131072},
	{"grok-4.3", 1000000},
	{"grok-4.5", 500000},
	{"grok-4.6", 500000},
	{"grok-4.20", 2000000},
	{"grok-build", 256000},
	{"grok-composer", 200000},
	{"kimi-k2.5", 262144},
	{"kimi-k2.6", 262144},
	{"kimi-k2.7", 262144},
	{"kimi-k2.8", 1048576},
	{"kimi-k2", 131072},
	{"kimi-k3", 1048576},
	{"deepseek-v4", 1048576},
	{"glm-5-2", 200000},
	{"glm-5-3-flash", 1000000},
	{"glm-5-3", 1048576},
	{"muse-spark", 1048576},
	{"nemotron-3-ultra", 1000000},
	{"swe-1.6", 200000},
	{"swe-1.7", 262000},
	{"swe-2", 262000},
}

// catalogRule is one table entry exposed to management clients.
type catalogRule struct {
	Pattern       string `json:"pattern"`
	ContextLength int    `json:"contextLength"`
	// Source reports whether the value comes from the built-in table or a user override.
	Source string `json:"source"`
}

// modelCatalogResponse carries the effective context window table the management page
// uses to prefill a manually entered context length.
type modelCatalogResponse struct {
	DefaultContextLength int           `json:"defaultContextLength"`
	MaxContextLength     int           `json:"maxContextLength"`
	Rules                []catalogRule `json:"rules"`
}

// contextLengthSourceBuiltIn marks a table entry that ships with the plugin.
const contextLengthSourceBuiltIn = "built-in"

// contextLengthSourceCustom marks a table entry overridden or added from the plugin config.
const contextLengthSourceCustom = "custom"

// modelCatalog returns the effective table (built-in entries with user overrides applied).
func modelCatalog(overrides contextLengths) modelCatalogResponse {
	custom := overrides.compactKeys()
	rules := make([]catalogRule, 0, len(builtInContextLengths)+len(overrides))
	for _, rule := range overrides.mergedRules() {
		source := contextLengthSourceBuiltIn
		if custom[compactModelName(rule.pattern)] {
			source = contextLengthSourceCustom
		}
		rules = append(rules, catalogRule{Pattern: rule.pattern, ContextLength: rule.contextLength, Source: source})
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].Pattern < rules[j].Pattern })
	return modelCatalogResponse{
		DefaultContextLength: defaultContextLength,
		MaxContextLength:     maxContextLength,
		Rules:                rules,
	}
}

// contextLengths holds user overrides for the built-in model name table, keyed by a
// readable model name or name family, for example {"gpt-5.5": 300000}.
type contextLengths map[string]int

// normalizeContextLengths drops blank keys and non-positive values and lower-cases the
// remaining model names so lookups are stable.
func normalizeContextLengths(input map[string]int) contextLengths {
	if len(input) == 0 {
		return nil
	}
	output := make(contextLengths, len(input))
	for rawPattern, value := range input {
		pattern := strings.ToLower(strings.TrimSpace(rawPattern))
		length := normalizeContextLength(value)
		if pattern == "" || length <= 0 {
			continue
		}
		output[pattern] = length
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

// compactKeys reports whether a model name is present in the override table.
func (c contextLengths) compactKeys() map[string]bool {
	keys := make(map[string]bool, len(c))
	for pattern := range c {
		keys[compactModelName(pattern)] = true
	}
	return keys
}

// mergedRules returns the built-in table with user overrides applied: an override whose
// name matches an existing entry replaces its value (family entries keep their scope),
// and unknown names are added as new entries.
func (c contextLengths) mergedRules() []contextLengthRule {
	rules := append([]contextLengthRule(nil), builtInContextLengths...)
	for pattern, value := range c {
		target := compactModelName(pattern)
		replaced := false
		for index := range rules {
			if compactModelName(rules[index].pattern) != target {
				continue
			}
			rules[index].contextLength = value
			replaced = true
			break
		}
		if !replaced {
			rules = append(rules, contextLengthRule{pattern: pattern, contextLength: value})
		}
	}
	return rules
}

// lookup returns the table context window for a model name, or 0 when the name is unknown.
func (c contextLengths) lookup(model string) int {
	return lookupContextLength(c.mergedRules(), model)
}

// lookupContextLength matches a model name against the table rules. The longest matching
// pattern wins, so a specific model overrides its family entry.
func lookupContextLength(rules []contextLengthRule, model string) int {
	name := compactModelName(model)
	if name == "" {
		return 0
	}
	bestLength := 0
	bestPattern := ""
	for _, rule := range rules {
		pattern := compactModelName(rule.pattern)
		if pattern == "" || !strings.HasPrefix(name, pattern) {
			continue
		}
		if len(pattern) > len(bestPattern) || (len(pattern) == len(bestPattern) && rule.contextLength > bestLength) {
			bestPattern, bestLength = pattern, rule.contextLength
		}
	}
	return bestLength
}

// knownContextLength returns the unmodified built-in context window for a model name.
func knownContextLength(model string) int {
	return lookupContextLength(builtInContextLengths, model)
}

// normalizeModelName lower-cases a model name and drops provider prefixes plus any
// thinking suffix such as "Port(high)".
func normalizeModelName(model string) string {
	name := strings.ToLower(strings.TrimSpace(model))
	if name == "" {
		return ""
	}
	if start := strings.IndexByte(name, '('); start > 0 {
		name = strings.TrimSpace(name[:start])
	}
	if slash := strings.LastIndexByte(name, '/'); slash >= 0 {
		name = strings.TrimSpace(name[slash+1:])
	}
	return name
}

// compactModelName removes the separators that only differ in spelling style, so that
// "gpt-5.6-sol", "gpt_5_6_sol" and "gpt56sol" all compare equal.
func compactModelName(name string) string {
	normalized := normalizeModelName(name)
	if normalized == "" {
		return ""
	}
	var builder strings.Builder
	builder.Grow(len(normalized))
	for _, r := range normalized {
		switch r {
		case '-', '.', '_', ' ':
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}
