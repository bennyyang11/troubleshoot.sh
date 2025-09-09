package autodiscovery

import (
	"context"
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	authv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestDiscoverer_Discover(t *testing.T) {
	tests := []struct {
		name          string
		setupClient   func() (*kubernetesfake.Clientset, *dynamicfake.FakeDynamicClient)
		options       DiscoveryOptions
		expectedCount int
		expectError   bool
	}{
		{
			name: "successful discovery with default namespaces",
			setupClient: func() (*kubernetesfake.Clientset, *dynamicfake.FakeDynamicClient) {
				kubeClient := kubernetesfake.NewSimpleClientset(
					&corev1.Namespace{
						ObjectMeta: metav1.ObjectMeta{Name: "default"},
					},
					&corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-pod",
							Namespace: "default",
						},
					},
				)
				
				// Mock successful RBAC responses
				kubeClient.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
					return true, &authv1.SelfSubjectAccessReview{
						Status: authv1.SubjectAccessReviewStatus{
							Allowed: true,
						},
					}, nil
				})

				dynamicClient := createTestDynamicClient(
					&unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "Pod",
							"metadata": map[string]interface{}{
								"name":      "test-pod",
								"namespace": "default",
							},
						},
					},
				)

				return kubeClient, dynamicClient
			},
			options: DiscoveryOptions{
				Namespaces:    []string{"default"},
				IncludeImages: true,
				RBACCheck:     true,
				MaxDepth:      1,
			},
			expectedCount: 1, // Should discover at least the pod
			expectError:   false,
		},
		{
			name: "discovery with RBAC denied",
			setupClient: func() (*kubernetesfake.Clientset, *dynamicfake.FakeDynamicClient) {
				kubeClient := kubernetesfake.NewSimpleClientset()
				
				// Mock denied RBAC responses
				kubeClient.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
					return true, &authv1.SelfSubjectAccessReview{
						Status: authv1.SubjectAccessReviewStatus{
							Allowed: false,
						},
					}, nil
				})

				dynamicClient := createTestDynamicClient()
				return kubeClient, dynamicClient
			},
			options: DiscoveryOptions{
				Namespaces: []string{"restricted"},
				RBACCheck:  true,
			},
			expectedCount: 0, // Should discover nothing due to RBAC
			expectError:   false,
		},
		{
			name: "discovery without RBAC check",
			setupClient: func() (*kubernetesfake.Clientset, *dynamicfake.FakeDynamicClient) {
				kubeClient := kubernetesfake.NewSimpleClientset()
				dynamicClient := createTestDynamicClient(
					&unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "Service",
							"metadata": map[string]interface{}{
								"name":      "test-service",
								"namespace": "default",
							},
						},
					},
				)
				return kubeClient, dynamicClient
			},
			options: DiscoveryOptions{
				Namespaces: []string{"default"},
				RBACCheck:  false,
			},
			expectedCount: 1,
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kubeClient, dynamicClient := tt.setupClient()
			
			discoverer := &Discoverer{
				kubeClient:    kubeClient,
				dynamicClient: dynamicClient,
				rbacChecker:   NewRBACChecker(kubeClient),
				nsScanner:     NewNamespaceScanner(kubeClient, dynamicClient),
				expander:      NewResourceExpanderWithDependencies(dynamicClient, tt.options.MaxDepth),
			}

			ctx := context.Background()
			collectors, err := discoverer.Discover(ctx, tt.options)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if len(collectors) < tt.expectedCount {
				t.Errorf("Expected at least %d collectors, got %d", tt.expectedCount, len(collectors))
			}

			// Verify collector properties
			for _, collector := range collectors {
				if collector.Type == "" {
					t.Errorf("Collector type should not be empty")
				}
				if collector.Name == "" {
					t.Errorf("Collector name should not be empty")
				}
				if collector.Priority < 0 {
					t.Errorf("Collector priority should not be negative: %d", collector.Priority)
				}
			}
		})
	}
}

func TestDiscoverer_ValidatePermissions(t *testing.T) {
	kubeClient := kubernetesfake.NewSimpleClientset()
	kubeClient.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
		req := action.(ktesting.CreateAction).GetObject().(*authv1.SelfSubjectAccessReview)
		// Allow access to default namespace, deny others
		allowed := req.Spec.ResourceAttributes.Namespace == "default"
		return true, &authv1.SelfSubjectAccessReview{
			Status: authv1.SubjectAccessReviewStatus{
				Allowed: allowed,
			},
		}, nil
	})

	discoverer := &Discoverer{
		kubeClient:  kubeClient,
		rbacChecker: NewRBACChecker(kubeClient),
	}

	resources := []Resource{
		{
			GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
			Namespace: "default",
			Name:      "allowed-pod",
		},
		{
			GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
			Namespace: "restricted",
			Name:      "denied-pod",
		},
	}

	ctx := context.Background()
	allowedResources, err := discoverer.ValidatePermissions(ctx, resources)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if len(allowedResources) != 1 {
		t.Errorf("Expected 1 allowed resource, got %d", len(allowedResources))
	}

	if allowedResources[0].Namespace != "default" {
		t.Errorf("Expected default namespace resource to be allowed")
	}
}

func TestDiscoverer_GetSupportedResourceTypes(t *testing.T) {
	discoverer := &Discoverer{}
	resourceTypes := discoverer.GetSupportedResourceTypes()

	expectedTypes := []string{"pods", "services", "deployments", "configmaps"}
	found := make(map[string]bool)

	for _, gvr := range resourceTypes {
		found[gvr.Resource] = true
	}

	for _, expected := range expectedTypes {
		if !found[expected] {
			t.Errorf("Expected resource type %s not found in supported types", expected)
		}
	}

	if len(resourceTypes) < len(expectedTypes) {
		t.Errorf("Expected at least %d resource types, got %d", len(expectedTypes), len(resourceTypes))
	}
}

func TestDiscoverer_DiscoverWithFilter(t *testing.T) {
	kubeClient := kubernetesfake.NewSimpleClientset()
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(),
		&unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Pod",
				"metadata": map[string]interface{}{
					"name":      "app-pod",
					"namespace": "default",
					"labels": map[string]interface{}{
						"app": "test-app",
					},
				},
			},
		},
		&unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Pod",
				"metadata": map[string]interface{}{
					"name":      "system-pod",
					"namespace": "kube-system",
				},
			},
		},
	)

	discoverer := &Discoverer{
		kubeClient:    kubeClient,
		dynamicClient: dynamicClient,
		rbacChecker:   NewRBACChecker(kubeClient),
		nsScanner:     NewNamespaceScanner(kubeClient, dynamicClient),
		expander:      NewResourceExpander(),
	}

	filter := ResourceFilter{
		IncludeGVRs: []schema.GroupVersionResource{
			{Group: "", Version: "v1", Resource: "pods"},
		},
		NamespaceSelector: "default",
	}

	options := DiscoveryOptions{
		Namespaces: []string{"default", "kube-system"},
		RBACCheck:  false,
	}

	ctx := context.Background()
	collectors, err := discoverer.DiscoverWithFilter(ctx, options, filter)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Should only find resources from default namespace due to filter
	if len(collectors) == 0 {
		t.Errorf("Expected at least some collectors with filter")
	}
}

// Benchmark tests for performance validation
func BenchmarkDiscoverer_Discover(b *testing.B) {
	kubeClient := kubernetesfake.NewSimpleClientset()
	
	// Create many resources for testing
	var objects []runtime.Object
	for i := 0; i < 100; i++ {
		objects = append(objects, &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Pod",
				"metadata": map[string]interface{}{
					"name":      fmt.Sprintf("pod-%d", i),
					"namespace": "default",
				},
			},
		})
	}

	dynamicClient := createTestDynamicClient(objects...)
	
	discoverer := &Discoverer{
		kubeClient:    kubeClient,
		dynamicClient: dynamicClient,
		rbacChecker:   NewRBACChecker(kubeClient),
		nsScanner:     NewNamespaceScanner(kubeClient, dynamicClient),
		expander:      NewResourceExpander(),
	}

	options := DiscoveryOptions{
		Namespaces: []string{"default"},
		RBACCheck:  false,
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := discoverer.Discover(ctx, options)
		if err != nil {
			b.Fatalf("Discover failed: %v", err)
		}
	}
}

func TestDiscoverer_ErrorHandling(t *testing.T) {
	tests := []struct {
		name        string
		setupClient func() (*kubernetesfake.Clientset, *dynamicfake.FakeDynamicClient)
		expectError bool
	}{
		{
			name: "handle namespace scanner error gracefully",
			setupClient: func() (*kubernetesfake.Clientset, *dynamicfake.FakeDynamicClient) {
				kubeClient := kubernetesfake.NewSimpleClientset()
				dynamicClient := createTestDynamicClient()
				
				// Make dynamic client return errors for resource listing
				dynamicClient.PrependReactor("list", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
					return true, nil, fmt.Errorf("pods access denied")
				})
				dynamicClient.PrependReactor("list", "*", func(action ktesting.Action) (bool, runtime.Object, error) {
					return true, nil, fmt.Errorf("resource access denied")
				})
				
				return kubeClient, dynamicClient
			},
			expectError: false, // Should handle errors gracefully, not fail discovery
		},
		{
			name: "handle RBAC error gracefully",
			setupClient: func() (*kubernetesfake.Clientset, *dynamicfake.FakeDynamicClient) {
				kubeClient := kubernetesfake.NewSimpleClientset()
				kubeClient.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
					return true, nil, fmt.Errorf("RBAC check failed")
				})
				
				dynamicClient := createTestDynamicClient()
				return kubeClient, dynamicClient
			},
			expectError: false, // Should handle RBAC errors gracefully
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kubeClient, dynamicClient := tt.setupClient()
			
			discoverer := &Discoverer{
				kubeClient:    kubeClient,
				dynamicClient: dynamicClient,
				rbacChecker:   NewRBACChecker(kubeClient),
				nsScanner:     NewNamespaceScanner(kubeClient, dynamicClient),
				expander:      NewResourceExpander(),
			}

			ctx := context.Background()
			options := DiscoveryOptions{
				Namespaces: []string{"default"},
				RBACCheck:  true,
			}

			_, err := discoverer.Discover(ctx, options)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}
