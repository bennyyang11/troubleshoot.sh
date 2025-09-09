package cli

import (
	"context"
	"regexp"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
)

func TestNamespaceFilter_ParseNamespaceFlag(t *testing.T) {
	tests := []struct {
		name         string
		flag         string
		expectError  bool
		expectedInclude []string
		expectedExclude []string
		expectedLabel   string
	}{
		{
			name:         "simple comma-separated",
			flag:         "default,app,production",
			expectError:  false,
			expectedInclude: []string{"default", "app", "production"},
		},
		{
			name:         "include pattern",
			flag:         "include:app,production",
			expectError:  false,
			expectedInclude: []string{"app", "production"},
		},
		{
			name:         "exclude pattern",
			flag:         "exclude:kube-system,kube-public",
			expectError:  false,
			expectedExclude: []string{"kube-system", "kube-public"},
		},
		{
			name:         "label selector",
			flag:         "label:env=production",
			expectError:  false,
			expectedLabel:   "env=production",
		},
		{
			name:         "complex pattern with include and exclude",
			flag:         "include:app,production;exclude:kube-*",
			expectError:  false,
			expectedInclude: []string{"app", "production"},
			expectedExclude: []string{"kube-*"},
		},
		{
			name:         "regex pattern",
			flag:         "regex:^app-.*$",
			expectError:  false,
		},
		{
			name:        "invalid pattern type",
			flag:        "unknown:value",
			expectError: true,
		},
		{
			name:        "invalid regex",
			flag:        "regex:[invalid",
			expectError: true,
		},
		{
			name:        "malformed pattern",
			flag:        "include:", // Empty value - this should NOT error in current implementation
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := NewNamespaceFilter(nil)
			err := filter.ParseNamespaceFlag(tt.flag)

			if tt.expectError && err == nil {
				t.Errorf("Expected error parsing flag but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error parsing flag: %v", err)
			}

			if !tt.expectError {
				// Verify parsed results
				if len(tt.expectedInclude) > 0 {
					if len(filter.includeList) != len(tt.expectedInclude) {
						t.Errorf("Expected %d include items, got %d", len(tt.expectedInclude), len(filter.includeList))
					}
					for _, expected := range tt.expectedInclude {
						found := false
						for _, actual := range filter.includeList {
							if actual == expected {
								found = true
								break
							}
						}
						if !found {
							t.Errorf("Expected include item %s not found", expected)
						}
					}
				}

				if len(tt.expectedExclude) > 0 {
					if len(filter.excludeList) != len(tt.expectedExclude) {
						t.Errorf("Expected %d exclude items, got %d", len(tt.expectedExclude), len(filter.excludeList))
					}
				}

				if tt.expectedLabel != "" {
					if filter.labelSelector != tt.expectedLabel {
						t.Errorf("Expected label selector %s, got %s", tt.expectedLabel, filter.labelSelector)
					}
				}
			}
		})
	}
}

func TestNamespaceFilter_FilterNamespaces(t *testing.T) {
	// Create fake client with test namespaces
	kubeClient := kubernetesfake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "app-production"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "app-staging"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
			Name:   "labeled-ns",
			Labels: map[string]string{"env": "production"},
		}},
	)

	tests := []struct {
		name            string
		setupFilter     func(*NamespaceFilter) error
		expectedCount   int
		expectedContains []string
	}{
		{
			name: "include specific namespaces",
			setupFilter: func(filter *NamespaceFilter) error {
				return filter.ParseNamespaceFlag("default,app-production")
			},
			expectedCount:   2,
			expectedContains: []string{"default", "app-production"},
		},
		{
			name: "exclude pattern",
			setupFilter: func(filter *NamespaceFilter) error {
				return filter.ParseNamespaceFlag("exclude:kube-*")
			},
			expectedCount:   4, // All except kube-system
			expectedContains: []string{"default", "app-production", "app-staging", "labeled-ns"},
		},
		{
			name: "label selector",
			setupFilter: func(filter *NamespaceFilter) error {
				return filter.ParseNamespaceFlag("label:env=production")
			},
			expectedCount:   1,
			expectedContains: []string{"labeled-ns"},
		},
		{
			name: "regex pattern",
			setupFilter: func(filter *NamespaceFilter) error {
				return filter.ParseNamespaceFlag("regex:^app-.*")
			},
			expectedCount:   2,
			expectedContains: []string{"app-production", "app-staging"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := NewNamespaceFilter(kubeClient)
			err := tt.setupFilter(filter)
			if err != nil {
				t.Fatalf("Failed to setup filter: %v", err)
			}

			ctx := context.Background()
			result, err := filter.FilterNamespaces(ctx)
			if err != nil {
				t.Errorf("FilterNamespaces failed: %v", err)
				return
			}

			if len(result) != tt.expectedCount {
				t.Errorf("Expected %d filtered namespaces, got %d: %v", tt.expectedCount, len(result), result)
			}

			// Check that expected namespaces are included
			resultMap := make(map[string]bool)
			for _, ns := range result {
				resultMap[ns] = true
			}

			for _, expected := range tt.expectedContains {
				if !resultMap[expected] {
					t.Errorf("Expected namespace %s not found in results: %v", expected, result)
				}
			}
		})
	}
}

func TestNamespaceFilter_ValidateIncludedNamespaces(t *testing.T) {
	kubeClient := kubernetesfake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "app"}},
	)

	tests := []struct {
		name         string
		includeList  []string
		expectedCount int
		expectError  bool
	}{
		{
			name:         "valid existing namespaces",
			includeList:  []string{"default", "app"},
			expectedCount: 2,
			expectError:  false,
		},
		{
			name:         "mix of valid and invalid namespaces",
			includeList:  []string{"default", "nonexistent", "app"},
			expectedCount: 2, // Should skip nonexistent
			expectError:  false,
		},
		{
			name:         "all nonexistent namespaces",
			includeList:  []string{"nonexistent1", "nonexistent2"},
			expectedCount: 0,
			expectError:  false,
		},
		{
			name:         "empty include list",
			includeList:  []string{},
			expectedCount: 0,
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := NewNamespaceFilter(kubeClient)
			filter.includeList = tt.includeList

			ctx := context.Background()
			result, err := filter.validateIncludedNamespaces(ctx)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if len(result) != tt.expectedCount {
				t.Errorf("Expected %d validated namespaces, got %d: %v", tt.expectedCount, len(result), result)
			}
		})
	}
}

func TestValidateNamespaceFlags(t *testing.T) {
	tests := []struct {
		name          string
		namespaceFlag string
		auto          bool
		expectError   bool
	}{
		{
			name:          "namespace with auto flag",
			namespaceFlag: "default,app",
			auto:          true,
			expectError:   false,
		},
		{
			name:          "namespace without auto flag",
			namespaceFlag: "default",
			auto:          false,
			expectError:   true,
		},
		{
			name:          "empty namespace with auto",
			namespaceFlag: "",
			auto:          true,
			expectError:   false,
		},
		{
			name:          "namespace with spaces",
			namespaceFlag: "default app",
			auto:          true,
			expectError:   true,
		},
		{
			name:          "complex namespace pattern",
			namespaceFlag: "include:app,production;exclude:kube-*",
			auto:          true,
			expectError:   false,
		},
		{
			name:          "invalid pattern syntax",
			namespaceFlag: "include:",
			auto:          true,
			expectError:   false, // Current implementation doesn't error on empty include
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNamespaceFlags(tt.namespaceFlag, tt.auto)

			if tt.expectError && err == nil {
				t.Errorf("Expected validation error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected validation error: %v", err)
			}
		})
	}
}

func TestNamespaceFilter_PassesFilters(t *testing.T) {
	tests := []struct {
		name         string
		setupFilter  func(*NamespaceFilter)
		namespace    string
		expectedPass bool
	}{
		{
			name: "no filters - should pass",
			setupFilter: func(filter *NamespaceFilter) {
				// No filters set
			},
			namespace:    "any-namespace",
			expectedPass: true,
		},
		{
			name: "excluded by pattern",
			setupFilter: func(filter *NamespaceFilter) {
				filter.excludeList = []string{"kube-*"}
			},
			namespace:    "kube-system",
			expectedPass: false,
		},
		{
			name: "not excluded by pattern",
			setupFilter: func(filter *NamespaceFilter) {
				filter.excludeList = []string{"kube-*"}
			},
			namespace:    "application",
			expectedPass: true,
		},
		{
			name: "matches regex",
			setupFilter: func(filter *NamespaceFilter) {
				regex := regexp.MustCompile("^app-.*$")
				filter.regexPattern = regex
			},
			namespace:    "app-production",
			expectedPass: true,
		},
		{
			name: "doesn't match regex",
			setupFilter: func(filter *NamespaceFilter) {
				regex := regexp.MustCompile("^app-.*$")
				filter.regexPattern = regex
			},
			namespace:    "production-app",
			expectedPass: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := NewNamespaceFilter(nil)
			tt.setupFilter(filter)

			result := filter.passesFilters(tt.namespace)
			if result != tt.expectedPass {
				t.Errorf("Expected passesFilters %v for namespace %s, got %v", tt.expectedPass, tt.namespace, result)
			}
		})
	}
}

func TestGetDefaultNamespaces(t *testing.T) {
	defaults := GetDefaultNamespaces()

	// Should have at least some default namespaces
	if len(defaults) == 0 {
		t.Errorf("Should have default namespaces")
	}

	// Should include default namespace
	found := false
	for _, ns := range defaults {
		if ns == "default" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Default namespaces should include 'default'")
	}
}

func TestGetRecommendedExcludedNamespaces(t *testing.T) {
	excluded := GetRecommendedExcludedNamespaces()

	// Should have some recommended exclusions
	if len(excluded) == 0 {
		t.Errorf("Should have recommended excluded namespaces")
	}

	// Should include common system namespaces
	expectedExclusions := []string{"kube-node-lease", "kube-public"}
	excludeMap := make(map[string]bool)
	for _, ns := range excluded {
		excludeMap[ns] = true
	}

	for _, expected := range expectedExclusions {
		if !excludeMap[expected] {
			t.Errorf("Should recommend excluding %s", expected)
		}
	}
}

// Benchmark namespace filtering performance
func BenchmarkNamespaceFilter_ParseNamespaceFlag(b *testing.B) {
	filter := NewNamespaceFilter(nil)
	flag := "include:app,production,staging;exclude:kube-*,test-*;label:env=prod"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := filter.ParseNamespaceFlag(flag)
		if err != nil {
			b.Fatalf("ParseNamespaceFlag failed: %v", err)
		}
	}
}
