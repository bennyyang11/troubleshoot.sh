package autodiscovery

import (
	"context"
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestResourceExpander_ExpandToCollectors(t *testing.T) {
	tests := []struct {
		name          string
		resources     []Resource
		options       DiscoveryOptions
		expectedCount int
		expectedTypes map[string]int // collector type -> count
	}{
		{
			name: "expand pods to logs collectors",
			resources: []Resource{
				{
					GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
					Namespace: "default",
					Name:      "app-pod",
					Labels:    map[string]string{"app": "web"},
				},
				{
					GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
					Namespace: "default",
					Name:      "db-pod",
					Labels:    map[string]string{"app": "database"},
				},
			},
			options: DiscoveryOptions{
				MaxDepth: 0, // No dependency resolution
			},
			expectedCount: 1, // Pods should be grouped into one logs collector
			expectedTypes: map[string]int{
				"logs": 1,
			},
		},
		{
			name: "expand mixed resources",
			resources: []Resource{
				{
					GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"},
					Namespace: "default",
					Name:      "web-service",
				},
				{
					GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"},
					Namespace: "default",
					Name:      "app-config",
				},
				{
					GVR:       schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
					Namespace: "default",
					Name:      "web-deployment",
				},
			},
			options: DiscoveryOptions{
				MaxDepth: 0,
			},
			expectedCount: 3,
			expectedTypes: map[string]int{
				"cluster-resources": 3,
			},
		},
		{
			name: "expand resources with dependency resolution",
			resources: []Resource{
				{
					GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
					Namespace: "default",
					Name:      "test-pod",
				},
			},
			options: DiscoveryOptions{
				MaxDepth: 2, // Enable dependency resolution
			},
			expectedCount: 1, // At least the original resource
			expectedTypes: map[string]int{
				"logs": 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup resource expander with or without dependency resolution
			var expander *ResourceExpander
			if tt.options.MaxDepth > 0 {
				dynamicClient := createTestDynamicClient()
				expander = NewResourceExpanderWithDependencies(dynamicClient, tt.options.MaxDepth)
			} else {
				expander = NewResourceExpander()
			}

			ctx := context.Background()
			collectors, err := expander.ExpandToCollectors(ctx, tt.resources, tt.options)

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if len(collectors) < tt.expectedCount {
				t.Errorf("Expected at least %d collectors, got %d", tt.expectedCount, len(collectors))
			}

			// Count collector types
			typeCounts := make(map[string]int)
			for _, collector := range collectors {
				typeCounts[collector.Type]++
			}

			for expectedType, expectedCount := range tt.expectedTypes {
				if typeCounts[expectedType] < expectedCount {
					t.Errorf("Expected at least %d %s collectors, got %d", expectedCount, expectedType, typeCounts[expectedType])
				}
			}

			// Validate collector properties
			for _, collector := range collectors {
				if collector.Type == "" {
					t.Errorf("Collector type should not be empty")
				}
				if collector.Name == "" {
					t.Errorf("Collector name should not be empty")
				}
				if collector.Priority < 0 {
					t.Errorf("Collector priority should not be negative")
				}
				if collector.Parameters == nil {
					t.Errorf("Collector parameters should not be nil")
				}
			}
		})
	}
}

func TestResourceExpander_generateLogCollectors(t *testing.T) {
	expander := NewResourceExpander()
	mapping := CollectorMapping{
		CollectorType: "logs",
		Priority:      int(PriorityHigh),
	}

	tests := []struct {
		name          string
		resources     []Resource
		expectedCount int
	}{
		{
			name: "single namespace pods",
			resources: []Resource{
				{
					GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
					Namespace: "default",
					Name:      "pod1",
				},
				{
					GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
					Namespace: "default",
					Name:      "pod2",
				},
			},
			expectedCount: 1, // One namespace-level log collector
		},
		{
			name: "multiple namespace pods",
			resources: []Resource{
				{
					GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
					Namespace: "default",
					Name:      "pod1",
				},
				{
					GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
					Namespace: "app-ns",
					Name:      "pod2",
				},
			},
			expectedCount: 2, // One collector per namespace
		},
		{
			name: "pods with error indicators",
			resources: []Resource{
				{
					GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
					Namespace: "default",
					Name:      "pod1",
					Labels:    map[string]string{"status": "error"},
				},
			},
			expectedCount: 2, // Namespace collector + targeted collector for error pod
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := DiscoveryOptions{}
			collectors := expander.generateLogCollectors(tt.resources, mapping, options)

			if len(collectors) < tt.expectedCount {
				t.Errorf("Expected at least %d collectors, got %d", tt.expectedCount, len(collectors))
			}

			// Verify all collectors are log type
			for _, collector := range collectors {
				if collector.Type != "logs" {
					t.Errorf("Expected logs collector, got %s", collector.Type)
				}
				if collector.Priority < int(PriorityNormal) {
					t.Errorf("Expected priority >= %d, got %d", int(PriorityNormal), collector.Priority)
				}
			}
		})
	}
}

func TestResourceExpander_generateClusterResourceCollectors(t *testing.T) {
	expander := NewResourceExpander()
	mapping := CollectorMapping{
		CollectorType: "cluster-resources",
		Priority:      int(PriorityNormal),
	}

	tests := []struct {
		name          string
		resources     []Resource
		expectedCount int
	}{
		{
			name: "same GVR resources",
			resources: []Resource{
				{
					GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"},
					Namespace: "default",
					Name:      "service1",
				},
				{
					GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"},
					Namespace: "default",
					Name:      "service2",
				},
			},
			expectedCount: 1, // Should be grouped into one collector
		},
		{
			name: "different GVR resources",
			resources: []Resource{
				{
					GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"},
					Namespace: "default",
					Name:      "service1",
				},
				{
					GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"},
					Namespace: "default",
					Name:      "config1",
				},
			},
			expectedCount: 2, // Different GVRs -> separate collectors
		},
		{
			name: "mixed namespaced and cluster-scoped",
			resources: []Resource{
				{
					GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"},
					Namespace: "default",
					Name:      "service1",
				},
				{
					GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "nodes"},
					Namespace: "", // Cluster-scoped
					Name:      "node1",
				},
			},
			expectedCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := DiscoveryOptions{}
			collectors := expander.generateClusterResourceCollectors(tt.resources, mapping, options)

			if len(collectors) != tt.expectedCount {
				t.Errorf("Expected %d collectors, got %d", tt.expectedCount, len(collectors))
			}

			// Verify all collectors are cluster-resources type
			for _, collector := range collectors {
				if collector.Type != "cluster-resources" {
					t.Errorf("Expected cluster-resources collector, got %s", collector.Type)
				}
				if collector.Parameters["resource"] == "" {
					t.Errorf("Collector should have resource parameter")
				}
			}
		})
	}
}

func TestResourceExpander_generateRunPodCollectors(t *testing.T) {
	expander := NewResourceExpander()
	mapping := CollectorMapping{
		CollectorType: "run-pod",
		Priority:      int(PriorityNormal),
	}

	tests := []struct {
		name          string
		resources     []Resource
		expectedCount int
	}{
		{
			name: "networking resources trigger run-pod collectors",
			resources: []Resource{
				{
					GVR:       schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"},
					Namespace: "default",
					Name:      "web-ingress",
				},
				{
					GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"},
					Namespace: "default",
					Name:      "web-service",
				},
			},
			expectedCount: 1, // One network diagnostic pod per namespace
		},
		{
			name: "multiple namespaces with networking resources",
			resources: []Resource{
				{
					GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"},
					Namespace: "default",
					Name:      "service1",
				},
				{
					GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"},
					Namespace: "app-ns",
					Name:      "service2",
				},
			},
			expectedCount: 2, // One diagnostic pod per namespace
		},
		{
			name: "no networking resources",
			resources: []Resource{
				{
					GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"},
					Namespace: "default",
					Name:      "config1",
				},
			},
			expectedCount: 0, // No network diagnostic pods needed
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := DiscoveryOptions{}
			collectors := expander.generateRunPodCollectors(tt.resources, mapping, options)

			if len(collectors) != tt.expectedCount {
				t.Errorf("Expected %d collectors, got %d", tt.expectedCount, len(collectors))
			}

			// Verify all collectors are run-pod type with correct configuration
			for _, collector := range collectors {
				if collector.Type != "run-pod" {
					t.Errorf("Expected run-pod collector, got %s", collector.Type)
				}
				
				// Check for diagnostic pod configuration
				podSpec, exists := collector.Parameters["podSpec"]
				if !exists {
					t.Errorf("run-pod collector should have podSpec parameter")
				}
				
				podSpecMap, ok := podSpec.(map[string]interface{})
				if !ok {
					t.Errorf("podSpec should be a map")
					continue
				}
				
				containers, exists := podSpecMap["containers"]
				if !exists {
					t.Errorf("podSpec should have containers")
				}
				
				containersList, ok := containers.([]map[string]interface{})
				if !ok || len(containersList) == 0 {
					t.Errorf("containers should be a non-empty slice")
				}
			}
		})
	}
}

func TestResourceExpander_groupResourcesByType(t *testing.T) {
	expander := NewResourceExpander()

	resources := []Resource{
		{
			GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
			Namespace: "default",
			Name:      "pod1",
		},
		{
			GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
			Namespace: "default",
			Name:      "pod2",
		},
		{
			GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"},
			Namespace: "default",
			Name:      "service1",
		},
		{
			GVR:       schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
			Namespace: "default",
			Name:      "deployment1",
		},
	}

	groups := expander.groupResourcesByType(resources)

	expectedGroups := 3 // pods, services, deployments
	if len(groups) != expectedGroups {
		t.Errorf("Expected %d groups, got %d", expectedGroups, len(groups))
	}

	// Check pod group
	podKey := "_v1_pods"
	if podGroup, exists := groups[podKey]; !exists {
		t.Errorf("Expected pod group not found")
	} else if len(podGroup) != 2 {
		t.Errorf("Expected 2 pods in group, got %d", len(podGroup))
	}

	// Check service group
	serviceKey := "_v1_services"
	if serviceGroup, exists := groups[serviceKey]; !exists {
		t.Errorf("Expected service group not found")
	} else if len(serviceGroup) != 1 {
		t.Errorf("Expected 1 service in group, got %d", len(serviceGroup))
	}

	// Check deployment group
	deploymentKey := "apps_v1_deployments"
	if deploymentGroup, exists := groups[deploymentKey]; !exists {
		t.Errorf("Expected deployment group not found")
	} else if len(deploymentGroup) != 1 {
		t.Errorf("Expected 1 deployment in group, got %d", len(deploymentGroup))
	}
}

func TestResourceExpander_Helper_Functions(t *testing.T) {
	expander := NewResourceExpander()

	// Test shouldCreateTargetedLogCollector
	tests := []struct {
		name     string
		resource Resource
		expected bool
	}{
		{
			name: "pod with failed status",
			resource: Resource{
				Name:   "failed-pod",
				Labels: map[string]string{"status": "error"},
			},
			expected: true,
		},
		{
			name: "pod with failed app label",
			resource: Resource{
				Name:   "app-pod", 
				Labels: map[string]string{"app": "failed-service"},
			},
			expected: true,
		},
		{
			name: "normal pod",
			resource: Resource{
				Name:   "normal-pod",
				Labels: map[string]string{"app": "web", "version": "v1"},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := expander.shouldCreateTargetedLogCollector(tt.resource)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}

	// Test shouldCreateExecCollector
	execTests := []struct {
		name     string
		resource Resource
		expected bool
	}{
		{
			name: "database pod",
			resource: Resource{
				Name:   "db-pod",
				Labels: map[string]string{"app": "database"},
			},
			expected: true,
		},
		{
			name: "cache pod",
			resource: Resource{
				Name:   "redis-pod",
				Labels: map[string]string{"app": "cache"},
			},
			expected: true,
		},
		{
			name: "web pod",
			resource: Resource{
				Name:   "web-pod",
				Labels: map[string]string{"app": "frontend"},
			},
			expected: false,
		},
	}

	for _, tt := range execTests {
		t.Run("exec_"+tt.name, func(t *testing.T) {
			result := expander.shouldCreateExecCollector(tt.resource)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestResourceExpander_initializeDefaultMappings(t *testing.T) {
	expander := NewResourceExpander()

	// Check that default mappings are properly initialized
	expectedMappings := map[string]string{
		"_v1_pods":                        "logs",
		"_v1_services":                    "cluster-resources",
		"_v1_events":                      "cluster-resources",
		"apps_v1_deployments":             "cluster-resources",
		"apps_v1_statefulsets":           "cluster-resources",
		"networking.k8s.io_v1_ingresses": "cluster-resources",
	}

	for key, expectedType := range expectedMappings {
		if mapping, exists := expander.collectorMappings[key]; !exists {
			t.Errorf("Expected mapping for %s not found", key)
		} else if mapping.CollectorType != expectedType {
			t.Errorf("Expected collector type %s for %s, got %s", expectedType, key, mapping.CollectorType)
		}
	}

	// Verify high priority resources
	highPriorityResources := []string{"_v1_pods", "_v1_events", "apps_v1_deployments", "apps_v1_statefulsets"}
	for _, resource := range highPriorityResources {
		if mapping, exists := expander.collectorMappings[resource]; exists {
			if mapping.Priority < int(PriorityHigh) {
				t.Errorf("Expected high priority for %s, got %d", resource, mapping.Priority)
			}
		}
	}
}

// Benchmark tests
func BenchmarkResourceExpander_ExpandToCollectors(b *testing.B) {
	expander := NewResourceExpander()
	
	// Create many resources for testing
	resources := make([]Resource, 1000)
	for i := 0; i < 1000; i++ {
		resources[i] = Resource{
			GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
			Namespace: "default",
			Name:      fmt.Sprintf("pod-%d", i),
		}
	}

	options := DiscoveryOptions{MaxDepth: 0}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := expander.ExpandToCollectors(ctx, resources, options)
		if err != nil {
			b.Fatalf("ExpandToCollectors failed: %v", err)
		}
	}
}
