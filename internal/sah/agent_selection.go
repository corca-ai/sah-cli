package sah

import (
	"fmt"
	"sort"
	"strings"
)

type AgentPicker struct {
	pool  []AgentSpec
	index int
}

func NewAgentPicker(config Config, options WorkerOptions) (*AgentPicker, error) {
	pool, err := ResolveAgentPool(config, options)
	if err != nil {
		return nil, err
	}
	return &AgentPicker{pool: pool}, nil
}

func (picker *AgentPicker) Pool() []AgentSpec {
	cloned := make([]AgentSpec, len(picker.pool))
	copy(cloned, picker.pool)
	return cloned
}

func (picker *AgentPicker) Next() AgentSpec {
	agent := picker.pool[picker.index%len(picker.pool)]
	picker.index++
	return agent
}

func ResolveAgentPool(config Config, options WorkerOptions) ([]AgentSpec, error) {
	switch {
	case options.RotateInstalled:
		return resolveInstalledAgentPool()
	case len(options.Agents) > 0:
		return resolveNamedAgentPool(options.Agents)
	case config.RotateInstalled:
		return resolveInstalledAgentPool()
	case len(config.AgentPool) > 0:
		return resolveNamedAgentPool(config.AgentPool)
	default:
		name := strings.TrimSpace(options.Agent)
		if name == "" {
			name = config.DefaultAgent
		}
		agent, err := ResolveAgent(name)
		if err != nil {
			return nil, err
		}
		return []AgentSpec{agent}, nil
	}
}

func resolveInstalledAgentPool() ([]AgentSpec, error) {
	pool := make([]AgentSpec, 0, len(SupportedAgents))
	for _, status := range InstalledAgents() {
		if status.Installed {
			pool = append(pool, status.AgentSpec)
		}
	}
	if len(pool) == 0 {
		return nil, fmt.Errorf("no supported agent CLI found in PATH")
	}
	return pool, nil
}

func resolveNamedAgentPool(names []string) ([]AgentSpec, error) {
	pool := make([]AgentSpec, 0, len(names))
	seen := map[string]struct{}{}
	for _, entry := range names {
		name := normalizeAgentName(entry)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		agent, err := ResolveAgent(name)
		if err != nil {
			return nil, err
		}
		seen[name] = struct{}{}
		pool = append(pool, agent)
	}
	if len(pool) == 0 {
		return nil, fmt.Errorf("agent pool is empty")
	}
	return pool, nil
}

func ParseAgentList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	return normalizeAgentPool(strings.Split(raw, ","))
}

func ParseAgentModels(raw string) (map[string]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}

	models := map[string]string{}
	for _, entry := range strings.Split(raw, ",") {
		pair := strings.SplitN(strings.TrimSpace(entry), "=", 2)
		if len(pair) != 2 {
			return nil, fmt.Errorf("invalid model override %q; expected agent=model", entry)
		}

		name := normalizeAgentName(pair[0])
		model := strings.TrimSpace(pair[1])
		if name == "" || model == "" {
			return nil, fmt.Errorf("invalid model override %q; expected agent=model", entry)
		}
		models[name] = model
	}
	return normalizeAgentModels(models), nil
}

func MergeAgentModels(base map[string]string, overrides map[string]string) map[string]string {
	if len(base) == 0 && len(overrides) == 0 {
		return nil
	}

	merged := map[string]string{}
	for name, model := range normalizeAgentModels(base) {
		merged[name] = model
	}
	for name, model := range normalizeAgentModels(overrides) {
		merged[name] = model
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

func ModelForAgent(agentName string, fallback string, overrides map[string]string) string {
	if model, ok := normalizeAgentModels(overrides)[normalizeAgentName(agentName)]; ok {
		return model
	}
	return strings.TrimSpace(fallback)
}

func DescribeAgentMode(config Config, options WorkerOptions) string {
	if options.RotateInstalled {
		return "all installed agents"
	}
	if len(options.Agents) > 0 {
		return strings.Join(options.Agents, ", ")
	}
	if config.RotateInstalled {
		return "all installed agents"
	}
	if len(config.AgentPool) > 0 {
		return strings.Join(config.AgentPool, ", ")
	}
	if strings.TrimSpace(options.Agent) != "" {
		return normalizeAgentName(options.Agent)
	}
	return config.DefaultAgent
}

func FormatAgentModels(models map[string]string) string {
	normalized := normalizeAgentModels(models)
	if len(normalized) == 0 {
		return ""
	}

	names := make([]string, 0, len(normalized))
	for name := range normalized {
		names = append(names, name)
	}
	sort.Strings(names)

	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s=%s", name, normalized[name]))
	}
	return strings.Join(parts, ", ")
}
