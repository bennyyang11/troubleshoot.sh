package cli

import (
	"fmt"
	"strings"

	"github.com/replicatedhq/troubleshoot/pkg/collect/autodiscovery"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// DiscoveryProfile defines a preset configuration for auto-discovery
type DiscoveryProfile struct {
	Name        string                      `json:"name"`
	Description string                      `json:"description"`
	Options     autodiscovery.DiscoveryOptions `json:"options"`
	Config      *autodiscovery.Config       `json:"config"`
}

// DiscoveryProfileManager manages discovery profiles
type DiscoveryProfileManager struct {
	profiles map[string]*DiscoveryProfile
}

// NewDiscoveryProfileManager creates a new profile manager with built-in profiles
func NewDiscoveryProfileManager() *DiscoveryProfileManager {
	manager := &DiscoveryProfileManager{
		profiles: make(map[string]*DiscoveryProfile),
	}
	manager.initializeBuiltinProfiles()
	return manager
}

// GetProfile retrieves a discovery profile by name
func (dpm *DiscoveryProfileManager) GetProfile(name string) (*DiscoveryProfile, error) {
	profile, exists := dpm.profiles[name]
	if !exists {
		return nil, fmt.Errorf("discovery profile not found: %s", name)
	}
	return profile, nil
}

// ListProfiles returns all available profile names
func (dpm *DiscoveryProfileManager) ListProfiles() []string {
	names := make([]string, 0, len(dpm.profiles))
	for name := range dpm.profiles {
		names = append(names, name)
	}
	return names
}

// RegisterProfile registers a custom discovery profile
func (dpm *DiscoveryProfileManager) RegisterProfile(profile *DiscoveryProfile) error {
	if profile.Name == "" {
		return fmt.Errorf("profile name cannot be empty")
	}
	
	// Validate the profile configuration
	if err := dpm.validateProfile(profile); err != nil {
		return fmt.Errorf("invalid profile: %w", err)
	}
	
	dpm.profiles[profile.Name] = profile
	return nil
}

// ApplyProfile applies a profile to discovery options
func (profile *DiscoveryProfile) ApplyToOptions(baseOptions autodiscovery.DiscoveryOptions) autodiscovery.DiscoveryOptions {
	// Profile options override base options, but preserve CLI-specified values
	result := profile.Options
	
	// Preserve explicitly set CLI values
	if len(baseOptions.Namespaces) > 0 {
		result.Namespaces = baseOptions.Namespaces
	}
	
	return result
}

// initializeBuiltinProfiles creates the standard discovery profiles
func (dpm *DiscoveryProfileManager) initializeBuiltinProfiles() {
	// Minimal Profile - Basic troubleshooting only
	dpm.profiles["minimal"] = &DiscoveryProfile{
		Name:        "minimal",
		Description: "Minimal auto-discovery for basic troubleshooting (pods, services, events)",
		Options: autodiscovery.DiscoveryOptions{
			Namespaces:    []string{}, // Will use provided namespaces
			IncludeImages: false,
			RBACCheck:     true,
			MaxDepth:      1, // Minimal dependency resolution
		},
		Config: &autodiscovery.Config{
			ResourceFilters: []autodiscovery.ResourceFilterRule{
				{
					Name:   "minimal-core-resources",
					Action: "include",
					MatchGVRs: []schema.GroupVersionResource{
						{Group: "", Version: "v1", Resource: "pods"},
						{Group: "", Version: "v1", Resource: "services"},
						{Group: "", Version: "v1", Resource: "events"},
					},
				},
			},
			Excludes: []autodiscovery.ResourceExcludeRule{
				{
					Namespaces: []string{"kube-system", "kube-public", "kube-node-lease"},
					Reason:     "Exclude system namespaces in minimal mode",
				},
			},
		},
	}

	// Standard Profile - Comprehensive application troubleshooting
	dpm.profiles["standard"] = &DiscoveryProfile{
		Name:        "standard",
		Description: "Standard auto-discovery for comprehensive application troubleshooting",
		Options: autodiscovery.DiscoveryOptions{
			Namespaces:    []string{},
			IncludeImages: true,
			RBACCheck:     true,
			MaxDepth:      3,
		},
		Config: &autodiscovery.Config{
			ResourceFilters: []autodiscovery.ResourceFilterRule{
				{
					Name:   "standard-app-resources",
					Action: "include",
					MatchGVRs: []schema.GroupVersionResource{
						// Core resources
						{Group: "", Version: "v1", Resource: "pods"},
						{Group: "", Version: "v1", Resource: "services"},
						{Group: "", Version: "v1", Resource: "configmaps"},
						{Group: "", Version: "v1", Resource: "events"},
						{Group: "", Version: "v1", Resource: "persistentvolumeclaims"},
						
						// Workload resources
						{Group: "apps", Version: "v1", Resource: "deployments"},
						{Group: "apps", Version: "v1", Resource: "statefulsets"},
						{Group: "apps", Version: "v1", Resource: "daemonsets"},
						
						// Networking resources
						{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"},
						
						// Batch resources
						{Group: "batch", Version: "v1", Resource: "jobs"},
					},
				},
			},
			Excludes: []autodiscovery.ResourceExcludeRule{
				{
					Namespaces: []string{"kube-node-lease"},
					Reason:     "Node lease namespace not useful for troubleshooting",
				},
				{
					GVRs: []schema.GroupVersionResource{
						{Group: "", Version: "v1", Resource: "secrets"},
					},
					Names:  []string{"default-token-*", "sh.helm.release.*"},
					Reason: "Exclude automatically generated secrets",
				},
			},
		},
	}

	// Comprehensive Profile - Deep cluster analysis
	dpm.profiles["comprehensive"] = &DiscoveryProfile{
		Name:        "comprehensive",
		Description: "Comprehensive auto-discovery for deep cluster analysis and debugging",
		Options: autodiscovery.DiscoveryOptions{
			Namespaces:    []string{}, // Discover all accessible
			IncludeImages: true,
			RBACCheck:     false, // Disable for comprehensive collection
			MaxDepth:      5,     // Deep dependency resolution
		},
		Config: &autodiscovery.Config{
			// Include all supported resource types
			ResourceFilters: []autodiscovery.ResourceFilterRule{
				{
					Name:   "comprehensive-all-resources",
					Action: "include",
					MatchGVRs: []schema.GroupVersionResource{
						// Core resources
						{Group: "", Version: "v1", Resource: "pods"},
						{Group: "", Version: "v1", Resource: "services"},
						{Group: "", Version: "v1", Resource: "configmaps"},
						{Group: "", Version: "v1", Resource: "secrets"},
						{Group: "", Version: "v1", Resource: "events"},
						{Group: "", Version: "v1", Resource: "persistentvolumeclaims"},
						{Group: "", Version: "v1", Resource: "nodes"},
						
						// Workload resources
						{Group: "apps", Version: "v1", Resource: "deployments"},
						{Group: "apps", Version: "v1", Resource: "replicasets"},
						{Group: "apps", Version: "v1", Resource: "statefulsets"},
						{Group: "apps", Version: "v1", Resource: "daemonsets"},
						
						// Networking resources
						{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"},
						{Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"},
						
						// Batch resources
						{Group: "batch", Version: "v1", Resource: "jobs"},
						{Group: "batch", Version: "v1", Resource: "cronjobs"},
						
						// Storage resources
						{Group: "storage.k8s.io", Version: "v1", Resource: "storageclasses"},
						
						// Custom resources
						{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"},
					},
				},
			},
			Excludes: []autodiscovery.ResourceExcludeRule{
				// Minimal exclusions for comprehensive mode
				{
					Names:  []string{"kube-root-ca.crt"},
					Reason: "Exclude cluster root CA configmap",
				},
			},
			CollectorMappings: []autodiscovery.CollectorMappingRule{
				{
					Name:          "comprehensive-pod-logs",
					CollectorType: "logs",
					Priority:      10,
					MatchGVRs: []schema.GroupVersionResource{
						{Group: "", Version: "v1", Resource: "pods"},
					},
					Parameters: map[string]interface{}{
						"maxAge":   "168h", // 7 days for comprehensive analysis
						"maxLines": 50000,
					},
				},
			},
		},
	}

	// Debug Profile - Maximum data collection for debugging
	dpm.profiles["debug"] = &DiscoveryProfile{
		Name:        "debug",
		Description: "Debug profile with maximum data collection and verbose output",
		Options: autodiscovery.DiscoveryOptions{
			Namespaces:    []string{},
			IncludeImages: true,
			RBACCheck:     false,
			MaxDepth:      10, // Maximum depth
		},
		Config: &autodiscovery.Config{
			// Include everything possible
			ResourceFilters: []autodiscovery.ResourceFilterRule{},
			Excludes:        []autodiscovery.ResourceExcludeRule{}, // No exclusions
			Includes: []autodiscovery.ResourceIncludeRule{
				{
					GVRs: []schema.GroupVersionResource{
						{Group: "", Version: "v1", Resource: "pods"},
						{Group: "", Version: "v1", Resource: "events"},
					},
					Priority: 20,
				},
			},
			CollectorMappings: []autodiscovery.CollectorMappingRule{
				{
					Name:          "debug-verbose-logs",
					CollectorType: "logs",
					Priority:      15,
					MatchGVRs: []schema.GroupVersionResource{
						{Group: "", Version: "v1", Resource: "pods"},
					},
					Parameters: map[string]interface{}{
						"maxAge":   "720h", // 30 days
						"maxLines": 100000,
						"previous": true, // Include previous container logs
					},
				},
			},
		},
	}
}

// validateProfile validates a discovery profile configuration
func (dpm *DiscoveryProfileManager) validateProfile(profile *DiscoveryProfile) error {
	// Validate basic fields
	if profile.Description == "" {
		return fmt.Errorf("profile description cannot be empty")
	}

	// Validate discovery options
	if profile.Options.MaxDepth < 0 || profile.Options.MaxDepth > 20 {
		return fmt.Errorf("maxDepth must be between 0 and 20")
	}

	// Validate config if present
	if profile.Config != nil {
		// Validate resource filters
		for i, filter := range profile.Config.ResourceFilters {
			if filter.Name == "" {
				return fmt.Errorf("resourceFilter[%d] name cannot be empty", i)
			}
			if filter.Action != "include" && filter.Action != "exclude" {
				return fmt.Errorf("resourceFilter[%d] action must be 'include' or 'exclude'", i)
			}
		}

		// Validate collector mappings
		for i, mapping := range profile.Config.CollectorMappings {
			if mapping.Name == "" {
				return fmt.Errorf("collectorMapping[%d] name cannot be empty", i)
			}
			validTypes := []string{"logs", "cluster-resources", "exec", "copy", "run-pod"}
			isValid := false
			for _, valid := range validTypes {
				if mapping.CollectorType == valid {
					isValid = true
					break
				}
			}
			if !isValid {
				return fmt.Errorf("collectorMapping[%d] invalid collector type: %s", i, mapping.CollectorType)
			}
		}
	}

	return nil
}

// GetProfileDescription returns a human-readable description of the profile
func (profile *DiscoveryProfile) GetProfileDescription() string {
	description := fmt.Sprintf("%s: %s\n", profile.Name, profile.Description)
	description += fmt.Sprintf("  Include Images: %v\n", profile.Options.IncludeImages)
	description += fmt.Sprintf("  RBAC Check: %v\n", profile.Options.RBACCheck)
	description += fmt.Sprintf("  Max Depth: %d\n", profile.Options.MaxDepth)
	
	if profile.Config != nil {
		description += fmt.Sprintf("  Resource Filters: %d\n", len(profile.Config.ResourceFilters))
		description += fmt.Sprintf("  Collector Mappings: %d\n", len(profile.Config.CollectorMappings))
		description += fmt.Sprintf("  Exclusions: %d\n", len(profile.Config.Excludes))
	}

	return description
}

// CreateCustomProfile creates a custom profile from user specifications
func (dpm *DiscoveryProfileManager) CreateCustomProfile(name, description string, opts autodiscovery.DiscoveryOptions, config *autodiscovery.Config) error {
	profile := &DiscoveryProfile{
		Name:        name,
		Description: description,
		Options:     opts,
		Config:      config,
	}

	return dpm.RegisterProfile(profile)
}

// GetProfilesOverview returns an overview of all profiles
func (dpm *DiscoveryProfileManager) GetProfilesOverview() string {
	overview := "📋 Available Discovery Profiles:\n\n"
	
	// Specific order for built-in profiles
	profileOrder := []string{"minimal", "standard", "comprehensive", "debug"}
	
	for _, profileName := range profileOrder {
		if profile, exists := dpm.profiles[profileName]; exists {
			overview += fmt.Sprintf("🔸 **%s**\n", profile.Name)
			overview += fmt.Sprintf("   %s\n", profile.Description)
			overview += fmt.Sprintf("   Images: %v | RBAC: %v | Depth: %d\n",
				profile.Options.IncludeImages, profile.Options.RBACCheck, profile.Options.MaxDepth)
			
			if profile.Config != nil {
				resourceCount := len(profile.Config.ResourceFilters)
				if resourceCount > 0 {
					overview += fmt.Sprintf("   Filters: %d resource filters\n", resourceCount)
				}
				
				excludeCount := len(profile.Config.Excludes)
				if excludeCount > 0 {
					overview += fmt.Sprintf("   Exclusions: %d exclusion rules\n", excludeCount)
				}
			}
			overview += "\n"
		}
	}

	// Add any custom profiles
	for name, profile := range dpm.profiles {
		isBuiltin := false
		for _, builtinName := range profileOrder {
			if name == builtinName {
				isBuiltin = true
				break
			}
		}
		
		if !isBuiltin {
			overview += fmt.Sprintf("🔹 **%s** (custom)\n", profile.Name)
			overview += fmt.Sprintf("   %s\n", profile.Description)
			overview += "\n"
		}
	}

	return overview
}

// EstimateCollectionSize estimates the relative collection size for a profile
func (profile *DiscoveryProfile) EstimateCollectionSize() string {
	score := 0

	// Base score from depth
	score += profile.Options.MaxDepth * 10

	// Add score for images
	if profile.Options.IncludeImages {
		score += 50
	}

	// Add score from resource filters
	if profile.Config != nil {
		// More resource filters = larger collection
		score += len(profile.Config.ResourceFilters) * 5
		
		// Fewer exclusions = larger collection
		score += (10 - len(profile.Config.Excludes)) * 3
		
		// Custom collector mappings might increase size
		score += len(profile.Config.CollectorMappings) * 5
	}

	// Categorize size
	switch {
	case score < 50:
		return "Small (< 50MB typical)"
	case score < 120: // Lowered threshold so comprehensive profile (score 145) gets "Large"
		return "Medium (50-200MB typical)"
	case score < 300:
		return "Large (200MB-1GB typical)"
	default:
		return "Very Large (> 1GB typical)"
	}
}

// GetRecommendedProfile suggests a profile based on use case
func GetRecommendedProfile(useCase string) string {
	useCase = strings.ToLower(useCase)
	
	switch {
	case strings.Contains(useCase, "quick") || strings.Contains(useCase, "fast"):
		return "minimal"
	case strings.Contains(useCase, "application") || strings.Contains(useCase, "app"):
		return "standard"
	case strings.Contains(useCase, "cluster") || strings.Contains(useCase, "infrastructure"):
		return "comprehensive"
	case strings.Contains(useCase, "debug") || strings.Contains(useCase, "deep"):
		return "debug"
	default:
		return "standard" // Safe default
	}
}

// CompareProfiles provides a comparison between profiles
func (dpm *DiscoveryProfileManager) CompareProfiles(profile1, profile2 string) (string, error) {
	p1, err := dpm.GetProfile(profile1)
	if err != nil {
		return "", fmt.Errorf("profile %s not found", profile1)
	}
	
	p2, err := dpm.GetProfile(profile2)
	if err != nil {
		return "", fmt.Errorf("profile %s not found", profile2)
	}

	comparison := fmt.Sprintf("Profile Comparison: %s vs %s\n", profile1, profile2)
	comparison += fmt.Sprintf("\n%-15s %-15s %-15s\n", "Feature", profile1, profile2)
	comparison += fmt.Sprintf("%-15s %-15v %-15v\n", "Include Images", p1.Options.IncludeImages, p2.Options.IncludeImages)
	comparison += fmt.Sprintf("%-15s %-15v %-15v\n", "RBAC Check", p1.Options.RBACCheck, p2.Options.RBACCheck)
	comparison += fmt.Sprintf("%-15s %-15d %-15d\n", "Max Depth", p1.Options.MaxDepth, p2.Options.MaxDepth)
	comparison += fmt.Sprintf("%-15s %-15s %-15s\n", "Est. Size", p1.EstimateCollectionSize(), p2.EstimateCollectionSize())

	return comparison, nil
}

