package cli

import (
	"strings"
	"testing"

	"github.com/replicatedhq/troubleshoot/pkg/collect/autodiscovery"
)

func TestDiscoveryProfileManager_BuiltinProfiles(t *testing.T) {
	manager := NewDiscoveryProfileManager()

	expectedProfiles := []string{"minimal", "standard", "comprehensive", "debug"}

	for _, expectedName := range expectedProfiles {
		profile, err := manager.GetProfile(expectedName)
		if err != nil {
			t.Errorf("Expected builtin profile %s not found: %v", expectedName, err)
			continue
		}

		if profile.Name != expectedName {
			t.Errorf("Profile name mismatch: expected %s, got %s", expectedName, profile.Name)
		}

		if profile.Description == "" {
			t.Errorf("Profile %s should have description", expectedName)
		}

		// Validate profile-specific characteristics
		switch expectedName {
		case "minimal":
			if profile.Options.IncludeImages {
				t.Errorf("Minimal profile should not include images")
			}
			if profile.Options.MaxDepth > 2 {
				t.Errorf("Minimal profile should have low max depth")
			}
		case "standard":
			if profile.Options.MaxDepth != 3 {
				t.Errorf("Standard profile should have max depth 3, got %d", profile.Options.MaxDepth)
			}
		case "comprehensive":
			if !profile.Options.IncludeImages {
				t.Errorf("Comprehensive profile should include images")
			}
			if profile.Options.MaxDepth < 5 {
				t.Errorf("Comprehensive profile should have high max depth")
			}
		case "debug":
			if profile.Options.MaxDepth != 10 {
				t.Errorf("Debug profile should have max depth 10, got %d", profile.Options.MaxDepth)
			}
			if profile.Options.RBACCheck {
				t.Errorf("Debug profile should disable RBAC check")
			}
		}
	}
}

func TestDiscoveryProfileManager_ListProfiles(t *testing.T) {
	manager := NewDiscoveryProfileManager()
	profiles := manager.ListProfiles()

	expectedMin := 4 // At least the builtin profiles
	if len(profiles) < expectedMin {
		t.Errorf("Expected at least %d profiles, got %d", expectedMin, len(profiles))
	}

	// Check for required builtin profiles
	requiredProfiles := []string{"minimal", "standard", "comprehensive"}
	profileMap := make(map[string]bool)
	for _, profile := range profiles {
		profileMap[profile] = true
	}

	for _, required := range requiredProfiles {
		if !profileMap[required] {
			t.Errorf("Required profile %s not found in list", required)
		}
	}
}

func TestDiscoveryProfileManager_RegisterProfile(t *testing.T) {
	manager := NewDiscoveryProfileManager()

	tests := []struct {
		name        string
		profile     *DiscoveryProfile
		expectError bool
	}{
		{
			name: "valid custom profile",
			profile: &DiscoveryProfile{
				Name:        "custom-test",
				Description: "Custom test profile",
				Options: autodiscovery.DiscoveryOptions{
					IncludeImages: true,
					MaxDepth:      3,
				},
				Config: &autodiscovery.Config{
					ResourceFilters: []autodiscovery.ResourceFilterRule{
						{
							Name:   "test-filter",
							Action: "include",
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "profile with empty name",
			profile: &DiscoveryProfile{
				Name:        "",
				Description: "Test profile",
			},
			expectError: true,
		},
		{
			name: "profile with invalid max depth",
			profile: &DiscoveryProfile{
				Name:        "invalid-depth",
				Description: "Test profile",
				Options: autodiscovery.DiscoveryOptions{
					MaxDepth: -5,
				},
			},
			expectError: true,
		},
		{
			name: "profile with invalid collector mapping",
			profile: &DiscoveryProfile{
				Name:        "invalid-mapping",
				Description: "Test profile",
				Config: &autodiscovery.Config{
					CollectorMappings: []autodiscovery.CollectorMappingRule{
						{
							Name:          "", // Invalid empty name
							CollectorType: "logs",
						},
					},
				},
			},
			expectError: true,
		},
		{
			name: "profile with invalid filter action",
			profile: &DiscoveryProfile{
				Name:        "invalid-filter",
				Description: "Test profile",
				Config: &autodiscovery.Config{
					ResourceFilters: []autodiscovery.ResourceFilterRule{
						{
							Name:   "bad-filter",
							Action: "invalid-action", // Should be include/exclude
						},
					},
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := manager.RegisterProfile(tt.profile)

			if tt.expectError && err == nil {
				t.Errorf("Expected error registering profile but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error registering profile: %v", err)
			}

			// If successful, verify profile can be retrieved
			if !tt.expectError && err == nil {
				retrieved, err := manager.GetProfile(tt.profile.Name)
				if err != nil {
					t.Errorf("Failed to retrieve registered profile: %v", err)
				} else if retrieved.Name != tt.profile.Name {
					t.Errorf("Retrieved profile name mismatch")
				}
			}
		})
	}
}

func TestDiscoveryProfile_ApplyToOptions(t *testing.T) {
	profile := &DiscoveryProfile{
		Name: "test-profile",
		Options: autodiscovery.DiscoveryOptions{
			Namespaces:    []string{"profile-ns"},
			IncludeImages: true,
			RBACCheck:     false,
			MaxDepth:      5,
		},
	}

	tests := []struct {
		name        string
		baseOptions autodiscovery.DiscoveryOptions
		expected    autodiscovery.DiscoveryOptions
	}{
		{
			name: "apply profile to empty options",
			baseOptions: autodiscovery.DiscoveryOptions{},
			expected: autodiscovery.DiscoveryOptions{
				Namespaces:    []string{"profile-ns"},
				IncludeImages: true,
				RBACCheck:     false,
				MaxDepth:      5,
			},
		},
		{
			name: "preserve CLI-specified namespaces",
			baseOptions: autodiscovery.DiscoveryOptions{
				Namespaces: []string{"cli-ns-1", "cli-ns-2"},
			},
			expected: autodiscovery.DiscoveryOptions{
				Namespaces:    []string{"cli-ns-1", "cli-ns-2"}, // CLI takes precedence
				IncludeImages: true,                              // From profile
				RBACCheck:     false,                             // From profile
				MaxDepth:      5,                                 // From profile
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := profile.ApplyToOptions(tt.baseOptions)

			// Check namespaces
			if len(result.Namespaces) != len(tt.expected.Namespaces) {
				t.Errorf("Expected %d namespaces, got %d", len(tt.expected.Namespaces), len(result.Namespaces))
			}

			// Check boolean options
			if result.IncludeImages != tt.expected.IncludeImages {
				t.Errorf("Expected IncludeImages %v, got %v", tt.expected.IncludeImages, result.IncludeImages)
			}
			if result.RBACCheck != tt.expected.RBACCheck {
				t.Errorf("Expected RBACCheck %v, got %v", tt.expected.RBACCheck, result.RBACCheck)
			}
			if result.MaxDepth != tt.expected.MaxDepth {
				t.Errorf("Expected MaxDepth %d, got %d", tt.expected.MaxDepth, result.MaxDepth)
			}
		})
	}
}

func TestDiscoveryProfile_EstimateCollectionSize(t *testing.T) {
	tests := []struct {
		name     string
		profile  *DiscoveryProfile
		expected string
	}{
		{
			name: "minimal profile",
			profile: &DiscoveryProfile{
				Name: "minimal",
				Options: autodiscovery.DiscoveryOptions{
					IncludeImages: false,
					MaxDepth:      1,
				},
				Config: &autodiscovery.Config{
					Excludes: []autodiscovery.ResourceExcludeRule{
						{Namespaces: []string{"kube-system"}},
						{Namespaces: []string{"kube-public"}},
					},
				},
			},
			expected: "Small", // Should start with "Small"
		},
		{
			name: "comprehensive profile",
			profile: &DiscoveryProfile{
				Name: "comprehensive",
				Options: autodiscovery.DiscoveryOptions{
					IncludeImages: true,
					MaxDepth:      5,
				},
				Config: &autodiscovery.Config{
					ResourceFilters: []autodiscovery.ResourceFilterRule{
						{Name: "filter1"},
						{Name: "filter2"},
						{Name: "filter3"},
					},
					Excludes: []autodiscovery.ResourceExcludeRule{}, // No exclusions
				},
			},
			expected: "Large", // Should start with "Large"
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.profile.EstimateCollectionSize()
			
			if !strings.HasPrefix(result, tt.expected) {
				t.Errorf("Expected size estimate to start with %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestGetRecommendedProfile(t *testing.T) {
	tests := []struct {
		useCase  string
		expected string
	}{
		{"quick troubleshooting", "minimal"},
		{"fast diagnosis", "minimal"},
		{"application debugging", "standard"},
		{"app performance issue", "standard"},
		{"cluster infrastructure problem", "comprehensive"},
		{"cluster networking issue", "comprehensive"},
		{"deep debugging session", "debug"},
		{"unknown issue", "standard"}, // Default
		{"", "standard"},              // Empty case
	}

	for _, tt := range tests {
		t.Run(tt.useCase, func(t *testing.T) {
			result := GetRecommendedProfile(tt.useCase)
			if result != tt.expected {
				t.Errorf("Expected recommended profile %s for use case '%s', got %s", tt.expected, tt.useCase, result)
			}
		})
	}
}

func TestDiscoveryProfileManager_CompareProfiles(t *testing.T) {
	manager := NewDiscoveryProfileManager()

	comparison, err := manager.CompareProfiles("minimal", "comprehensive")
	if err != nil {
		t.Fatalf("Failed to compare profiles: %v", err)
	}

	// Verify comparison contains expected information
	if !strings.Contains(comparison, "minimal") {
		t.Errorf("Comparison should mention minimal profile")
	}
	if !strings.Contains(comparison, "comprehensive") {
		t.Errorf("Comparison should mention comprehensive profile")
	}
	if !strings.Contains(comparison, "Include Images") {
		t.Errorf("Comparison should include feature comparison")
	}

	// Test error case
	_, err = manager.CompareProfiles("nonexistent", "minimal")
	if err == nil {
		t.Errorf("Expected error comparing with nonexistent profile")
	}
}

func TestDiscoveryProfile_GetProfileDescription(t *testing.T) {
	profile := &DiscoveryProfile{
		Name:        "test-profile",
		Description: "Test profile for unit testing",
		Options: autodiscovery.DiscoveryOptions{
			IncludeImages: true,
			RBACCheck:     false,
			MaxDepth:      3,
		},
		Config: &autodiscovery.Config{
			ResourceFilters: []autodiscovery.ResourceFilterRule{
				{Name: "filter1"},
				{Name: "filter2"},
			},
			CollectorMappings: []autodiscovery.CollectorMappingRule{
				{Name: "mapping1"},
			},
			Excludes: []autodiscovery.ResourceExcludeRule{
				{Reason: "exclude1"},
			},
		},
	}

	description := profile.GetProfileDescription()

	// Verify description contains key information
	if !strings.Contains(description, profile.Name) {
		t.Errorf("Description should contain profile name")
	}
	if !strings.Contains(description, "Include Images: true") {
		t.Errorf("Description should show image inclusion setting")
	}
	if !strings.Contains(description, "Max Depth: 3") {
		t.Errorf("Description should show max depth")
	}
	if !strings.Contains(description, "Resource Filters: 2") {
		t.Errorf("Description should show resource filter count")
	}
}

func TestDiscoveryProfileManager_GetProfilesOverview(t *testing.T) {
	manager := NewDiscoveryProfileManager()

	// Add a custom profile for testing
	customProfile := &DiscoveryProfile{
		Name:        "custom-test",
		Description: "Custom test profile",
		Options:     autodiscovery.DiscoveryOptions{},
	}
	err := manager.RegisterProfile(customProfile)
	if err != nil {
		t.Fatalf("Failed to register custom profile: %v", err)
	}

	overview := manager.GetProfilesOverview()

	// Verify overview contains all profiles
	expectedProfiles := []string{"minimal", "standard", "comprehensive", "debug", "custom-test"}
	for _, expected := range expectedProfiles {
		if !strings.Contains(overview, expected) {
			t.Errorf("Overview should contain profile %s", expected)
		}
	}

	// Verify structure
	if !strings.Contains(overview, "Available Discovery Profiles") {
		t.Errorf("Overview should have header")
	}
	if !strings.Contains(overview, "(custom)") {
		t.Errorf("Overview should mark custom profiles")
	}
}

// Benchmark tests
func BenchmarkDiscoveryProfileManager_GetProfile(b *testing.B) {
	manager := NewDiscoveryProfileManager()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := manager.GetProfile("standard")
		if err != nil {
			b.Fatalf("GetProfile failed: %v", err)
		}
	}
}

func BenchmarkDiscoveryProfile_ApplyToOptions(b *testing.B) {
	manager := NewDiscoveryProfileManager()
	profile, err := manager.GetProfile("standard")
	if err != nil {
		b.Fatalf("Failed to get standard profile: %v", err)
	}

	baseOptions := autodiscovery.DiscoveryOptions{
		Namespaces: []string{"test-ns"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := profile.ApplyToOptions(baseOptions)
		if len(result.Namespaces) == 0 {
			b.Fatalf("Applied options should preserve namespaces")
		}
	}
}
