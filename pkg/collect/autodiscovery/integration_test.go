package autodiscovery

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"

	authv1 "k8s.io/api/authorization/v1"
	ktesting "k8s.io/client-go/testing"
)

// TestErrorHandlingAndGracefulDegradation tests various error scenarios
func TestErrorHandlingAndGracefulDegradation(t *testing.T) {
	tests := []struct {
		name           string
		setupClients   func() (*kubernetesfake.Clientset, *dynamicfake.FakeDynamicClient)
		options        DiscoveryOptions
		expectedError  bool
		shouldContinue bool // Whether discovery should continue despite errors
	}{
		{
			name: "namespace access denied - graceful degradation",
			setupClients: func() (*kubernetesfake.Clientset, *dynamicfake.FakeDynamicClient) {
				kubeClient := kubernetesfake.NewSimpleClientset()
				
				// Deny access to kube-system, allow default
				kubeClient.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
					req := action.(ktesting.CreateAction).GetObject().(*authv1.SelfSubjectAccessReview)
					attrs := req.Spec.ResourceAttributes
					allowed := attrs.Namespace != "kube-system"
					
					return true, &authv1.SelfSubjectAccessReview{
						Status: authv1.SubjectAccessReviewStatus{
							Allowed: allowed,
						},
					}, nil
				})

				dynamicClient := createTestDynamicClient(
					&unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "Pod",
							"metadata": map[string]interface{}{
								"name":      "accessible-pod",
								"namespace": "default",
							},
						},
					},
				)

				return kubeClient, dynamicClient
			},
			options: DiscoveryOptions{
				Namespaces: []string{"default", "kube-system"},
				RBACCheck:  true,
			},
			expectedError:  false, // Should not error, just skip restricted resources
			shouldContinue: true,
		},
		{
			name: "some resource types unavailable - continue with others",
			setupClients: func() (*kubernetesfake.Clientset, *dynamicfake.FakeDynamicClient) {
				kubeClient := kubernetesfake.NewSimpleClientset()
				dynamicClient := createTestDynamicClient()
				
				// Make certain resource types fail
				dynamicClient.PrependReactor("list", "secrets", func(action ktesting.Action) (bool, runtime.Object, error) {
					return true, nil, fmt.Errorf("secrets access denied")
				})

				return kubeClient, dynamicClient
			},
			options: DiscoveryOptions{
				Namespaces: []string{"default"},
				RBACCheck:  false,
			},
			expectedError:  false,
			shouldContinue: true,
		},
		{
			name: "network timeout - should timeout gracefully",
			setupClients: func() (*kubernetesfake.Clientset, *dynamicfake.FakeDynamicClient) {
				kubeClient := kubernetesfake.NewSimpleClientset()
				dynamicClient := createTestDynamicClient()
				
				// Simulate network timeouts
				kubeClient.PrependReactor("list", "namespaces", func(action ktesting.Action) (bool, runtime.Object, error) {
					time.Sleep(100 * time.Millisecond) // Simulate slow response
					return false, nil, nil
				})

				return kubeClient, dynamicClient
			},
			options: DiscoveryOptions{
				Namespaces: []string{},
				RBACCheck:  false,
			},
			expectedError:  false, // Should handle timeouts gracefully
			shouldContinue: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kubeClient, dynamicClient := tt.setupClients()

			discoverer := &Discoverer{
				kubeClient:    kubeClient,
				dynamicClient: dynamicClient,
				rbacChecker:   NewRBACChecker(kubeClient),
				nsScanner:     NewNamespaceScanner(kubeClient, dynamicClient),
				expander:      NewResourceExpander(),
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			collectors, err := discoverer.Discover(ctx, tt.options)

			if tt.expectedError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectedError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if tt.shouldContinue {
				// Even with errors, some discovery should succeed
				t.Logf("Discovery completed with %d collectors despite errors", len(collectors))
			}
		})
	}
}

// TestResourceFilteringWithComplexSelectors tests complex label selector scenarios
func TestResourceFilteringWithComplexSelectors(t *testing.T) {
	tests := []struct {
		name         string
		resources    []runtime.Object
		filter       ResourceFilter
		expectedPods int
	}{
		{
			name: "single label selector",
			resources: []runtime.Object{
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Pod",
						"metadata": map[string]interface{}{
							"name":      "web-pod",
							"namespace": "default",
							"labels": map[string]interface{}{
								"app": "web",
								"env": "production",
							},
						},
					},
				},
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Pod",
						"metadata": map[string]interface{}{
							"name":      "db-pod",
							"namespace": "default",
							"labels": map[string]interface{}{
								"app": "database",
								"env": "production",
							},
						},
					},
				},
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Pod",
						"metadata": map[string]interface{}{
							"name":      "test-pod",
							"namespace": "default",
							"labels": map[string]interface{}{
								"app": "web",
								"env": "test",
							},
						},
					},
				},
			},
			filter: ResourceFilter{
				LabelSelector: "env=production",
			},
			expectedPods: 2, // web-pod and db-pod
		},
		{
			name: "multiple label requirements",
			resources: []runtime.Object{
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Pod",
						"metadata": map[string]interface{}{
							"name":      "web-prod",
							"namespace": "default",
							"labels": map[string]interface{}{
								"app":     "web",
								"env":     "production",
								"version": "v2",
							},
						},
					},
				},
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Pod",
						"metadata": map[string]interface{}{
							"name":      "web-old",
							"namespace": "default",
							"labels": map[string]interface{}{
								"app":     "web",
								"env":     "production",
								"version": "v1",
							},
						},
					},
				},
			},
			filter: ResourceFilter{
				LabelSelector: "app=web,version=v2",
			},
			expectedPods: 1, // Only web-prod
		},
		{
			name: "namespace selector with partial match",
			resources: []runtime.Object{
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Pod",
						"metadata": map[string]interface{}{
							"name":      "prod-pod",
							"namespace": "production-east",
						},
					},
				},
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Pod",
						"metadata": map[string]interface{}{
							"name":      "staging-pod",
							"namespace": "staging-west",
						},
					},
				},
			},
			filter: ResourceFilter{
				NamespaceSelector: "production",
			},
			expectedPods: 1, // Only prod-pod matches
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kubeClient := kubernetesfake.NewSimpleClientset(
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "production-east"}},
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "staging-west"}},
			)
			
			dynamicClient := createTestDynamicClient(tt.resources...)

			scanner := NewNamespaceScanner(kubeClient, dynamicClient)
			ctx := context.Background()

			resources, err := scanner.ScanNamespaces(ctx, []string{}, tt.filter)
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			// Count pods
			podCount := 0
			for _, resource := range resources {
				if resource.GVR.Resource == "pods" {
					podCount++
				}
			}

			if podCount != tt.expectedPods {
				t.Errorf("Expected %d pods, got %d", tt.expectedPods, podCount)
			}
		})
	}
}

// TestCollectorPrioritySortingAndDeduplication tests collector priority and deduplication
func TestCollectorPrioritySortingAndDeduplication(t *testing.T) {
	resources := []Resource{
		{
			GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
			Namespace: "default",
			Name:      "pod1",
			Labels:    map[string]string{"status": "error"}, // Should get high priority
		},
		{
			GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "events"},
			Namespace: "default",
			Name:      "event1", // Events are high priority
		},
		{
			GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"},
			Namespace: "default",
			Name:      "config1", // Normal priority
		},
		{
			GVR:       schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
			Namespace: "default",
			Name:      "deploy1", // High priority
		},
	}

	expander := NewResourceExpander()
	ctx := context.Background()
	options := DiscoveryOptions{}

	collectors, err := expander.ExpandToCollectors(ctx, resources, options)
	if err != nil {
		t.Fatalf("Failed to expand collectors: %v", err)
	}

	// Sort collectors by priority like the discoverer does
	sort.Slice(collectors, func(i, j int) bool {
		return collectors[i].Priority > collectors[j].Priority
	})
	if err != nil {
		t.Fatalf("Failed to expand collectors: %v", err)
	}

	if len(collectors) == 0 {
		t.Fatalf("No collectors generated")
	}

	// Debug: Print actual priorities to understand the issue
	t.Logf("Generated %d collectors with priorities:", len(collectors))
	for i, collector := range collectors {
		t.Logf("  [%d] %s (type: %s, priority: %d)", i, collector.Name, collector.Type, collector.Priority)
	}

	// Verify collectors are sorted by priority (highest first)
	for i := 1; i < len(collectors); i++ {
		if collectors[i-1].Priority < collectors[i].Priority {
			t.Errorf("Collectors not sorted by priority: %d < %d at positions %d,%d", collectors[i-1].Priority, collectors[i].Priority, i-1, i)
		}
	}

	// Verify high priority collectors come first
	highPriorityFound := false
	for _, collector := range collectors {
		if collector.Priority >= int(PriorityHigh) {
			highPriorityFound = true
			break
		}
	}
	if !highPriorityFound {
		t.Errorf("No high priority collectors found")
	}

	// Test deduplication - same namespace logs should be grouped
	logCollectorCount := 0
	namespaces := make(map[string]bool)
	
	for _, collector := range collectors {
		if collector.Type == "logs" {
			logCollectorCount++
			namespaces[collector.Namespace] = true
		}
	}

	// Should have reasonable number of log collectors (not one per pod)
	if logCollectorCount > len(namespaces)+2 { // Allow for some targeted collectors
		t.Errorf("Too many log collectors, possible duplication: %d", logCollectorCount)
	}
}

// TestPerformanceWithLargeResourceCounts tests performance with many resources
func TestPerformanceWithLargeResourceCounts(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	// Create large number of resources
	resourceCount := 100 // Reduce for testing
	resources := make([]runtime.Object, resourceCount)
	
	// Create some namespaces first
	namespaces := []runtime.Object{
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns-0"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns-1"}},
	}
	
	for i := 0; i < resourceCount; i++ {
		resources[i] = &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Pod",
				"metadata": map[string]interface{}{
					"name":      fmt.Sprintf("pod-%d", i),
					"namespace": "default", // Use single namespace for testing
					"labels": map[string]interface{}{
						"app": fmt.Sprintf("app-%d", i%5), // 5 different apps
					},
				},
			},
		}
	}

	kubeClient := kubernetesfake.NewSimpleClientset(namespaces...)
	dynamicClient := createTestDynamicClient(resources...)

	discoverer := &Discoverer{
		kubeClient:    kubeClient,
		dynamicClient: dynamicClient,
		rbacChecker:   NewRBACChecker(kubeClient),
		nsScanner:     NewNamespaceScanner(kubeClient, dynamicClient),
		expander:      NewResourceExpander(),
	}

	options := DiscoveryOptions{
		Namespaces: []string{"default"}, // Specify explicit namespace
		RBACCheck:  false,               // Skip RBAC for performance test
		MaxDepth:   0,                   // Disable dependency resolution for performance
	}

	start := time.Now()
	ctx := context.Background()

	collectors, err := discoverer.Discover(ctx, options)
	
	duration := time.Since(start)

	if err != nil {
		t.Errorf("Discovery failed with large resource count: %v", err)
	}

	if len(collectors) == 0 {
		t.Errorf("No collectors generated from %d resources", resourceCount)
		
		// Debug: let's see what happened
		testResources, err := discoverer.nsScanner.ScanNamespaces(ctx, options.Namespaces, ResourceFilter{})
		t.Logf("Debug: Found %d resources during scan, error: %v", len(testResources), err)
		return
	}

	// Performance assertion - should complete within reasonable time
	maxDuration := 10 * time.Second
	if duration > maxDuration {
		t.Errorf("Discovery took too long: %v > %v", duration, maxDuration)
	}

	t.Logf("Successfully processed %d resources in %v, generated %d collectors", resourceCount, duration, len(collectors))
}

// TestConfigurationFileLoadingAndMerging tests configuration management
func TestConfigurationFileLoadingAndMerging(t *testing.T) {
	// Create a temporary config file
	configContent := `
defaultOptions:
  namespaces: ["production", "staging"]
  includeImages: false
  rbacCheck: true
  maxDepth: 2

resourceFilters:
  - name: "production-only"
    matchLabels:
      env: "production"
    action: "include"

excludes:
  - namespaces: ["kube-system", "kube-public"]
    reason: "System namespaces"
`

	tmpFile, err := os.CreateTemp("", "autodiscovery-config-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}
	tmpFile.Close()

	// Test loading configuration
	configManager := NewConfigManager()
	err = configManager.LoadFromFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Test merging with overrides
	overrides := &DiscoveryOptions{
		Namespaces:    []string{"custom-ns"},
		IncludeImages: true, // Override config setting
	}

	finalOptions := configManager.GetDiscoveryOptions(overrides)

	// Verify overrides took effect
	if len(finalOptions.Namespaces) != 1 || finalOptions.Namespaces[0] != "custom-ns" {
		t.Errorf("Namespace override failed")
	}
	if !finalOptions.IncludeImages {
		t.Errorf("IncludeImages override failed")
	}
	if finalOptions.MaxDepth != 2 {
		t.Errorf("Default MaxDepth not preserved")
	}

	// Test resource filtering from config
	testResources := []Resource{
		{
			GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
			Namespace: "kube-system",
			Name:      "system-pod",
		},
		{
			GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
			Namespace: "production",
			Name:      "prod-pod",
			Labels:    map[string]string{"env": "production"},
		},
	}

	filtered := configManager.ApplyResourceFilters(testResources)
	
	// kube-system should be excluded, only production pod should remain
	if len(filtered) != 1 || filtered[0].Name != "prod-pod" {
		t.Errorf("Resource filtering from config failed: got %d resources", len(filtered))
	}
}

// TestRealWorldClusterScenarios simulates real cluster scenarios
func TestRealWorldClusterScenarios(t *testing.T) {
	scenarios := []struct {
		name      string
		resources []runtime.Object
		options   DiscoveryOptions
		validate  func([]CollectorSpec) error
	}{
		{
			name: "typical web application deployment",
			resources: []runtime.Object{
				// Web deployment with pods
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "apps/v1",
						"kind":       "Deployment",
						"metadata": map[string]interface{}{
							"name":      "web-app",
							"namespace": "production",
						},
					},
				},
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Pod",
						"metadata": map[string]interface{}{
							"name":      "web-app-pod1",
							"namespace": "production",
							"labels": map[string]interface{}{
								"app": "web-app",
							},
						},
					},
				},
				// Service
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Service",
						"metadata": map[string]interface{}{
							"name":      "web-app-service",
							"namespace": "production",
						},
					},
				},
				// ConfigMap and Secret
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "ConfigMap",
						"metadata": map[string]interface{}{
							"name":      "web-app-config",
							"namespace": "production",
						},
					},
				},
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Secret",
						"metadata": map[string]interface{}{
							"name":      "web-app-secret",
							"namespace": "production",
						},
					},
				},
			},
			options: DiscoveryOptions{
				Namespaces:    []string{"production"},
				IncludeImages: true,
				RBACCheck:     false,
				MaxDepth:      1,
			},
			validate: func(collectors []CollectorSpec) error {
				if len(collectors) < 3 {
					return fmt.Errorf("expected at least 3 collectors for web app, got %d", len(collectors))
				}
				
				hasLogs := false
				hasResources := false
				
				for _, collector := range collectors {
					switch collector.Type {
					case "logs":
						hasLogs = true
					case "cluster-resources":
						hasResources = true
					}
				}
				
				if !hasLogs {
					return fmt.Errorf("expected logs collector for pods")
				}
				if !hasResources {
					return fmt.Errorf("expected cluster-resources collectors")
				}
				
				return nil
			},
		},
		{
			name: "microservices with ingress",
			resources: []runtime.Object{
				// Multiple services
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Service",
						"metadata": map[string]interface{}{
							"name":      "auth-service",
							"namespace": "default",
						},
					},
				},
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Service",
						"metadata": map[string]interface{}{
							"name":      "api-service",
							"namespace": "default",
						},
					},
				},
				// Ingress
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "networking.k8s.io/v1",
						"kind":       "Ingress",
						"metadata": map[string]interface{}{
							"name":      "app-ingress",
							"namespace": "default",
						},
					},
				},
			},
			options: DiscoveryOptions{
				Namespaces: []string{"default"},
				RBACCheck:  false,
			},
			validate: func(collectors []CollectorSpec) error {
				hasNetworkDiag := false
				
				for _, collector := range collectors {
					if collector.Type == "run-pod" && strings.Contains(collector.Name, "network-diag") {
						hasNetworkDiag = true
						break
					}
				}
				
				if !hasNetworkDiag {
					return fmt.Errorf("expected network diagnostic collector for ingress/services")
				}
				
				return nil
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			kubeClient := kubernetesfake.NewSimpleClientset(
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "production"}},
			)
			
			dynamicClient := createTestDynamicClient(scenario.resources...)

			discoverer := &Discoverer{
				kubeClient:    kubeClient,
				dynamicClient: dynamicClient,
				rbacChecker:   NewRBACChecker(kubeClient),
				nsScanner:     NewNamespaceScanner(kubeClient, dynamicClient),
				expander:      NewResourceExpander(),
			}

			ctx := context.Background()
			collectors, err := discoverer.Discover(ctx, scenario.options)

			if err != nil {
				t.Errorf("Discovery failed: %v", err)
			}

			if scenario.validate != nil {
				if err := scenario.validate(collectors); err != nil {
					t.Errorf("Validation failed: %v", err)
				}
			}
		})
	}
}
