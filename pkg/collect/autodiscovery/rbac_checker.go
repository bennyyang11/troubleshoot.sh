package autodiscovery

import (
	"context"
	"fmt"
	"time"

	authv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
)

// RBACChecker handles permission validation for auto-discovered resources
type RBACChecker struct {
	kubeClient kubernetes.Interface
	cache      *PermissionCache
}

// NewRBACChecker creates a new RBACChecker instance
func NewRBACChecker(client kubernetes.Interface) *RBACChecker {
	return &RBACChecker{
		kubeClient: client,
		cache:      NewPermissionCache(5 * time.Minute), // 5 minute TTL
	}
}

// NewRBACCheckerWithCache creates a new RBACChecker instance with custom cache settings
func NewRBACCheckerWithCache(client kubernetes.Interface, cacheTTL time.Duration) *RBACChecker {
	return &RBACChecker{
		kubeClient: client,
		cache:      NewPermissionCache(cacheTTL),
	}
}

// FilterByPermissions filters the provided resources based on RBAC permissions
func (r *RBACChecker) FilterByPermissions(ctx context.Context, resources []Resource) ([]Resource, error) {
	var allowedResources []Resource

	for _, resource := range resources {
		allowed, err := r.checkResourceAccess(ctx, resource)
		if err != nil {
			// Log warning but continue processing other resources
			fmt.Printf("Warning: failed to check permissions for %s/%s: %v\n", resource.Namespace, resource.Name, err)
			continue
		}

		if allowed {
			allowedResources = append(allowedResources, resource)
		}
	}

	return allowedResources, nil
}

// checkResourceAccess checks if the current user has access to a specific resource
func (r *RBACChecker) checkResourceAccess(ctx context.Context, resource Resource) (bool, error) {
	// Check cache first
	getKey := PermissionKey{
		Namespace: resource.Namespace,
		Verb:      "get",
		GVR:       resource.GVR,
		Name:      resource.Name,
	}
	
	if result, found, err := r.cache.Get(getKey); found {
		if err != nil {
			return false, err
		}
		// If we have "get" permission, that's sufficient for basic access
		if result {
			return true, nil
		}
	}
	
	return r.checkResourceAccessWithCache(ctx, resource)
}

// checkResourceAccessWithCache performs the actual permission check and caches the result
func (r *RBACChecker) checkResourceAccessWithCache(ctx context.Context, resource Resource) (bool, error) {
	// Check for "get" permission on the resource
	getReview := &authv1.SelfSubjectAccessReview{
		Spec: authv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authv1.ResourceAttributes{
				Namespace: resource.Namespace,
				Verb:      "get",
				Group:     resource.GVR.Group,
				Version:   resource.GVR.Version,
				Resource:  resource.GVR.Resource,
				Name:      resource.Name,
			},
		},
	}

	getResult, err := r.kubeClient.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, getReview, metav1.CreateOptions{})
	
	// Cache the result
	getKey := PermissionKey{
		Namespace: resource.Namespace,
		Verb:      "get",
		GVR:       resource.GVR,
		Name:      resource.Name,
	}
	
	if err != nil {
		r.cache.Set(getKey, false, err)
		return false, fmt.Errorf("failed to check get permission: %w", err)
	}
	
	if getResult == nil {
		r.cache.Set(getKey, false, fmt.Errorf("nil response from API"))
		return false, fmt.Errorf("received nil response from RBAC API")
	}
	
	r.cache.Set(getKey, getResult.Status.Allowed, nil)

	if !getResult.Status.Allowed {
		return false, nil
	}

	// For some resources, also check "list" permission
	if r.requiresListPermission(resource.GVR.Resource) {
		listReview := &authv1.SelfSubjectAccessReview{
			Spec: authv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authv1.ResourceAttributes{
					Namespace: resource.Namespace,
					Verb:      "list",
					Group:     resource.GVR.Group,
					Version:   resource.GVR.Version,
					Resource:  resource.GVR.Resource,
				},
			},
		}

		listResult, err := r.kubeClient.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, listReview, metav1.CreateOptions{})
		
		// Cache the list permission result
		listKey := PermissionKey{
			Namespace: resource.Namespace,
			Verb:      "list",
			GVR:       resource.GVR,
			Name:      "",
		}
		
		if err != nil {
			r.cache.Set(listKey, false, err)
			return false, fmt.Errorf("failed to check list permission: %w", err)
		}
		
		r.cache.Set(listKey, listResult.Status.Allowed, nil)
		return listResult.Status.Allowed, nil
	}

	return true, nil
}

// requiresListPermission determines if a resource type requires list permissions for effective collection
func (r *RBACChecker) requiresListPermission(resource string) bool {
	// Resources that typically require list permissions for effective collection
	listRequiredResources := map[string]bool{
		"pods":                       true,
		"events":                     true,
		"configmaps":                 true,
		"secrets":                    false, // Often restricted, check only get
		"persistentvolumeclaims":     true,
		"deployments":                true,
		"replicasets":                true,
		"statefulsets":               true,
		"daemonsets":                 true,
		"jobs":                       true,
		"cronjobs":                   true,
		"services":                   true,
		"ingresses":                  true,
		"networkpolicies":            true,
		"customresourcedefinitions":  true,
	}

	return listRequiredResources[resource]
}

// CheckNamespaceAccess checks if the user has access to a specific namespace
func (r *RBACChecker) CheckNamespaceAccess(ctx context.Context, namespace string) (bool, error) {
	review := &authv1.SelfSubjectAccessReview{
		Spec: authv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authv1.ResourceAttributes{
				Namespace: namespace,
				Verb:      "get",
				Resource:  "namespaces",
			},
		},
	}

	result, err := r.kubeClient.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		return false, fmt.Errorf("failed to check namespace access: %w", err)
	}

	return result.Status.Allowed, nil
}

// GetAccessibleNamespaces returns a list of namespaces the user has access to
func (r *RBACChecker) GetAccessibleNamespaces(ctx context.Context, requestedNamespaces []string) ([]string, error) {
	var accessibleNamespaces []string

	for _, ns := range requestedNamespaces {
		hasAccess, err := r.CheckNamespaceAccess(ctx, ns)
		if err != nil {
			fmt.Printf("Warning: failed to check access for namespace %s: %v\n", ns, err)
			continue
		}

		if hasAccess {
			accessibleNamespaces = append(accessibleNamespaces, ns)
		}
	}

	return accessibleNamespaces, nil
}

// CheckResourceTypeAccess checks if the user has general access to a resource type
func (r *RBACChecker) CheckResourceTypeAccess(ctx context.Context, gvr schema.GroupVersionResource, namespace string) (bool, error) {
	review := &authv1.SelfSubjectAccessReview{
		Spec: authv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authv1.ResourceAttributes{
				Namespace: namespace,
				Verb:      "list",
				Group:     gvr.Group,
				Version:   gvr.Version,
				Resource:  gvr.Resource,
			},
		},
	}

	result, err := r.kubeClient.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		return false, fmt.Errorf("failed to check resource type access: %w", err)
	}

	return result.Status.Allowed, nil
}

// GetCacheStats returns statistics about the permission cache
func (r *RBACChecker) GetCacheStats() CacheStats {
	return r.cache.GetStats()
}

// ClearCache clears all cached permission results
func (r *RBACChecker) ClearCache() {
	r.cache.Clear()
}

// StartCacheCleanup starts the background cache cleanup process
func (r *RBACChecker) StartCacheCleanup(ctx context.Context) {
	r.cache.StartCleanupTimer(ctx, time.Minute) // Cleanup every minute
}
