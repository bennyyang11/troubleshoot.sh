package cli

import (
	"fmt"
	"strings"
	"testing"

	"github.com/replicatedhq/troubleshoot/pkg/collect/autodiscovery"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestPatternParser_ParseExclusionFlag(t *testing.T) {
	tests := []struct {
		name         string
		flag         string
		expectError  bool
		expectedCount int
	}{
		{
			name:         "simple exclusion list",
			flag:         "kube-system,kube-public",
			expectError:  false,
			expectedCount: 2,
		},
		{
			name:         "exclusion with patterns",
			flag:         "ns:kube-*;label:app=test",
			expectError:  false,
			expectedCount: 2,
		},
		{
			name:         "complex exclusion patterns",
			flag:         "secrets,ns:test-*,gvr:apps/v1/deployments",
			expectError:  false,
			expectedCount: 3,
		},
		{
			name:         "invalid regex pattern",
			flag:         "regex:[invalid",
			expectError:  true,
		},
		{
			name:         "empty flag",
			flag:         "",
			expectError:  false,
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewPatternParser()
			err := parser.ParseExclusionFlag(tt.flag)

			if tt.expectError && err == nil {
				t.Errorf("Expected error parsing exclusion flag but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error parsing exclusion flag: %v", err)
			}

			if !tt.expectError {
				exclusions := parser.GetExclusionPatterns()
				if len(exclusions) != tt.expectedCount {
					t.Errorf("Expected %d exclusion patterns, got %d", tt.expectedCount, len(exclusions))
				}
			}
		})
	}
}

func TestPatternParser_ParseInclusionFlag(t *testing.T) {
	tests := []struct {
		name         string
		flag         string
		expectError  bool
		expectedCount int
	}{
		{
			name:         "simple inclusion list",
			flag:         "pods,services,deployments",
			expectError:  false,
			expectedCount: 3,
		},
		{
			name:         "inclusion with label selector",
			flag:         "label:app=web,pods",
			expectError:  false,
			expectedCount: 2,
		},
		{
			name:         "inclusion with GVR patterns",
			flag:         "gvr:apps/v1/deployments;gvr:v1/services",
			expectError:  false,
			expectedCount: 2,
		},
		{
			name:         "invalid pattern type",
			flag:         "unknown:value",
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewPatternParser()
			err := parser.ParseInclusionFlag(tt.flag)

			if tt.expectError && err == nil {
				t.Errorf("Expected error parsing inclusion flag but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error parsing inclusion flag: %v", err)
			}

			if !tt.expectError {
				inclusions := parser.GetInclusionPatterns()
				if len(inclusions) != tt.expectedCount {
					t.Errorf("Expected %d inclusion patterns, got %d", tt.expectedCount, len(inclusions))
				}
			}
		})
	}
}

func TestPatternParser_ConvertToResourceFilterRules(t *testing.T) {
	parser := NewPatternParser()
	
	// Add some test patterns
	err := parser.ParseExclusionFlag("secrets,ns:kube-system")
	if err != nil {
		t.Fatalf("Failed to parse exclusion patterns: %v", err)
	}
	
	err = parser.ParseInclusionFlag("pods,label:app=web")
	if err != nil {
		t.Fatalf("Failed to parse inclusion patterns: %v", err)
	}

	rules := parser.ConvertToResourceFilterRules()

	// Should have 4 rules: 2 exclusions + 2 inclusions
	if len(rules) != 4 {
		t.Errorf("Expected 4 resource filter rules, got %d", len(rules))
	}

	// Count exclusion and inclusion rules
	excludeCount := 0
	includeCount := 0
	
	for _, rule := range rules {
		switch rule.Action {
		case "exclude":
			excludeCount++
		case "include":
			includeCount++
		}
	}

	if excludeCount != 2 {
		t.Errorf("Expected 2 exclude rules, got %d", excludeCount)
	}
	if includeCount != 2 {
		t.Errorf("Expected 2 include rules, got %d", includeCount)
	}

	// Verify specific rule content
	foundSecretsExclusion := false
	foundPodsInclusion := false

	for _, rule := range rules {
		if rule.Action == "exclude" {
			for _, gvr := range rule.MatchGVRs {
				if gvr.Resource == "secrets" {
					foundSecretsExclusion = true
				}
			}
		}
		if rule.Action == "include" {
			for _, gvr := range rule.MatchGVRs {
				if gvr.Resource == "pods" {
					foundPodsInclusion = true
				}
			}
		}
	}

	if !foundSecretsExclusion {
		t.Errorf("Should have secrets exclusion rule")
	}
	if !foundPodsInclusion {
		t.Errorf("Should have pods inclusion rule")
	}
}

func TestPatternParser_TestPatternMatching(t *testing.T) {
	parser := NewPatternParser()
	err := parser.ParseExclusionFlag("ns:kube-system,secrets")
	if err != nil {
		t.Fatalf("Failed to parse patterns: %v", err)
	}

	// Create test resources
	sampleResources := []autodiscovery.Resource{
		{
			GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
			Namespace: "default",
			Name:      "app-pod",
		},
		{
			GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"},
			Namespace: "default",
			Name:      "app-secret",
		},
		{
			GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
			Namespace: "kube-system",
			Name:      "system-pod",
		},
	}

	results := parser.TestPatternMatching(sampleResources)

	// Should have results for each resource × pattern combination
	if len(results) == 0 {
		t.Errorf("Should have pattern matching results")
	}

	// Verify some matches
	foundExclusions := false
	for _, result := range results {
		if strings.Contains(result.Pattern, "exclude") && result.Matched {
			foundExclusions = true
		}
	}

	if !foundExclusions {
		t.Errorf("Should find some exclusion matches")
	}
}

func TestPatternParser_ValidatePattern(t *testing.T) {
	parser := NewPatternParser()

	tests := []struct {
		pattern     string
		expectError bool
	}{
		// Valid patterns
		{"pods", false},
		{"services", false},
		{"ns:kube-system", false},
		{"namespace:default", false},
		{"label:app=web", false},
		{"labels:env=production,app=web", false},
		{"gvr:apps/v1/deployments", false},
		{"gvr:v1/pods", false},
		{"regex:^app-.*$", false},
		{"kube-*", false}, // Wildcard

		// Invalid patterns
		{"", true},                    // Empty
		{"unknown:value", true},       // Unknown type
		{"ns:", true},                 // Empty namespace
		{"label:", true},              // Empty label
		{"regex:[invalid", true},      // Invalid regex
		{"gvr:invalid", false},        // Single resource name is actually valid
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			err := parser.validatePattern(tt.pattern)

			if tt.expectError && err == nil {
				t.Errorf("Expected validation error for pattern '%s' but got none", tt.pattern)
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected validation error for pattern '%s': %v", tt.pattern, err)
			}
		})
	}
}

func TestPatternParser_ResourceNameToGVR(t *testing.T) {
	parser := NewPatternParser()

	tests := []struct {
		resourceName string
		expectedGVR  schema.GroupVersionResource
	}{
		{
			resourceName: "pods",
			expectedGVR:  schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
		},
		{
			resourceName: "deployments",
			expectedGVR:  schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
		},
		{
			resourceName: "ingresses",
			expectedGVR:  schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"},
		},
		{
			resourceName: "unknown-resource",
			expectedGVR:  schema.GroupVersionResource{Group: "", Version: "v1", Resource: "unknown-resource"}, // Default to core
		},
	}

	for _, tt := range tests {
		t.Run(tt.resourceName, func(t *testing.T) {
			result := parser.resourceNameToGVR(tt.resourceName)

			if result.Group != tt.expectedGVR.Group {
				t.Errorf("Expected group %s, got %s", tt.expectedGVR.Group, result.Group)
			}
			if result.Version != tt.expectedGVR.Version {
				t.Errorf("Expected version %s, got %s", tt.expectedGVR.Version, result.Version)
			}
			if result.Resource != tt.expectedGVR.Resource {
				t.Errorf("Expected resource %s, got %s", tt.expectedGVR.Resource, result.Resource)
			}
		})
	}
}

func TestPatternParser_ParseGVR(t *testing.T) {
	parser := NewPatternParser()

	tests := []struct {
		name        string
		gvrString   string
		expectedGVR schema.GroupVersionResource
		expectError bool
	}{
		{
			name:      "full GVR",
			gvrString: "apps/v1/deployments",
			expectedGVR: schema.GroupVersionResource{
				Group:    "apps",
				Version:  "v1",
				Resource: "deployments",
			},
			expectError: false,
		},
		{
			name:      "core resource GVR",
			gvrString: "v1/pods",
			expectedGVR: schema.GroupVersionResource{
				Group:    "",
				Version:  "v1",
				Resource: "pods",
			},
			expectError: false,
		},
		{
			name:      "resource only",
			gvrString: "services",
			expectedGVR: schema.GroupVersionResource{
				Group:    "",
				Version:  "v1",
				Resource: "services",
			},
			expectError: false,
		},
		{
			name:        "invalid GVR format",
			gvrString:   "apps/v1/deployments/extra",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parser.parseGVR(tt.gvrString)

			if tt.expectError && err == nil {
				t.Errorf("Expected error parsing GVR but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error parsing GVR: %v", err)
			}

			if !tt.expectError {
				if result != tt.expectedGVR {
					t.Errorf("Expected GVR %+v, got %+v", tt.expectedGVR, result)
				}
			}
		})
	}
}

func TestGetPatternsHelp(t *testing.T) {
	help := GetPatternsHelp()

	// Verify help contains key information
	expectedSections := []string{
		"Pattern Syntax Help",
		"Resource Types",
		"Namespace Patterns", 
		"Label Selectors",
		"GVR (Group/Version/Resource)",
		"Regex Patterns",
		"Examples",
		"--exclude",
		"--include",
	}

	for _, expected := range expectedSections {
		if !strings.Contains(help, expected) {
			t.Errorf("Pattern help should contain section: %s", expected)
		}
	}

	// Verify examples are present
	examplePatterns := []string{
		"kube-*",
		"app=myapp",
		"apps/v1/deployments",
		"regex:^app-.*$",
	}

	for _, pattern := range examplePatterns {
		if !strings.Contains(help, pattern) {
			t.Errorf("Pattern help should contain example: %s", pattern)
		}
	}
}

func TestGeneratePatternExamples(t *testing.T) {
	examples := GeneratePatternExamples()

	// Should have several examples
	if len(examples) == 0 {
		t.Errorf("Should have pattern examples")
	}

	// Verify each example has required fields
	for i, example := range examples {
		if example.Name == "" {
			t.Errorf("Example %d should have name", i)
		}
		if example.Pattern == "" {
			t.Errorf("Example %d should have pattern", i)
		}
		if example.Description == "" {
			t.Errorf("Example %d should have description", i)
		}

		// Verify pattern syntax looks reasonable
		if !strings.Contains(example.Pattern, "--") {
			t.Errorf("Example %d pattern should be a command flag", i)
		}
	}

	// Verify examples cover different pattern types
	foundNamespaceExample := false
	foundLabelExample := false
	foundGVRExample := false

	for _, example := range examples {
		if strings.Contains(example.Pattern, "ns:") || strings.Contains(example.Pattern, "namespace:") {
			foundNamespaceExample = true
		}
		if strings.Contains(example.Pattern, "label:") {
			foundLabelExample = true
		}
		if strings.Contains(example.Pattern, "gvr:") {
			foundGVRExample = true
		}
	}

	if !foundNamespaceExample {
		t.Errorf("Should have namespace pattern example")
	}
	if !foundLabelExample {
		t.Errorf("Should have label pattern example")
	}
	if !foundGVRExample {
		t.Errorf("Should have GVR pattern example")
	}
}

func TestPatternParser_TestLabelSelector(t *testing.T) {
	parser := NewPatternParser()

	tests := []struct {
		name     string
		selector string
		labels   map[string]string
		expected bool
	}{
		{
			name:     "simple key-value match",
			selector: "app=web",
			labels:   map[string]string{"app": "web", "version": "v1"},
			expected: true,
		},
		{
			name:     "simple key-value no match",
			selector: "app=database",
			labels:   map[string]string{"app": "web"},
			expected: false,
		},
		{
			name:     "multiple selectors match",
			selector: "app=web,env=production",
			labels:   map[string]string{"app": "web", "env": "production", "version": "v1"},
			expected: true,
		},
		{
			name:     "multiple selectors partial match",
			selector: "app=web,env=staging",
			labels:   map[string]string{"app": "web", "env": "production"},
			expected: false,
		},
		{
			name:     "key existence",
			selector: "version",
			labels:   map[string]string{"app": "web", "version": "v1"},
			expected: true,
		},
		{
			name:     "key non-existence",
			selector: "missing",
			labels:   map[string]string{"app": "web"},
			expected: false,
		},
		{
			name:     "empty labels",
			selector: "app=web",
			labels:   nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parser.testLabelSelector(tt.selector, tt.labels)

			if result != tt.expected {
				t.Errorf("Expected label selector result %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		pattern  string
		value    string
		expected bool
	}{
		// Exact matches
		{"default", "default", true},
		{"kube-system", "kube-system", true},
		{"app", "application", false},

		// Wildcard patterns
		{"kube-*", "kube-system", true},
		{"kube-*", "kube-public", true},
		{"kube-*", "default", false},
		{"*-system", "kube-system", true},
		{"*-system", "custom-system", true},
		{"*-system", "system", false},
		{"app-*-prod", "app-web-prod", true},
		{"app-*-prod", "app-db-staging", false},

		// Edge cases
		{"*", "anything", true},
		{"", "", true},
		{"*test*", "my-test-app", true},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s_%s", tt.pattern, tt.value), func(t *testing.T) {
			result := matchPattern(tt.pattern, tt.value)

			if result != tt.expected {
				t.Errorf("Pattern '%s' matching '%s': expected %v, got %v", tt.pattern, tt.value, tt.expected, result)
			}
		})
	}
}

// Benchmark pattern parsing performance
func BenchmarkPatternParser_ConvertToResourceFilterRules(b *testing.B) {
	parser := NewPatternParser()
	
	// Setup complex patterns
	parser.ParseExclusionFlag("secrets,ns:kube-*,label:app=test,gvr:apps/v1/deployments")
	parser.ParseInclusionFlag("pods,services,ns:production,label:env=prod")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rules := parser.ConvertToResourceFilterRules()
		if len(rules) == 0 {
			b.Fatalf("Should generate resource filter rules")
		}
	}
}
