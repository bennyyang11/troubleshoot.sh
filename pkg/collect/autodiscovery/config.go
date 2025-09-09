package autodiscovery

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"gopkg.in/yaml.v2"
)

// Config represents the auto-discovery configuration
type Config struct {
	// DefaultOptions are the default discovery options
	DefaultOptions DiscoveryOptions `json:"defaultOptions" yaml:"defaultOptions"`
	
	// ResourceFilters define custom resource filtering rules
	ResourceFilters []ResourceFilterRule `json:"resourceFilters,omitempty" yaml:"resourceFilters,omitempty"`
	
	// CollectorMappings define custom resource-to-collector mappings
	CollectorMappings []CollectorMappingRule `json:"collectorMappings,omitempty" yaml:"collectorMappings,omitempty"`
	
	// Excludes define resources to always exclude from auto-discovery
	Excludes []ResourceExcludeRule `json:"excludes,omitempty" yaml:"excludes,omitempty"`
	
	// Includes define additional resources to always include
	Includes []ResourceIncludeRule `json:"includes,omitempty" yaml:"includes,omitempty"`
}

// ResourceFilterRule defines filtering criteria for resources
type ResourceFilterRule struct {
	Name              string                        `json:"name" yaml:"name"`
	MatchGVRs         []schema.GroupVersionResource `json:"matchGVRs" yaml:"matchGVRs"`
	MatchNamespaces   []string                      `json:"matchNamespaces" yaml:"matchNamespaces"`
	MatchLabels       map[string]string             `json:"matchLabels" yaml:"matchLabels"`
	LabelSelector     string                        `json:"labelSelector" yaml:"labelSelector"`
	Action            string                        `json:"action" yaml:"action"` // "include" or "exclude"
}

// CollectorMappingRule defines custom collector generation rules
type CollectorMappingRule struct {
	Name           string                        `json:"name" yaml:"name"`
	MatchGVRs      []schema.GroupVersionResource `json:"matchGVRs" yaml:"matchGVRs"`
	CollectorType  string                        `json:"collectorType" yaml:"collectorType"`
	Priority       int                           `json:"priority" yaml:"priority"`
	Parameters     map[string]interface{}        `json:"parameters" yaml:"parameters"`
	Condition      string                        `json:"condition,omitempty" yaml:"condition,omitempty"`
}

// ResourceExcludeRule defines resources to exclude from discovery
type ResourceExcludeRule struct {
	GVRs       []schema.GroupVersionResource `json:"gvrs" yaml:"gvrs"`
	Namespaces []string                      `json:"namespaces" yaml:"namespaces"`
	Names      []string                      `json:"names" yaml:"names"`
	Reason     string                        `json:"reason" yaml:"reason"`
}

// ResourceIncludeRule defines resources to always include in discovery
type ResourceIncludeRule struct {
	GVRs       []schema.GroupVersionResource `json:"gvrs" yaml:"gvrs"`
	Namespaces []string                      `json:"namespaces" yaml:"namespaces"`
	Names      []string                      `json:"names" yaml:"names"`
	Priority   int                           `json:"priority" yaml:"priority"`
}

// ConfigManager handles auto-discovery configuration loading and management
type ConfigManager struct {
	config *Config
}

// NewConfigManager creates a new ConfigManager with default configuration
func NewConfigManager() *ConfigManager {
	return &ConfigManager{
		config: getDefaultConfig(),
	}
}

// LoadFromFile loads configuration from a file (supports JSON and YAML)
func (c *ConfigManager) LoadFromFile(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	ext := filepath.Ext(filePath)
	switch ext {
	case ".json":
		return c.LoadFromJSON(data)
	case ".yaml", ".yml":
		return c.LoadFromYAML(data)
	default:
		return fmt.Errorf("unsupported config file format: %s", ext)
	}
}

// LoadFromJSON loads configuration from JSON data
func (c *ConfigManager) LoadFromJSON(data []byte) error {
	config := &Config{}
	if err := json.Unmarshal(data, config); err != nil {
		return fmt.Errorf("failed to parse JSON config: %w", err)
	}

	// Merge with defaults
	c.config = mergeWithDefaults(config)
	return nil
}

// LoadFromYAML loads configuration from YAML data
func (c *ConfigManager) LoadFromYAML(data []byte) error {
	config := &Config{}
	if err := yaml.Unmarshal(data, config); err != nil {
		return fmt.Errorf("failed to parse YAML config: %w", err)
	}

	// Merge with defaults
	c.config = mergeWithDefaults(config)
	return nil
}

// GetConfig returns the current configuration
func (c *ConfigManager) GetConfig() *Config {
	return c.config
}

// GetDiscoveryOptions returns the discovery options, merging defaults with any overrides
func (c *ConfigManager) GetDiscoveryOptions(overrides *DiscoveryOptions) DiscoveryOptions {
	options := c.config.DefaultOptions

	if overrides != nil {
		if len(overrides.Namespaces) > 0 {
			options.Namespaces = overrides.Namespaces
		}
		if overrides.IncludeImages {
			options.IncludeImages = overrides.IncludeImages
		}
		if overrides.RBACCheck {
			options.RBACCheck = overrides.RBACCheck
		}
		if overrides.MaxDepth > 0 {
			options.MaxDepth = overrides.MaxDepth
		}
	}

	return options
}

// ApplyResourceFilters applies configured resource filters to a list of resources
func (c *ConfigManager) ApplyResourceFilters(resources []Resource) []Resource {
	filteredResources := resources

	// Apply exclusion rules first
	for _, exclude := range c.config.Excludes {
		filteredResources = c.applyExcludeRule(filteredResources, exclude)
	}

	// Apply filter rules
	for _, filter := range c.config.ResourceFilters {
		if filter.Action == "exclude" {
			filteredResources = c.applyFilterRule(filteredResources, filter, true)
		}
	}

	for _, filter := range c.config.ResourceFilters {
		if filter.Action == "include" {
			filteredResources = c.applyFilterRule(filteredResources, filter, false)
		}
	}

	return filteredResources
}

// GetCollectorMappings returns custom collector mappings that override defaults
func (c *ConfigManager) GetCollectorMappings() map[string]CollectorMapping {
	mappings := make(map[string]CollectorMapping)

	for _, rule := range c.config.CollectorMappings {
		for _, gvr := range rule.MatchGVRs {
			key := fmt.Sprintf("%s_%s_%s", gvr.Group, gvr.Version, gvr.Resource)
			// Capture rule parameters in closure
			ruleParams := make(map[string]interface{})
			for k, v := range rule.Parameters {
				ruleParams[k] = v
			}
			
			mappings[key] = CollectorMapping{
				CollectorType: rule.CollectorType,
				Priority:      rule.Priority,
				ParameterBuilder: func(resource Resource) map[string]interface{} {
					// Start with rule parameters and add resource-specific ones
					params := make(map[string]interface{})
					for k, v := range ruleParams {
						params[k] = v
					}
					// Add standard resource parameters
					params["name"] = resource.Name
					params["namespace"] = resource.Namespace
					return params
				},
			}
		}
	}

	return mappings
}

// applyExcludeRule applies an exclusion rule to filter out resources
func (c *ConfigManager) applyExcludeRule(resources []Resource, rule ResourceExcludeRule) []Resource {
	var filtered []Resource

	for _, resource := range resources {
		excluded := false

		// Check GVR match
		for _, gvr := range rule.GVRs {
			if resource.GVR == gvr {
				excluded = true
				break
			}
		}

		// Check namespace match
		if !excluded && len(rule.Namespaces) > 0 {
			for _, ns := range rule.Namespaces {
				if resource.Namespace == ns {
					excluded = true
					break
				}
			}
		}

		// Check name match
		if !excluded && len(rule.Names) > 0 {
			for _, name := range rule.Names {
				if resource.Name == name {
					excluded = true
					break
				}
			}
		}

		if !excluded {
			filtered = append(filtered, resource)
		}
	}

	return filtered
}

// applyFilterRule applies a filter rule to include or exclude resources
func (c *ConfigManager) applyFilterRule(resources []Resource, rule ResourceFilterRule, exclude bool) []Resource {
	var filtered []Resource

	for _, resource := range resources {
		matches := c.resourceMatchesFilter(resource, rule)
		
		if exclude && matches {
			// Skip this resource (exclude it)
			continue
		} else if !exclude && !matches {
			// Skip this resource (only include matching)
			continue
		}
		
		filtered = append(filtered, resource)
	}

	return filtered
}

// resourceMatchesFilter checks if a resource matches the filter criteria
func (c *ConfigManager) resourceMatchesFilter(resource Resource, rule ResourceFilterRule) bool {
	// Check GVR match
	if len(rule.MatchGVRs) > 0 {
		found := false
		for _, gvr := range rule.MatchGVRs {
			if resource.GVR == gvr {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check namespace match
	if len(rule.MatchNamespaces) > 0 {
		found := false
		for _, ns := range rule.MatchNamespaces {
			if resource.Namespace == ns {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check label match
	if len(rule.MatchLabels) > 0 {
		for key, value := range rule.MatchLabels {
			if resourceValue, exists := resource.Labels[key]; !exists || resourceValue != value {
				return false
			}
		}
	}

	// TODO: Implement label selector matching using k8s.io/apimachinery/pkg/labels

	return true
}

// getDefaultConfig returns the default auto-discovery configuration
func getDefaultConfig() *Config {
	return &Config{
		DefaultOptions: DiscoveryOptions{
			Namespaces:    []string{}, // Empty means all accessible namespaces
			IncludeImages: true,
			RBACCheck:     true,
			MaxDepth:      3,
		},
		ResourceFilters:   []ResourceFilterRule{},
		CollectorMappings: []CollectorMappingRule{},
		Excludes: []ResourceExcludeRule{
			{
				// Exclude system namespaces by default
				Namespaces: []string{"kube-system", "kube-public", "kube-node-lease"},
				Reason:     "System namespaces excluded by default",
			},
		},
		Includes: []ResourceIncludeRule{},
	}
}

// mergeWithDefaults merges user configuration with defaults
func mergeWithDefaults(userConfig *Config) *Config {
	defaultConfig := getDefaultConfig()

	// Keep user's explicit values, use defaults only for zero values
	if len(userConfig.DefaultOptions.Namespaces) == 0 {
		userConfig.DefaultOptions.Namespaces = defaultConfig.DefaultOptions.Namespaces
	}
	
	// For boolean fields, we need to check if they were explicitly set
	// Since Go doesn't distinguish between false and zero value, we assume user intent
	// Only change maxDepth if it's 0 (user didn't set it)
	if userConfig.DefaultOptions.MaxDepth == 0 {
		userConfig.DefaultOptions.MaxDepth = defaultConfig.DefaultOptions.MaxDepth
	}

	// Initialize empty slices if nil
	if userConfig.ResourceFilters == nil {
		userConfig.ResourceFilters = []ResourceFilterRule{}
	}
	if userConfig.CollectorMappings == nil {
		userConfig.CollectorMappings = []CollectorMappingRule{}
	}
	if userConfig.Includes == nil {
		userConfig.Includes = []ResourceIncludeRule{}
	}

	// Merge excludes (user excludes are added to defaults)
	userConfig.Excludes = append(defaultConfig.Excludes, userConfig.Excludes...)

	return userConfig
}

// SaveToFile saves the current configuration to a file
func (c *ConfigManager) SaveToFile(filePath string) error {
	ext := filepath.Ext(filePath)
	switch ext {
	case ".json":
		return c.SaveToJSON(filePath)
	case ".yaml", ".yml":
		return c.SaveToYAML(filePath)
	default:
		return fmt.Errorf("unsupported config file format: %s", ext)
	}
}

// SaveToJSON saves configuration to JSON file
func (c *ConfigManager) SaveToJSON(filePath string) error {
	data, err := json.MarshalIndent(c.config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config to JSON: %w", err)
	}

	return os.WriteFile(filePath, data, 0644)
}

// SaveToYAML saves configuration to YAML file  
func (c *ConfigManager) SaveToYAML(filePath string) error {
	data, err := yaml.Marshal(c.config)
	if err != nil {
		return fmt.Errorf("failed to marshal config to YAML: %w", err)
	}

	return os.WriteFile(filePath, data, 0644)
}
