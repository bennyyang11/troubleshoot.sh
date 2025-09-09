package autodiscovery

import (
	"context"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestNamespaceScanner_ScanNamespaces(t *testing.T) {
	tests := []struct {
		name           string
		namespaces     []string
		filter         ResourceFilter
		setupResources func() []runtime.Object
		expectedCount  int
		expectError    bool
	}{
		{
			name:       "scan specific namespaces with pods",
			namespaces: []string{"default", "app-ns"},
			filter:     ResourceFilter{},
			setupResources: func() []runtime.Object {
				return []runtime.Object{
					&unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "Pod",
							"metadata": map[string]interface{}{
								"name":      "pod1",
								"namespace": "default",
								"labels": map[string]interface{}{
									"app": "test",
								},
							},
						},
					},
					&unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "Pod",
							"metadata": map[string]interface{}{
								"name":      "pod2",
								"namespace": "app-ns",
								"labels": map[string]interface{}{
									"app": "production",
								},
							},
						},
					},
					&unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "Pod",
							"metadata": map[string]interface{}{
								"name":      "pod3",
								"namespace": "other-ns", // Should be filtered out
							},
						},
					},
				}
			},
			expectedCount: 2, // Only pods from default and app-ns
			expectError:   false,
		},
		{
			name:       "scan with GVR filter",
			namespaces: []string{"default"},
			filter: ResourceFilter{
				IncludeGVRs: []schema.GroupVersionResource{
					{Group: "", Version: "v1", Resource: "services"},
				},
			},
			setupResources: func() []runtime.Object {
				return []runtime.Object{
					&unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "Service",
							"metadata": map[string]interface{}{
								"name":      "service1",
								"namespace": "default",
							},
						},
					},
					&unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "Pod",
							"metadata": map[string]interface{}{
								"name":      "pod1",
								"namespace": "default",
							},
						},
					},
				}
			},
			expectedCount: 1, // Only service should match filter
			expectError:   false,
		},
		{
			name:       "scan with label selector filter",
			namespaces: []string{"default"},
			filter: ResourceFilter{
				LabelSelector: "app=production",
			},
			setupResources: func() []runtime.Object {
				return []runtime.Object{
					&unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "Pod",
							"metadata": map[string]interface{}{
								"name":      "prod-pod",
								"namespace": "default",
								"labels": map[string]interface{}{
									"app": "production",
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
									"app": "test",
								},
							},
						},
					},
				}
			},
			expectedCount: 1, // Only production pod should match
			expectError:   false,
		},
		{
			name:       "auto-discover all namespaces",
			namespaces: []string{}, // Empty means auto-discover
			filter:     ResourceFilter{},
			setupResources: func() []runtime.Object {
				return []runtime.Object{
					&unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "Pod",
							"metadata": map[string]interface{}{
								"name":      "auto-pod",
								"namespace": "discovered-ns",
							},
						},
					},
				}
			},
			expectedCount: 1,
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup fake clients
			kubeClient := kubernetesfake.NewSimpleClientset(
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "app-ns"}},
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "other-ns"}},
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "discovered-ns"}},
			)

			resources := tt.setupResources()
			dynamicClient := createTestDynamicClient(resources...)

			scanner := NewNamespaceScanner(kubeClient, dynamicClient)
			ctx := context.Background()

			results, err := scanner.ScanNamespaces(ctx, tt.namespaces, tt.filter)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if len(results) != tt.expectedCount {
				t.Errorf("Expected %d resources, got %d", tt.expectedCount, len(results))
			}

			// Validate resource structure
			for _, resource := range results {
				if resource.Name == "" {
					t.Errorf("Resource name should not be empty")
				}
				if resource.Namespace == "" && !scanner.isClusterScoped(resource.GVR) {
					t.Errorf("Namespaced resource should have namespace")
				}
				if resource.GVR.Resource == "" {
					t.Errorf("Resource GVR should not be empty")
				}
			}
		})
	}
}

func TestNamespaceScanner_scanNamespace(t *testing.T) {
	tests := []struct {
		name           string
		namespace      string
		gvrs           []schema.GroupVersionResource
		setupResources func() []runtime.Object
		expectedCount  int
		expectError    bool
	}{
		{
			name:      "scan single namespace for pods",
			namespace: "default",
			gvrs: []schema.GroupVersionResource{
				{Group: "", Version: "v1", Resource: "pods"},
			},
			setupResources: func() []runtime.Object {
				return []runtime.Object{
					&unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "Pod",
							"metadata": map[string]interface{}{
								"name":      "pod1",
								"namespace": "default",
							},
						},
					},
					&unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "Pod",
							"metadata": map[string]interface{}{
								"name":      "pod2",
								"namespace": "other", // Different namespace
							},
						},
					},
				}
			},
			expectedCount: 1, // Only pod from default namespace
			expectError:   false,
		},
		{
			name:      "scan namespace for multiple resource types",
			namespace: "default",
			gvrs: []schema.GroupVersionResource{
				{Group: "", Version: "v1", Resource: "pods"},
				{Group: "", Version: "v1", Resource: "services"},
			},
			setupResources: func() []runtime.Object {
				return []runtime.Object{
					&unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "Pod",
							"metadata": map[string]interface{}{
								"name":      "pod1",
								"namespace": "default",
							},
						},
					},
					&unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "Service",
							"metadata": map[string]interface{}{
								"name":      "service1",
								"namespace": "default",
							},
						},
					},
				}
			},
			expectedCount: 2,
			expectError:   false,
		},
		{
			name:      "handle cluster-scoped resources",
			namespace: "", // Empty for cluster-scoped
			gvrs: []schema.GroupVersionResource{
				{Group: "", Version: "v1", Resource: "nodes"},
			},
			setupResources: func() []runtime.Object {
				return []runtime.Object{
					&unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "Node",
							"metadata": map[string]interface{}{
								"name": "node1",
							},
						},
					},
				}
			},
			expectedCount: 1,
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kubeClient := kubernetesfake.NewSimpleClientset()
			resources := tt.setupResources()
			dynamicClient := createTestDynamicClient(resources...)

			scanner := NewNamespaceScanner(kubeClient, dynamicClient)
			ctx := context.Background()

			results, err := scanner.scanNamespace(ctx, tt.namespace, tt.gvrs, ResourceFilter{})

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if len(results) != tt.expectedCount {
				t.Errorf("Expected %d resources, got %d", tt.expectedCount, len(results))
			}
		})
	}
}

func TestNamespaceScanner_matchesFilter(t *testing.T) {
	tests := []struct {
		name     string
		resource Resource
		filter   ResourceFilter
		expected bool
	}{
		{
			name: "no filter - should match",
			resource: Resource{
				GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
				Namespace: "default",
				Name:      "test-pod",
				Labels:    map[string]string{"app": "test"},
			},
			filter:   ResourceFilter{},
			expected: true,
		},
		{
			name: "GVR include filter - match",
			resource: Resource{
				GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
				Namespace: "default",
				Name:      "test-pod",
			},
			filter: ResourceFilter{
				IncludeGVRs: []schema.GroupVersionResource{
					{Group: "", Version: "v1", Resource: "pods"},
				},
			},
			expected: true,
		},
		{
			name: "GVR include filter - no match",
			resource: Resource{
				GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
				Namespace: "default",
				Name:      "test-pod",
			},
			filter: ResourceFilter{
				IncludeGVRs: []schema.GroupVersionResource{
					{Group: "", Version: "v1", Resource: "services"},
				},
			},
			expected: false,
		},
		{
			name: "GVR exclude filter - excluded",
			resource: Resource{
				GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"},
				Namespace: "default",
				Name:      "test-secret",
			},
			filter: ResourceFilter{
				ExcludeGVRs: []schema.GroupVersionResource{
					{Group: "", Version: "v1", Resource: "secrets"},
				},
			},
			expected: false,
		},
		{
			name: "label selector - match",
			resource: Resource{
				GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
				Namespace: "default",
				Name:      "test-pod",
				Labels:    map[string]string{"app": "production", "version": "v1"},
			},
			filter: ResourceFilter{
				LabelSelector: "app=production",
			},
			expected: true,
		},
		{
			name: "label selector - no match",
			resource: Resource{
				GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
				Namespace: "default",
				Name:      "test-pod",
				Labels:    map[string]string{"app": "test"},
			},
			filter: ResourceFilter{
				LabelSelector: "app=production",
			},
			expected: false,
		},
		{
			name: "namespace selector - match",
			resource: Resource{
				GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
				Namespace: "production-ns",
				Name:      "test-pod",
			},
			filter: ResourceFilter{
				NamespaceSelector: "production",
			},
			expected: true,
		},
		{
			name: "namespace selector - no match",
			resource: Resource{
				GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
				Namespace: "test-ns",
				Name:      "test-pod",
			},
			filter: ResourceFilter{
				NamespaceSelector: "production",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scanner := &NamespaceScanner{}
			result := scanner.matchesFilter(tt.resource, tt.filter)

			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestNamespaceScanner_getSupportedGVRs(t *testing.T) {
	scanner := &NamespaceScanner{}

	tests := []struct {
		name           string
		filter         ResourceFilter
		expectedLength int
		expectedTypes  []string
	}{
		{
			name:           "default GVRs",
			filter:         ResourceFilter{},
			expectedLength: 14, // Should return default resource types
			expectedTypes:  []string{"pods", "services", "deployments", "configmaps"},
		},
		{
			name: "filtered GVRs",
			filter: ResourceFilter{
				IncludeGVRs: []schema.GroupVersionResource{
					{Group: "", Version: "v1", Resource: "pods"},
					{Group: "", Version: "v1", Resource: "services"},
				},
			},
			expectedLength: 2,
			expectedTypes:  []string{"pods", "services"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gvrs := scanner.getSupportedGVRs(tt.filter)

			if len(gvrs) != tt.expectedLength {
				t.Errorf("Expected %d GVRs, got %d", tt.expectedLength, len(gvrs))
			}

			// Check for expected resource types
			found := make(map[string]bool)
			for _, gvr := range gvrs {
				found[gvr.Resource] = true
			}

			for _, expectedType := range tt.expectedTypes {
				if !found[expectedType] {
					t.Errorf("Expected resource type %s not found", expectedType)
				}
			}
		})
	}
}

func TestNamespaceScanner_isClusterScoped(t *testing.T) {
	scanner := &NamespaceScanner{}

	tests := []struct {
		name     string
		gvr      schema.GroupVersionResource
		expected bool
	}{
		{
			name:     "pods are namespace-scoped",
			gvr:      schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
			expected: false,
		},
		{
			name:     "nodes are cluster-scoped",
			gvr:      schema.GroupVersionResource{Group: "", Version: "v1", Resource: "nodes"},
			expected: true,
		},
		{
			name:     "persistentvolumes are cluster-scoped",
			gvr:      schema.GroupVersionResource{Group: "", Version: "v1", Resource: "persistentvolumes"},
			expected: true,
		},
		{
			name:     "services are namespace-scoped",
			gvr:      schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"},
			expected: false,
		},
		{
			name:     "storageclasses are cluster-scoped",
			gvr:      schema.GroupVersionResource{Group: "storage.k8s.io", Version: "v1", Resource: "storageclasses"},
			expected: true,
		},
		{
			name:     "unknown resource defaults to namespace-scoped",
			gvr:      schema.GroupVersionResource{Group: "custom", Version: "v1", Resource: "customresources"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scanner.isClusterScoped(tt.gvr)
			if result != tt.expected {
				t.Errorf("Expected %v for %s, got %v", tt.expected, tt.gvr.Resource, result)
			}
		})
	}
}

func TestNamespaceScanner_discoverAccessibleNamespaces(t *testing.T) {
	tests := []struct {
		name           string
		setupClient    func() *kubernetesfake.Clientset
		expectedCount  int
		expectedNames  []string
		expectError    bool
	}{
		{
			name: "discover multiple namespaces",
			setupClient: func() *kubernetesfake.Clientset {
				return kubernetesfake.NewSimpleClientset(
					&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
					&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system"}},
					&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "app-ns"}},
				)
			},
			expectedCount: 3,
			expectedNames: []string{"default", "kube-system", "app-ns"},
			expectError:   false,
		},
		{
			name: "no namespaces",
			setupClient: func() *kubernetesfake.Clientset {
				return kubernetesfake.NewSimpleClientset()
			},
			expectedCount: 0,
			expectedNames: []string{},
			expectError:   false,
		},
		{
			name: "API error",
			setupClient: func() *kubernetesfake.Clientset {
				client := kubernetesfake.NewSimpleClientset()
				client.PrependReactor("list", "namespaces", func(action ktesting.Action) (bool, runtime.Object, error) {
					return true, nil, fmt.Errorf("API server error")
				})
				return client
			},
			expectedCount: 0,
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := tt.setupClient()
			scanner := NewNamespaceScanner(client, nil)

			ctx := context.Background()
			namespaces, err := scanner.discoverAccessibleNamespaces(ctx)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if len(namespaces) != tt.expectedCount {
				t.Errorf("Expected %d namespaces, got %d", tt.expectedCount, len(namespaces))
			}

			// Check for expected namespace names
			namespaceMap := make(map[string]bool)
			for _, ns := range namespaces {
				namespaceMap[ns] = true
			}

			for _, expectedName := range tt.expectedNames {
				if !namespaceMap[expectedName] {
					t.Errorf("Expected namespace %s not found", expectedName)
				}
			}
		})
	}
}

// Benchmark tests for performance validation
func BenchmarkNamespaceScanner_ScanNamespaces(b *testing.B) {
	// Setup with many resources
	resources := make([]runtime.Object, 1000)
	for i := 0; i < 1000; i++ {
		resources[i] = &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Pod",
				"metadata": map[string]interface{}{
					"name":      fmt.Sprintf("pod-%d", i),
					"namespace": "default",
				},
			},
		}
	}

	kubeClient := kubernetesfake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
	)
	dynamicClient := createTestDynamicClient(resources...)

	scanner := NewNamespaceScanner(kubeClient, dynamicClient)
	ctx := context.Background()
	namespaces := []string{"default"}
	filter := ResourceFilter{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := scanner.ScanNamespaces(ctx, namespaces, filter)
		if err != nil {
			b.Fatalf("ScanNamespaces failed: %v", err)
		}
	}
}
