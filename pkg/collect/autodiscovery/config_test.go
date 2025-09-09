package autodiscovery

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestConfigManager_LoadFromFile(t *testing.T) {
	tests := []struct {
		name        string
		filename    string
		content     string
		expectError bool
		validate    func(*Config) error
	}{
		{
			name:     "valid YAML config",
			filename: "config.yaml",
			content: `
defaultOptions:
  namespaces: ["default", "app"]
  includeImages: true
  rbacCheck: true
  maxDepth: 3

resourceFilters:
  - name: "exclude-secrets"
    matchGVRs:
      - group: ""
        version: "v1"
        resource: "secrets"
    action: "exclude"

excludes:
  - gvrs:
      - group: ""
        version: "v1"
        resource: "secrets"
    names: ["admin-password"]
    reason: "Sensitive data"
`,
			expectError: false,
			validate: func(config *Config) error {
				if len(config.DefaultOptions.Namespaces) != 2 {
					return fmt.Errorf("expected 2 namespaces, got %d", len(config.DefaultOptions.Namespaces))
				}
				if !config.DefaultOptions.IncludeImages {
					return fmt.Errorf("expected includeImages to be true, got %v", config.DefaultOptions.IncludeImages)
				}
				if len(config.ResourceFilters) != 1 {
					return fmt.Errorf("expected 1 resource filter, got %d", len(config.ResourceFilters))
				}
				return nil
			},
		},
		{
			name:     "valid JSON config",
			filename: "config.json",
			content: `{
  "defaultOptions": {
    "namespaces": ["production"],
    "includeImages": false,
    "rbacCheck": true,
    "maxDepth": 2
  },
  "collectorMappings": [
    {
      "name": "custom-logs",
      "matchGVRs": [
        {
          "group": "",
          "version": "v1",
          "resource": "pods"
        }
      ],
      "collectorType": "logs",
      "priority": 10
    }
  ]
}`,
			expectError: false,
			validate: func(config *Config) error {
				if len(config.DefaultOptions.Namespaces) != 1 {
					return fmt.Errorf("expected 1 namespace, got %d", len(config.DefaultOptions.Namespaces))
				}
				if config.DefaultOptions.Namespaces[0] != "production" {
					return fmt.Errorf("expected production namespace")
				}
				if len(config.CollectorMappings) != 1 {
					return fmt.Errorf("expected 1 collector mapping, got %d", len(config.CollectorMappings))
				}
				return nil
			},
		},
		{
			name:     "invalid YAML",
			filename: "invalid.yaml",
			content: `
invalid: yaml: content
  - malformed
    structure
`,
			expectError: true,
		},
		{
			name:     "invalid JSON",
			filename: "invalid.json",
			content:  `{"invalid": json, "missing": quotes}`,
			expectError: true,
		},
		{
			name:        "unsupported file extension",
			filename:    "config.txt",
			content:     "some content",
			expectError: true,
		},
		{
			name:        "non-existent file",
			filename:    "nonexistent.yaml",
			content:     "", // Will not be created
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory
			tmpDir, err := os.MkdirTemp("", "config-test")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			configManager := NewConfigManager()
			
			var filePath string
			if tt.content != "" && tt.filename != "nonexistent.yaml" {
				// Create test file
				filePath = filepath.Join(tmpDir, tt.filename)
				err = os.WriteFile(filePath, []byte(tt.content), 0644)
				if err != nil {
					t.Fatalf("Failed to write test file: %v", err)
				}
			} else if tt.filename == "nonexistent.yaml" {
				filePath = filepath.Join(tmpDir, tt.filename)
			}

			err = configManager.LoadFromFile(filePath)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if !tt.expectError && tt.validate != nil {
				config := configManager.GetConfig()
				if validateErr := tt.validate(config); validateErr != nil {
					t.Errorf("Config validation failed: %v", validateErr)
				}
			}
		})
	}
}

func TestConfigManager_GetDiscoveryOptions(t *testing.T) {
	tests := []struct {
		name      string
		config    *Config
		overrides *DiscoveryOptions
		expected  DiscoveryOptions
	}{
		{
			name: "no overrides",
			config: &Config{
				DefaultOptions: DiscoveryOptions{
					Namespaces:    []string{"default"},
					IncludeImages: true,
					RBACCheck:     true,
					MaxDepth:      2,
				},
			},
			overrides: nil,
			expected: DiscoveryOptions{
				Namespaces:    []string{"default"},
				IncludeImages: true,
				RBACCheck:     true,
				MaxDepth:      2,
			},
		},
		{
			name: "with namespace override",
			config: &Config{
				DefaultOptions: DiscoveryOptions{
					Namespaces:    []string{"default"},
					IncludeImages: true,
					RBACCheck:     true,
					MaxDepth:      2,
				},
			},
			overrides: &DiscoveryOptions{
				Namespaces: []string{"production", "staging"},
			},
			expected: DiscoveryOptions{
				Namespaces:    []string{"production", "staging"},
				IncludeImages: true,
				RBACCheck:     true,
				MaxDepth:      2,
			},
		},
		{
			name: "with multiple overrides",
			config: &Config{
				DefaultOptions: DiscoveryOptions{
					Namespaces:    []string{"default"},
					IncludeImages: false,
					RBACCheck:     false,
					MaxDepth:      1,
				},
			},
			overrides: &DiscoveryOptions{
				Namespaces:    []string{"custom"},
				IncludeImages: true,
				MaxDepth:      5,
			},
			expected: DiscoveryOptions{
				Namespaces:    []string{"custom"},
				IncludeImages: true,
				RBACCheck:     false, // Not overridden
				MaxDepth:      5,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configManager := &ConfigManager{config: tt.config}
			result := configManager.GetDiscoveryOptions(tt.overrides)

			if len(result.Namespaces) != len(tt.expected.Namespaces) {
				t.Errorf("Expected %d namespaces, got %d", len(tt.expected.Namespaces), len(result.Namespaces))
			}

			for i, ns := range tt.expected.Namespaces {
				if i >= len(result.Namespaces) || result.Namespaces[i] != ns {
					t.Errorf("Expected namespace %s at index %d, got %s", ns, i, result.Namespaces[i])
				}
			}

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

func TestConfigManager_ApplyResourceFilters(t *testing.T) {
	tests := []struct {
		name      string
		config    *Config
		resources []Resource
		expected  int
	}{
		{
			name: "exclude filter",
			config: &Config{
				Excludes: []ResourceExcludeRule{
					{
						GVRs: []schema.GroupVersionResource{
							{Group: "", Version: "v1", Resource: "secrets"},
						},
						Reason: "Sensitive data",
					},
				},
				ResourceFilters: []ResourceFilterRule{
					{
						Name:   "exclude-system-pods",
						Action: "exclude",
						MatchGVRs: []schema.GroupVersionResource{
							{Group: "", Version: "v1", Resource: "pods"},
						},
						MatchNamespaces: []string{"kube-system"},
					},
				},
			},
			resources: []Resource{
				{
					GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
					Namespace: "default",
					Name:      "app-pod",
				},
				{
					GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
					Namespace: "kube-system",
					Name:      "system-pod",
				},
				{
					GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"},
					Namespace: "default",
					Name:      "secret1",
				},
			},
			expected: 1, // Only app-pod should remain
		},
		{
			name: "include filter",
			config: &Config{
				ResourceFilters: []ResourceFilterRule{
					{
						Name:   "include-only-production",
						Action: "include",
						MatchLabels: map[string]string{
							"env": "production",
						},
					},
				},
			},
			resources: []Resource{
				{
					GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
					Namespace: "default",
					Name:      "prod-pod",
					Labels:    map[string]string{"env": "production"},
				},
				{
					GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
					Namespace: "default",
					Name:      "test-pod",
					Labels:    map[string]string{"env": "test"},
				},
			},
			expected: 1, // Only prod-pod should remain
		},
		{
			name:      "no filters",
			config:    &Config{},
			resources: []Resource{
				{
					GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
					Namespace: "default",
					Name:      "pod1",
				},
				{
					GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"},
					Namespace: "default",
					Name:      "service1",
				},
			},
			expected: 2, // All resources should remain
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configManager := &ConfigManager{config: tt.config}
			filtered := configManager.ApplyResourceFilters(tt.resources)

			if len(filtered) != tt.expected {
				t.Errorf("Expected %d filtered resources, got %d", tt.expected, len(filtered))
			}
		})
	}
}

func TestConfigManager_GetCollectorMappings(t *testing.T) {
	config := &Config{
		CollectorMappings: []CollectorMappingRule{
			{
				Name:          "custom-pod-logs",
				CollectorType: "logs",
				Priority:      15,
				MatchGVRs: []schema.GroupVersionResource{
					{Group: "", Version: "v1", Resource: "pods"},
				},
				Parameters: map[string]interface{}{
					"maxAge":   "24h",
					"maxLines": 5000,
				},
			},
			{
				Name:          "custom-service-collector",
				CollectorType: "cluster-resources",
				Priority:      8,
				MatchGVRs: []schema.GroupVersionResource{
					{Group: "", Version: "v1", Resource: "services"},
				},
			},
		},
	}

	configManager := &ConfigManager{config: config}
	mappings := configManager.GetCollectorMappings()

	expectedMappings := 2
	if len(mappings) != expectedMappings {
		t.Errorf("Expected %d mappings, got %d", expectedMappings, len(mappings))
	}

	// Check pod mapping
	podKey := "_v1_pods"
	if mapping, exists := mappings[podKey]; !exists {
		t.Errorf("Expected pod mapping not found")
	} else {
		if mapping.CollectorType != "logs" {
			t.Errorf("Expected logs collector type, got %s", mapping.CollectorType)
		}
		if mapping.Priority != 15 {
			t.Errorf("Expected priority 15, got %d", mapping.Priority)
		}
		
		// Test parameter builder
		if mapping.ParameterBuilder == nil {
			t.Errorf("Parameter builder should not be nil")
		} else {
			testResource := Resource{Name: "test-pod", Namespace: "test-ns"}
			params := mapping.ParameterBuilder(testResource)
			
			if params["name"] != "test-pod" {
				t.Errorf("Expected name parameter to be test-pod")
			}
			if params["namespace"] != "test-ns" {
				t.Errorf("Expected namespace parameter to be test-ns")
			}
			if params["maxAge"] != "24h" {
				t.Errorf("Expected maxAge parameter from rule, got: %v", params["maxAge"])
			}
		}
	}

	// Check service mapping
	serviceKey := "_v1_services"
	if mapping, exists := mappings[serviceKey]; !exists {
		t.Errorf("Expected service mapping not found")
	} else {
		if mapping.CollectorType != "cluster-resources" {
			t.Errorf("Expected cluster-resources collector type, got %s", mapping.CollectorType)
		}
		if mapping.Priority != 8 {
			t.Errorf("Expected priority 8, got %d", mapping.Priority)
		}
	}
}

func TestConfigManager_SaveToFile(t *testing.T) {
	// Create a config to save
	config := &Config{
		DefaultOptions: DiscoveryOptions{
			Namespaces:    []string{"default", "test"},
			IncludeImages: true,
			RBACCheck:     false,
			MaxDepth:      3,
		},
		ResourceFilters: []ResourceFilterRule{
			{
				Name:   "test-filter",
				Action: "exclude",
				MatchNamespaces: []string{"kube-system"},
			},
		},
	}

	configManager := &ConfigManager{config: config}

	tests := []struct {
		name        string
		filename    string
		expectError bool
	}{
		{
			name:        "save as YAML",
			filename:    "test-config.yaml",
			expectError: false,
		},
		{
			name:        "save as JSON",
			filename:    "test-config.json",
			expectError: false,
		},
		{
			name:        "unsupported extension",
			filename:    "test-config.txt",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory
			tmpDir, err := os.MkdirTemp("", "config-save-test")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			filePath := filepath.Join(tmpDir, tt.filename)
			err = configManager.SaveToFile(filePath)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if !tt.expectError {
				// Verify file was created and can be loaded
				if _, err := os.Stat(filePath); os.IsNotExist(err) {
					t.Errorf("Config file was not created")
				}

				// Try loading the saved config
				newConfigManager := NewConfigManager()
				err = newConfigManager.LoadFromFile(filePath)
				if err != nil {
					t.Errorf("Failed to load saved config: %v", err)
				}

				// Verify content
				loadedConfig := newConfigManager.GetConfig()
				if len(loadedConfig.DefaultOptions.Namespaces) != len(config.DefaultOptions.Namespaces) {
					t.Errorf("Saved/loaded config namespace count mismatch")
				}
			}
		})
	}
}

func TestConfigManager_resourceMatchesFilter(t *testing.T) {
	configManager := NewConfigManager()

	tests := []struct {
		name     string
		resource Resource
		filter   ResourceFilterRule
		expected bool
	}{
		{
			name: "GVR match",
			resource: Resource{
				GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
				Namespace: "default",
				Name:      "test-pod",
			},
			filter: ResourceFilterRule{
				MatchGVRs: []schema.GroupVersionResource{
					{Group: "", Version: "v1", Resource: "pods"},
				},
			},
			expected: true,
		},
		{
			name: "GVR no match",
			resource: Resource{
				GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"},
				Namespace: "default",
				Name:      "test-service",
			},
			filter: ResourceFilterRule{
				MatchGVRs: []schema.GroupVersionResource{
					{Group: "", Version: "v1", Resource: "pods"},
				},
			},
			expected: false,
		},
		{
			name: "namespace match",
			resource: Resource{
				GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
				Namespace: "production",
				Name:      "app-pod",
			},
			filter: ResourceFilterRule{
				MatchNamespaces: []string{"production", "staging"},
			},
			expected: true,
		},
		{
			name: "namespace no match",
			resource: Resource{
				GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
				Namespace: "development",
				Name:      "dev-pod",
			},
			filter: ResourceFilterRule{
				MatchNamespaces: []string{"production", "staging"},
			},
			expected: false,
		},
		{
			name: "label match",
			resource: Resource{
				GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
				Namespace: "default",
				Name:      "app-pod",
				Labels:    map[string]string{"app": "web", "env": "production"},
			},
			filter: ResourceFilterRule{
				MatchLabels: map[string]string{"app": "web"},
			},
			expected: true,
		},
		{
			name: "label no match",
			resource: Resource{
				GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
				Namespace: "default",
				Name:      "db-pod",
				Labels:    map[string]string{"app": "database"},
			},
			filter: ResourceFilterRule{
				MatchLabels: map[string]string{"app": "web"},
			},
			expected: false,
		},
		{
			name: "multiple criteria - all match",
			resource: Resource{
				GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
				Namespace: "production",
				Name:      "web-pod",
				Labels:    map[string]string{"app": "web"},
			},
			filter: ResourceFilterRule{
				MatchGVRs: []schema.GroupVersionResource{
					{Group: "", Version: "v1", Resource: "pods"},
				},
				MatchNamespaces: []string{"production"},
				MatchLabels:     map[string]string{"app": "web"},
			},
			expected: true,
		},
		{
			name: "multiple criteria - partial match",
			resource: Resource{
				GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
				Namespace: "staging",
				Name:      "web-pod",
				Labels:    map[string]string{"app": "web"},
			},
			filter: ResourceFilterRule{
				MatchGVRs: []schema.GroupVersionResource{
					{Group: "", Version: "v1", Resource: "pods"},
				},
				MatchNamespaces: []string{"production"}, // This doesn't match
				MatchLabels:     map[string]string{"app": "web"},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := configManager.resourceMatchesFilter(tt.resource, tt.filter)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestGetDefaultConfig(t *testing.T) {
	config := getDefaultConfig()

	// Test default options
	if len(config.DefaultOptions.Namespaces) != 0 {
		t.Errorf("Expected empty namespaces by default")
	}
	if !config.DefaultOptions.IncludeImages {
		t.Errorf("Expected includeImages to be true by default")
	}
	if !config.DefaultOptions.RBACCheck {
		t.Errorf("Expected rbacCheck to be true by default")
	}
	if config.DefaultOptions.MaxDepth != 3 {
		t.Errorf("Expected maxDepth to be 3 by default, got %d", config.DefaultOptions.MaxDepth)
	}

	// Test default excludes
	if len(config.Excludes) == 0 {
		t.Errorf("Expected default excludes to be present")
	}

	// Check for system namespace exclusions
	foundSystemExcludes := false
	for _, exclude := range config.Excludes {
		for _, ns := range exclude.Namespaces {
			if ns == "kube-system" {
				foundSystemExcludes = true
				break
			}
		}
		if foundSystemExcludes {
			break
		}
	}
	if !foundSystemExcludes {
		t.Errorf("Expected system namespaces to be excluded by default")
	}
}

func TestMergeWithDefaults(t *testing.T) {
	userConfig := &Config{
		DefaultOptions: DiscoveryOptions{
			Namespaces:    []string{"custom"},
			IncludeImages: false,
			RBACCheck:     false,
			MaxDepth:      5,
		},
		ResourceFilters: []ResourceFilterRule{
			{
				Name:   "user-filter",
				Action: "include",
			},
		},
		Excludes: []ResourceExcludeRule{
			{
				Namespaces: []string{"user-exclude"},
				Reason:     "User specified",
			},
		},
	}

	merged := mergeWithDefaults(userConfig)

	// User settings should be preserved
	if len(merged.DefaultOptions.Namespaces) != 1 || merged.DefaultOptions.Namespaces[0] != "custom" {
		t.Errorf("User namespaces not preserved")
	}
	if merged.DefaultOptions.IncludeImages {
		t.Errorf("User includeImages setting not preserved")
	}
	if merged.DefaultOptions.MaxDepth != 5 {
		t.Errorf("User maxDepth setting not preserved")
	}

	// User filters should be preserved
	if len(merged.ResourceFilters) != 1 {
		t.Errorf("User resource filters not preserved")
	}

	// Default excludes should be merged with user excludes
	if len(merged.Excludes) < 2 { // At least default + user excludes
		t.Errorf("Default excludes not merged with user excludes")
	}
}
