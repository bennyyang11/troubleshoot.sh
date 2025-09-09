package autodiscovery

import (
	"context"
	"fmt"
	"testing"
	"time"

	authv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestRBACChecker_FilterByPermissions(t *testing.T) {
	tests := []struct {
		name            string
		resources       []Resource
		allowedPatterns map[string]bool // namespace/resource -> allowed
		expectedCount   int
	}{
		{
			name: "mixed permissions - some allowed, some denied",
			resources: []Resource{
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
				{
					GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"},
					Namespace: "default",
					Name:      "allowed-service",
				},
			},
			allowedPatterns: map[string]bool{
				"default/pods":     true,
				"restricted/pods":  false,
				"default/services": true,
			},
			expectedCount: 2,
		},
		{
			name: "all permissions denied",
			resources: []Resource{
				{
					GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"},
					Namespace: "kube-system",
					Name:      "secret1",
				},
			},
			allowedPatterns: map[string]bool{
				"kube-system/secrets": false,
			},
			expectedCount: 0,
		},
		{
			name: "all permissions allowed",
			resources: []Resource{
				{
					GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"},
					Namespace: "default",
					Name:      "config1",
				},
				{
					GVR:       schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
					Namespace: "default",
					Name:      "deploy1",
				},
			},
			allowedPatterns: map[string]bool{
				"default/configmaps":  true,
				"default/deployments": true,
			},
			expectedCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kubeClient := kubernetesfake.NewSimpleClientset()
			
			// Mock RBAC responses based on patterns
			kubeClient.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
				req := action.(ktesting.CreateAction).GetObject().(*authv1.SelfSubjectAccessReview)
				attrs := req.Spec.ResourceAttributes
				
				key := fmt.Sprintf("%s/%s", attrs.Namespace, attrs.Resource)
				allowed, exists := tt.allowedPatterns[key]
				if !exists {
					allowed = false // Default deny
				}

				return true, &authv1.SelfSubjectAccessReview{
					Status: authv1.SubjectAccessReviewStatus{
						Allowed: allowed,
					},
				}, nil
			})

			rbacChecker := NewRBACChecker(kubeClient)
			ctx := context.Background()

			allowedResources, err := rbacChecker.FilterByPermissions(ctx, tt.resources)
			
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if len(allowedResources) != tt.expectedCount {
				t.Errorf("Expected %d allowed resources, got %d", tt.expectedCount, len(allowedResources))
			}

			// Verify that only allowed resources are returned
			for _, resource := range allowedResources {
				key := fmt.Sprintf("%s/%s", resource.Namespace, resource.GVR.Resource)
				if allowed, exists := tt.allowedPatterns[key]; !exists || !allowed {
					t.Errorf("Resource %s should not be allowed", key)
				}
			}
		})
	}
}

func TestRBACChecker_CheckResourceAccess(t *testing.T) {
	tests := []struct {
		name         string
		resource     Resource
		allowGet     bool
		allowList    bool
		expectResult bool
		expectError  bool
	}{
		{
			name: "get allowed, list not required",
			resource: Resource{
				GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"},
				Namespace: "default",
				Name:      "secret1",
			},
			allowGet:     true,
			allowList:    false,
			expectResult: true,
			expectError:  false,
		},
		{
			name: "get denied",
			resource: Resource{
				GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
				Namespace: "kube-system",
				Name:      "system-pod",
			},
			allowGet:     false,
			allowList:    true,
			expectResult: false,
			expectError:  false,
		},
		{
			name: "get allowed, list required and allowed",
			resource: Resource{
				GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
				Namespace: "default",
				Name:      "app-pod",
			},
			allowGet:     true,
			allowList:    true,
			expectResult: true,
			expectError:  false,
		},
		{
			name: "get allowed, list required but denied",
			resource: Resource{
				GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "events"},
				Namespace: "default",
				Name:      "event1",
			},
			allowGet:     true,
			allowList:    false,
			expectResult: false,
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kubeClient := kubernetesfake.NewSimpleClientset()

			kubeClient.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
				req := action.(ktesting.CreateAction).GetObject().(*authv1.SelfSubjectAccessReview)
				attrs := req.Spec.ResourceAttributes

				var allowed bool
				switch attrs.Verb {
				case "get":
					allowed = tt.allowGet
				case "list":
					allowed = tt.allowList
				default:
					allowed = false
				}

				return true, &authv1.SelfSubjectAccessReview{
					Status: authv1.SubjectAccessReviewStatus{
						Allowed: allowed,
					},
				}, nil
			})

			rbacChecker := NewRBACChecker(kubeClient)
			ctx := context.Background()

			result, err := rbacChecker.checkResourceAccess(ctx, tt.resource)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if result != tt.expectResult {
				t.Errorf("Expected result %v, got %v", tt.expectResult, result)
			}
		})
	}
}

func TestRBACChecker_PermissionCache(t *testing.T) {
	kubeClient := kubernetesfake.NewSimpleClientset()
	
	callCount := 0
	kubeClient.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
		callCount++
		return true, &authv1.SelfSubjectAccessReview{
			Status: authv1.SubjectAccessReviewStatus{
				Allowed: true,
			},
		}, nil
	})

	// Create RBAC checker with short cache TTL for testing
	rbacChecker := NewRBACCheckerWithCache(kubeClient, time.Millisecond*100)
	ctx := context.Background()

	// Use secrets instead of pods because secrets only need "get" permission, not "list"
	resource := Resource{
		GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"},
		Namespace: "default",
		Name:      "test-secret",
	}

	// First call should hit the API
	result1, err1 := rbacChecker.checkResourceAccess(ctx, resource)
	if err1 != nil {
		t.Fatalf("First call failed: %v", err1)
	}
	if !result1 {
		t.Fatalf("First call should be allowed")
	}
	if callCount != 1 {
		t.Errorf("Expected 1 API call after first check, got %d", callCount)
	}

	// Second call should use cache
	result2, err2 := rbacChecker.checkResourceAccess(ctx, resource)
	if err2 != nil {
		t.Fatalf("Second call failed: %v", err2)
	}
	if !result2 {
		t.Fatalf("Second call should be allowed")
	}
	if callCount != 1 {
		t.Errorf("Expected 1 API call after cached check, got %d", callCount)
	}

	// Wait for cache to expire
	time.Sleep(time.Millisecond * 150)

	// Third call should hit the API again
	result3, err3 := rbacChecker.checkResourceAccess(ctx, resource)
	if err3 != nil {
		t.Fatalf("Third call failed: %v", err3)
	}
	if !result3 {
		t.Fatalf("Third call should be allowed")
	}
	if callCount != 2 {
		t.Errorf("Expected 2 API calls after cache expiry, got %d", callCount)
	}
}

func TestRBACChecker_CheckNamespaceAccess(t *testing.T) {
	kubeClient := kubernetesfake.NewSimpleClientset()
	
	kubeClient.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
		req := action.(ktesting.CreateAction).GetObject().(*authv1.SelfSubjectAccessReview)
		attrs := req.Spec.ResourceAttributes
		
		// Allow access to 'default' namespace only
		allowed := attrs.Namespace == "default"
		
		return true, &authv1.SelfSubjectAccessReview{
			Status: authv1.SubjectAccessReviewStatus{
				Allowed: allowed,
			},
		}, nil
	})

	rbacChecker := NewRBACChecker(kubeClient)
	ctx := context.Background()

	// Test allowed namespace
	allowed, err := rbacChecker.CheckNamespaceAccess(ctx, "default")
	if err != nil {
		t.Errorf("Unexpected error for default namespace: %v", err)
	}
	if !allowed {
		t.Errorf("Expected access to default namespace")
	}

	// Test denied namespace
	denied, err := rbacChecker.CheckNamespaceAccess(ctx, "kube-system")
	if err != nil {
		t.Errorf("Unexpected error for kube-system namespace: %v", err)
	}
	if denied {
		t.Errorf("Expected denial for kube-system namespace")
	}
}

func TestRBACChecker_GetAccessibleNamespaces(t *testing.T) {
	kubeClient := kubernetesfake.NewSimpleClientset()
	
	allowedNamespaces := map[string]bool{
		"default": true,
		"app-ns":  true,
		"kube-system": false,
		"restricted":  false,
	}
	
	kubeClient.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
		req := action.(ktesting.CreateAction).GetObject().(*authv1.SelfSubjectAccessReview)
		attrs := req.Spec.ResourceAttributes
		
		allowed, exists := allowedNamespaces[attrs.Namespace]
		if !exists {
			allowed = false
		}
		
		return true, &authv1.SelfSubjectAccessReview{
			Status: authv1.SubjectAccessReviewStatus{
				Allowed: allowed,
			},
		}, nil
	})

	rbacChecker := NewRBACChecker(kubeClient)
	ctx := context.Background()

	requestedNamespaces := []string{"default", "app-ns", "kube-system", "restricted"}
	accessibleNamespaces, err := rbacChecker.GetAccessibleNamespaces(ctx, requestedNamespaces)
	
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	expectedCount := 2 // default and app-ns
	if len(accessibleNamespaces) != expectedCount {
		t.Errorf("Expected %d accessible namespaces, got %d", expectedCount, len(accessibleNamespaces))
	}

	// Verify correct namespaces are returned
	accessibleMap := make(map[string]bool)
	for _, ns := range accessibleNamespaces {
		accessibleMap[ns] = true
	}

	if !accessibleMap["default"] || !accessibleMap["app-ns"] {
		t.Errorf("Expected default and app-ns to be accessible")
	}
	if accessibleMap["kube-system"] || accessibleMap["restricted"] {
		t.Errorf("kube-system and restricted should not be accessible")
	}
}

func TestRBACChecker_CacheManagement(t *testing.T) {
	kubeClient := kubernetesfake.NewSimpleClientset()
	rbacChecker := NewRBACChecker(kubeClient)

	// Test cache stats
	stats := rbacChecker.GetCacheStats()
	if stats.Size != 0 {
		t.Errorf("Expected empty cache initially, got size %d", stats.Size)
	}

	// Test cache clear
	rbacChecker.ClearCache()
	stats = rbacChecker.GetCacheStats()
	if stats.Size != 0 {
		t.Errorf("Expected empty cache after clear, got size %d", stats.Size)
	}

	// Test cache cleanup (basic functionality)
	ctx, cancel := context.WithCancel(context.Background())
	rbacChecker.StartCacheCleanup(ctx)
	
	// Cancel to stop cleanup goroutine
	cancel()
	
	// This is mainly to ensure no panic occurs
}

func TestRBACChecker_ErrorHandling(t *testing.T) {
	tests := []struct {
		name        string
		setupClient func() *kubernetesfake.Clientset
		expectError bool
	}{
		{
			name: "API server error",
			setupClient: func() *kubernetesfake.Clientset {
				client := kubernetesfake.NewSimpleClientset()
				client.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
					return true, nil, fmt.Errorf("API server unavailable")
				})
				return client
			},
			expectError: true,
		},
		{
			name: "malformed response",
			setupClient: func() *kubernetesfake.Clientset {
				client := kubernetesfake.NewSimpleClientset()
				client.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
					// Return nil response
					return true, nil, nil
				})
				return client
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := tt.setupClient()
			rbacChecker := NewRBACChecker(client)
			
			resource := Resource{
				GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
				Namespace: "default",
				Name:      "test-pod",
			}

			ctx := context.Background()
			_, err := rbacChecker.checkResourceAccess(ctx, resource)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

// Benchmark tests for performance validation
func BenchmarkRBACChecker_FilterByPermissions(b *testing.B) {
	kubeClient := kubernetesfake.NewSimpleClientset()
	kubeClient.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, &authv1.SelfSubjectAccessReview{
			Status: authv1.SubjectAccessReviewStatus{
				Allowed: true,
			},
		}, nil
	})

	rbacChecker := NewRBACChecker(kubeClient)
	
	// Create many resources for testing
	resources := make([]Resource, 100)
	for i := 0; i < 100; i++ {
		resources[i] = Resource{
			GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
			Namespace: "default",
			Name:      fmt.Sprintf("pod-%d", i),
		}
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := rbacChecker.FilterByPermissions(ctx, resources)
		if err != nil {
			b.Fatalf("FilterByPermissions failed: %v", err)
		}
	}
}
