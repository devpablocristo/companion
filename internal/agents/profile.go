package agents

import "strings"

type MemoryPolicy struct {
	AllowedTypes []string `json:"allowed_types"`
	MaxItems     int      `json:"max_items"`
}

type Profile struct {
	ID                  string       `json:"id"`
	ProductSurface      string       `json:"product_surface"`
	MaxAutonomy         string       `json:"max_autonomy"`
	AllowedTools        []string     `json:"allowed_tools"`
	AllowedCapabilities []string     `json:"allowed_capabilities,omitempty"`
	MemoryPolicy        MemoryPolicy `json:"memory_policy"`
	RequiredScopes      []string     `json:"required_scopes,omitempty"`
	Enabled             bool         `json:"enabled"`
	Version             string       `json:"version"`
}

type Registry struct {
	profiles []Profile
}

func DefaultRegistry() Registry {
	return Registry{profiles: []Profile{
		{
			ID:             "companion.default",
			ProductSurface: "companion",
			MaxAutonomy:    "A2",
			AllowedTools:   []string{"get_overview", "remember", "recall"},
			MemoryPolicy:   MemoryPolicy{AllowedTypes: []string{"preference", "playbook", "task_projection"}, MaxItems: 10},
			Enabled:        true,
			Version:        "v1",
		},
		{
			ID:             "companion.governance",
			ProductSurface: "companion",
			MaxAutonomy:    "A2",
			AllowedTools:   []string{"get_overview", "check_approvals", "list_policies"},
			RequiredScopes: []string{"companion:governance:admin"},
			MemoryPolicy:   MemoryPolicy{AllowedTypes: []string{"task_projection"}, MaxItems: 5},
			Enabled:        true,
			Version:        "v1",
		},
		{
			ID:             "companion.operations",
			ProductSurface: "companion",
			MaxAutonomy:    "A2",
			AllowedTools:   []string{"get_overview", "list_watchers"},
			RequiredScopes: []string{"companion:watchers:read"},
			MemoryPolicy:   MemoryPolicy{AllowedTypes: []string{"operational", "task_projection"}, MaxItems: 10},
			Enabled:        true,
			Version:        "v1",
		},
		{
			ID:             "companion.memory",
			ProductSurface: "companion",
			MaxAutonomy:    "A1",
			AllowedTools:   []string{"remember", "recall"},
			MemoryPolicy:   MemoryPolicy{AllowedTypes: []string{"preference", "playbook"}, MaxItems: 10},
			Enabled:        true,
			Version:        "v1",
		},
	}}
}

func (r Registry) Resolve(productSurface, intent, requestedAutonomy string, scopes []string, availableTools []string) Profile {
	selected := r.profiles[0]
	switch {
	case strings.HasPrefix(intent, "governance."):
		selected = r.find("companion.governance", selected)
	case strings.HasPrefix(intent, "operations."):
		selected = r.find("companion.operations", selected)
	case intent == "memory":
		selected = r.find("companion.memory", selected)
	}
	if strings.TrimSpace(productSurface) != "" {
		selected.ProductSurface = strings.TrimSpace(productSurface)
	}
	if requestedAutonomy != "" && autonomyRank(requestedAutonomy) < autonomyRank(selected.MaxAutonomy) {
		selected.MaxAutonomy = requestedAutonomy
	}
	selected.AllowedTools = intersect(selected.AllowedTools, availableTools)
	if len(selected.RequiredScopes) > 0 && !hasAnyScope(scopes, selected.RequiredScopes) {
		selected.AllowedTools = nil
	}
	return selected
}

func (r Registry) find(id string, fallback Profile) Profile {
	for _, profile := range r.profiles {
		if profile.Enabled && profile.ID == id {
			return profile
		}
	}
	return fallback
}

func intersect(allowed, available []string) []string {
	set := make(map[string]struct{}, len(available))
	for _, name := range available {
		if name = strings.TrimSpace(name); name != "" {
			set[name] = struct{}{}
		}
	}
	out := make([]string, 0, len(allowed))
	for _, name := range allowed {
		if _, ok := set[strings.TrimSpace(name)]; ok {
			out = append(out, strings.TrimSpace(name))
		}
	}
	return out
}

func hasAnyScope(have, required []string) bool {
	set := make(map[string]struct{}, len(have))
	for _, scope := range have {
		if scope = strings.TrimSpace(scope); scope != "" {
			set[scope] = struct{}{}
		}
	}
	for _, scope := range required {
		if _, ok := set[strings.TrimSpace(scope)]; ok {
			return true
		}
	}
	return false
}

func autonomyRank(level string) int {
	switch strings.TrimSpace(level) {
	case "A0":
		return 0
	case "A1":
		return 1
	case "A2":
		return 2
	case "A3":
		return 3
	case "A4":
		return 4
	case "A5":
		return 5
	default:
		return 2
	}
}
