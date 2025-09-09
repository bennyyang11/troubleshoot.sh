package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/replicatedhq/troubleshoot/pkg/collect/autodiscovery"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"gopkg.in/yaml.v2"
)

// SupportBundleSpec represents the extended support bundle specification with auto-discovery
type SupportBundleSpec struct {
	APIVersion string                    `json:"apiVersion" yaml:"apiVersion"`
	Kind       string                    `json:"kind" yaml:"kind"`
	Metadata   SupportBundleMetadata     `json:"metadata" yaml:"metadata"`
	Spec       SupportBundleSpecDetails  `json:"spec" yaml:"spec"`
}

// SupportBundleMetadata contains metadata for the support bundle
type SupportBundleMetadata struct {
	Name        string            `json:"name" yaml:"name"`
	Namespace   string            `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Labels      map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty" yaml:"annotations,omitempty"`
}

// SupportBundleSpecDetails contains the specification details
type SupportBundleSpecDetails struct {
	// Traditional collectors array (backwards compatible)
	Collectors []map[string]interface{} `json:"collectors,omitempty" yaml:"collectors,omitempty"`
	
	// Auto-discovery configuration (new)
	AutoDiscovery *AutoDiscoveryConfig `json:"autoDiscovery,omitempty" yaml:"autoDiscovery,omitempty"`
	
	// Analyzers (existing)
	Analyzers []map[string]interface{} `json:"analyzers,omitempty" yaml:"analyzers,omitempty"`
	
	// Redaction configuration (new)
	Redaction *RedactionConfig `json:"redaction,omitempty" yaml:"redaction,omitempty"`
}

// AutoDiscoveryConfig configures auto-discovery behavior in support bundle specs
type AutoDiscoveryConfig struct {
	Enabled       bool                               `json:"enabled" yaml:"enabled"`
	Namespaces    []string                          `json:"namespaces,omitempty" yaml:"namespaces,omitempty"`
	IncludeImages bool                              `json:"includeImages" yaml:"includeImages"`
	RBACCheck     bool                              `json:"rbacCheck" yaml:"rbacCheck"`
	MaxDepth      int                               `json:"maxDepth,omitempty" yaml:"maxDepth,omitempty"`
	Profile       string                            `json:"profile,omitempty" yaml:"profile,omitempty"`
	
	// Resource filtering
	ResourceFilters []autodiscovery.ResourceFilterRule `json:"resourceFilters,omitempty" yaml:"resourceFilters,omitempty"`
	
	// Custom collector mappings
	CollectorMappings []autodiscovery.CollectorMappingRule `json:"collectorMappings,omitempty" yaml:"collectorMappings,omitempty"`
	
	// Exclusions and inclusions
	Excludes []autodiscovery.ResourceExcludeRule `json:"excludes,omitempty" yaml:"excludes,omitempty"`
	Includes []autodiscovery.ResourceIncludeRule `json:"includes,omitempty" yaml:"includes,omitempty"`
	
	// Image collection configuration
	ImageOptions *ImageCollectionConfig `json:"imageOptions,omitempty" yaml:"imageOptions,omitempty"`
}

// ImageCollectionConfig configures image metadata collection
type ImageCollectionConfig struct {
	IncludeManifests bool                                     `json:"includeManifests" yaml:"includeManifests"`
	IncludeLayers    bool                                     `json:"includeLayers" yaml:"includeLayers"`
	IncludeConfig    bool                                     `json:"includeConfig" yaml:"includeConfig"`
	CacheEnabled     bool                                     `json:"cacheEnabled" yaml:"cacheEnabled"`
	Timeout          string                                   `json:"timeout" yaml:"timeout"`
	MaxConcurrency   int                                      `json:"maxConcurrency" yaml:"maxConcurrency"`
	RetryCount       int                                      `json:"retryCount" yaml:"retryCount"`
	RegistryAuth     map[string]*RegistryAuthConfig           `json:"registryAuth,omitempty" yaml:"registryAuth,omitempty"`
}

// RegistryAuthConfig configures registry authentication
type RegistryAuthConfig struct {
	Username string `json:"username,omitempty" yaml:"username,omitempty"`
	Password string `json:"password,omitempty" yaml:"password,omitempty"`
	Token    string `json:"token,omitempty" yaml:"token,omitempty"`
	SecretRef string `json:"secretRef,omitempty" yaml:"secretRef,omitempty"`
}

// RedactionConfig configures redaction behavior (placeholder for future implementation)
type RedactionConfig struct {
	Enabled bool   `json:"enabled" yaml:"enabled"`
	Profile string `json:"profile,omitempty" yaml:"profile,omitempty"`
}

// SupportBundleSpecLoader loads and validates support bundle specifications
type SupportBundleSpecLoader struct {
	configManager *autodiscovery.ConfigManager
}

// NewSupportBundleSpecLoader creates a new spec loader
func NewSupportBundleSpecLoader() *SupportBundleSpecLoader {
	return &SupportBundleSpecLoader{
		configManager: autodiscovery.NewConfigManager(),
	}
}

// LoadFromFile loads a support bundle spec from a file
func (sbsl *SupportBundleSpecLoader) LoadFromFile(filePath string) (*SupportBundleSpec, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read spec file: %w", err)
	}

	// Try YAML first (more common for Kubernetes)
	spec := &SupportBundleSpec{}
	if err := yaml.Unmarshal(data, spec); err != nil {
		// Try JSON fallback
		if jsonErr := json.Unmarshal(data, spec); jsonErr != nil {
			return nil, fmt.Errorf("failed to parse spec as YAML or JSON: yaml=%v, json=%v", err, jsonErr)
		}
	}

	// Validate the spec
	if err := sbsl.ValidateSpec(spec); err != nil {
		return nil, fmt.Errorf("invalid spec: %w", err)
	}

	return spec, nil
}

// ValidateSpec validates a support bundle specification
func (sbsl *SupportBundleSpecLoader) ValidateSpec(spec *SupportBundleSpec) error {
	// Validate required fields
	if spec.APIVersion == "" {
		return fmt.Errorf("apiVersion is required")
	}
	if spec.Kind == "" {
		return fmt.Errorf("kind is required")
	}
	if spec.Metadata.Name == "" {
		return fmt.Errorf("metadata.name is required")
	}

	// Validate API version compatibility
	supportedVersions := []string{"troubleshoot.sh/v1beta2", "troubleshoot.sh/v1beta3"}
	isSupported := false
	for _, supported := range supportedVersions {
		if spec.APIVersion == supported {
			isSupported = true
			break
		}
	}
	if !isSupported {
		return fmt.Errorf("unsupported apiVersion: %s (supported: %v)", spec.APIVersion, supportedVersions)
	}

	// Validate kind
	supportedKinds := []string{"SupportBundle", "Preflight"}
	isValidKind := false
	for _, supported := range supportedKinds {
		if spec.Kind == supported {
			isValidKind = true
			break
		}
	}
	if !isValidKind {
		return fmt.Errorf("unsupported kind: %s (supported: %v)", spec.Kind, supportedKinds)
	}

	// Validate auto-discovery configuration if present
	if spec.Spec.AutoDiscovery != nil {
		if err := sbsl.validateAutoDiscoveryConfig(spec.Spec.AutoDiscovery); err != nil {
			return fmt.Errorf("invalid autoDiscovery config: %w", err)
		}
	}

	return nil
}

func (sbsl *SupportBundleSpecLoader) validateAutoDiscoveryConfig(config *AutoDiscoveryConfig) error {
	// Validate namespace list
	for _, ns := range config.Namespaces {
		if ns == "" {
			return fmt.Errorf("namespace cannot be empty")
		}
		if strings.Contains(ns, " ") {
			return fmt.Errorf("namespace cannot contain spaces: %s", ns)
		}
	}

	// Validate max depth
	if config.MaxDepth < 0 || config.MaxDepth > 10 {
		return fmt.Errorf("maxDepth must be between 0 and 10")
	}

	// Validate profile name
	if config.Profile != "" {
		validProfiles := []string{"minimal", "standard", "comprehensive", "custom"}
		isValid := false
		for _, valid := range validProfiles {
			if config.Profile == valid {
				isValid = true
				break
			}
		}
		if !isValid {
			return fmt.Errorf("invalid profile: %s (valid: %v)", config.Profile, validProfiles)
		}
	}

	// Validate image options if present
	if config.ImageOptions != nil {
		if err := sbsl.validateImageConfig(config.ImageOptions); err != nil {
			return fmt.Errorf("invalid imageOptions: %w", err)
		}
	}

	return nil
}

func (sbsl *SupportBundleSpecLoader) validateImageConfig(config *ImageCollectionConfig) error {
	// Validate timeout format
	if config.Timeout != "" {
		if _, err := time.ParseDuration(config.Timeout); err != nil {
			return fmt.Errorf("invalid timeout format: %w", err)
		}
	}

	// Validate concurrency
	if config.MaxConcurrency < 1 || config.MaxConcurrency > 50 {
		return fmt.Errorf("maxConcurrency must be between 1 and 50")
	}

	// Validate retry count
	if config.RetryCount < 0 || config.RetryCount > 10 {
		return fmt.Errorf("retryCount must be between 0 and 10")
	}

	return nil
}

// ExtractAutoDiscoveryOptions extracts auto-discovery options from spec
func (sbsl *SupportBundleSpecLoader) ExtractAutoDiscoveryOptions(spec *SupportBundleSpec) autodiscovery.DiscoveryOptions {
	opts := autodiscovery.DiscoveryOptions{
		Namespaces:    []string{},
		IncludeImages: false,
		RBACCheck:     true, // Default to true for safety
		MaxDepth:      3,    // Default
	}

	if spec.Spec.AutoDiscovery != nil {
		config := spec.Spec.AutoDiscovery
		
		if len(config.Namespaces) > 0 {
			opts.Namespaces = config.Namespaces
		}
		opts.IncludeImages = config.IncludeImages
		opts.RBACCheck = config.RBACCheck
		
		if config.MaxDepth > 0 {
			opts.MaxDepth = config.MaxDepth
		}
	}

	return opts
}

// MergeWithCLIOptions merges spec options with CLI options (CLI takes precedence)
func MergeWithCLIOptions(specOpts autodiscovery.DiscoveryOptions, cliOpts SupportBundleCollectOptions) autodiscovery.DiscoveryOptions {
	merged := specOpts

	// CLI options override spec options
	if len(cliOpts.Namespaces) > 0 {
		merged.Namespaces = cliOpts.Namespaces
	}
	
	// For boolean flags, CLI explicit settings override spec
	if cliOpts.IncludeImages {
		merged.IncludeImages = true
	}
	if cliOpts.RBACCheck {
		merged.RBACCheck = true
	}

	return merged
}

// GenerateExampleSupportBundleSpec creates an example spec with auto-discovery
func GenerateExampleSupportBundleSpec() *SupportBundleSpec {
	return &SupportBundleSpec{
		APIVersion: "troubleshoot.sh/v1beta3",
		Kind:       "SupportBundle",
		Metadata: SupportBundleMetadata{
			Name: "auto-discovery-example",
		},
		Spec: SupportBundleSpecDetails{
			AutoDiscovery: &AutoDiscoveryConfig{
				Enabled:       true,
				Namespaces:    []string{"default", "kube-system"},
				IncludeImages: true,
				RBACCheck:     true,
				MaxDepth:      3,
				Profile:       "standard",
				
				ResourceFilters: []autodiscovery.ResourceFilterRule{
					{
						Name:   "exclude-secrets",
						Action: "exclude",
						MatchGVRs: []schema.GroupVersionResource{
							{Group: "", Version: "v1", Resource: "secrets"},
						},
						MatchNamespaces: []string{"kube-system"},
					},
				},
				
				Excludes: []autodiscovery.ResourceExcludeRule{
					{
						Namespaces: []string{"kube-node-lease"},
						Reason:     "Node lease namespace not needed for troubleshooting",
					},
				},
				
				ImageOptions: &ImageCollectionConfig{
					IncludeManifests: true,
					IncludeLayers:    false, // Reduce size
					IncludeConfig:    true,
					CacheEnabled:     true,
					Timeout:          "60s",
					MaxConcurrency:   3,
					RetryCount:       2,
				},
			},
			
			// Traditional collectors can coexist with auto-discovery
			Collectors: []map[string]interface{}{
				{
					"logs": map[string]interface{}{
						"name":      "application-logs",
						"namespace": "default",
						"selector":  []string{"app=my-application"},
					},
				},
			},
			
			Redaction: &RedactionConfig{
				Enabled: true,
				Profile: "standard",
			},
		},
	}
}

// SaveExampleSpecToFile saves an example support bundle spec with auto-discovery
func SaveExampleSpecToFile(filePath string) error {
	spec := GenerateExampleSupportBundleSpec()
	
	data, err := yaml.Marshal(spec)
	if err != nil {
		return fmt.Errorf("failed to marshal example spec: %w", err)
	}

	return os.WriteFile(filePath, data, 0644)
}

// ConvertSpecToAutoDiscoveryConfig converts spec auto-discovery section to config
func ConvertSpecToAutoDiscoveryConfig(autoDiscoverySpec *AutoDiscoveryConfig) *autodiscovery.Config {
	if autoDiscoverySpec == nil {
		return autodiscovery.NewConfigManager().GetConfig()
	}

	config := &autodiscovery.Config{
		DefaultOptions: autodiscovery.DiscoveryOptions{
			Namespaces:    autoDiscoverySpec.Namespaces,
			IncludeImages: autoDiscoverySpec.IncludeImages,
			RBACCheck:     autoDiscoverySpec.RBACCheck,
			MaxDepth:      autoDiscoverySpec.MaxDepth,
		},
		ResourceFilters:   autoDiscoverySpec.ResourceFilters,
		CollectorMappings: autoDiscoverySpec.CollectorMappings,
		Excludes:          autoDiscoverySpec.Excludes,
		Includes:          autoDiscoverySpec.Includes,
	}

	// Set defaults if not specified
	if config.DefaultOptions.MaxDepth == 0 {
		config.DefaultOptions.MaxDepth = 3
	}

	return config
}

// CompatibilityChecker validates backwards compatibility
type CompatibilityChecker struct {
	supportedVersions map[string]bool
	deprecatedFields  map[string]string
}

// NewCompatibilityChecker creates a new compatibility checker
func NewCompatibilityChecker() *CompatibilityChecker {
	return &CompatibilityChecker{
		supportedVersions: map[string]bool{
			"troubleshoot.sh/v1beta1": true, // Legacy support
			"troubleshoot.sh/v1beta2": true, // Current
			"troubleshoot.sh/v1beta3": true, // Latest with auto-discovery
		},
		deprecatedFields: map[string]string{
			"spec.collectors.run": "spec.collectors.runPod", // Example deprecation
		},
	}
}

// CheckBackwardsCompatibility validates backwards compatibility
func (cc *CompatibilityChecker) CheckBackwardsCompatibility(spec *SupportBundleSpec) []CompatibilityWarning {
	var warnings []CompatibilityWarning

	// Check API version compatibility
	if !cc.supportedVersions[spec.APIVersion] {
		warnings = append(warnings, CompatibilityWarning{
			Type:        "unsupported_version",
			Message:     fmt.Sprintf("API version %s is not supported", spec.APIVersion),
			Severity:    "error",
			Suggestion:  "Use troubleshoot.sh/v1beta3 for auto-discovery support",
		})
	}

	// Check for auto-discovery with old API versions
	if spec.Spec.AutoDiscovery != nil && spec.APIVersion != "troubleshoot.sh/v1beta3" {
		warnings = append(warnings, CompatibilityWarning{
			Type:        "feature_version_mismatch",
			Message:     "Auto-discovery requires API version v1beta3",
			Severity:    "error",
			Suggestion:  "Update apiVersion to troubleshoot.sh/v1beta3",
		})
	}

	// Check for deprecated fields
	// This would inspect the collectors array for deprecated collector types
	for i, collector := range spec.Spec.Collectors {
		for field, replacement := range cc.deprecatedFields {
			if cc.hasDeprecatedField(collector, field) {
				warnings = append(warnings, CompatibilityWarning{
					Type:        "deprecated_field",
					Message:     fmt.Sprintf("Collector %d uses deprecated field %s", i, field),
					Severity:    "warning",
					Suggestion:  fmt.Sprintf("Replace with %s", replacement),
				})
			}
		}
	}

	return warnings
}

// CompatibilityWarning represents a backwards compatibility issue
type CompatibilityWarning struct {
	Type       string `json:"type"`
	Message    string `json:"message"`
	Severity   string `json:"severity"` // "error", "warning", "info"
	Suggestion string `json:"suggestion"`
	Field      string `json:"field,omitempty"`
}

func (cc *CompatibilityChecker) hasDeprecatedField(collector map[string]interface{}, field string) bool {
	// Simple field presence check - in a full implementation this would be more sophisticated
	return collector[field] != nil
}

// PrintCompatibilityWarnings prints compatibility warnings to console
func PrintCompatibilityWarnings(warnings []CompatibilityWarning) {
	if len(warnings) == 0 {
		fmt.Printf("✅ No compatibility issues found\n")
		return
	}

	fmt.Printf("⚠️  Compatibility Issues Found:\n")
	for i, warning := range warnings {
		icon := "ℹ️"
		if warning.Severity == "warning" {
			icon = "⚠️"
		} else if warning.Severity == "error" {
			icon = "❌"
		}

		fmt.Printf("  [%d] %s %s: %s\n", i+1, icon, warning.Type, warning.Message)
		if warning.Suggestion != "" {
			fmt.Printf("      💡 %s\n", warning.Suggestion)
		}
	}
}
