package autodiscovery

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// NamespaceScanner handles namespace-aware resource enumeration
type NamespaceScanner struct {
	kubeClient    kubernetes.Interface
	dynamicClient dynamic.Interface
}

// NewNamespaceScanner creates a new NamespaceScanner instance
func NewNamespaceScanner(kubeClient kubernetes.Interface, dynamicClient dynamic.Interface) *NamespaceScanner {
	return &NamespaceScanner{
		kubeClient:    kubeClient,
		dynamicClient: dynamicClient,
	}
}

// ScanNamespaces scans the specified namespaces for resources matching the filter
func (n *NamespaceScanner) ScanNamespaces(ctx context.Context, namespaces []string, filter ResourceFilter) ([]Resource, error) {
	var allResources []Resource

	// Get the list of supported resource types
	supportedGVRs := n.getSupportedGVRs(filter)

	// If no namespaces specified, scan all accessible namespaces
	if len(namespaces) == 0 {
		discoveredNamespaces, err := n.discoverAccessibleNamespaces(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to discover accessible namespaces: %w", err)
		}
		namespaces = discoveredNamespaces
	}

	// Scan each namespace for resources
	for _, namespace := range namespaces {
		resources, err := n.scanNamespace(ctx, namespace, supportedGVRs, filter)
		if err != nil {
			// Log warning but continue with other namespaces
			fmt.Printf("Warning: failed to scan namespace %s: %v\n", namespace, err)
			continue
		}
		allResources = append(allResources, resources...)
	}

	return allResources, nil
}

// scanNamespace scans a single namespace for the specified resource types
func (n *NamespaceScanner) scanNamespace(ctx context.Context, namespace string, gvrs []schema.GroupVersionResource, filter ResourceFilter) ([]Resource, error) {
	var resources []Resource

	for _, gvr := range gvrs {
		// Skip cluster-scoped resources when scanning specific namespaces
		if n.isClusterScoped(gvr) && namespace != "" {
			continue
		}

		resourceList, err := n.listResources(ctx, gvr, namespace)
		if err != nil {
			// Some resources might not exist or might not be accessible - continue with others
			fmt.Printf("Debug: failed to list %s in namespace %s: %v\n", gvr.Resource, namespace, err)
			continue
		}

		// Convert to our Resource type and apply filtering
		for _, item := range resourceList {
			resource := n.convertToResource(item, gvr)
			if n.matchesFilter(resource, filter) {
				resources = append(resources, resource)
			}
		}
	}

	return resources, nil
}

// listResources lists resources of a specific GVR in a namespace
func (n *NamespaceScanner) listResources(ctx context.Context, gvr schema.GroupVersionResource, namespace string) ([]unstructured.Unstructured, error) {
	resourceClient := n.dynamicClient.Resource(gvr)
	
	var list *unstructured.UnstructuredList
	var err error
	
	if namespace == "" || n.isClusterScoped(gvr) {
		list, err = resourceClient.List(ctx, metav1.ListOptions{})
	} else {
		list, err = resourceClient.Namespace(namespace).List(ctx, metav1.ListOptions{})
	}
	
	if err != nil {
		return nil, fmt.Errorf("failed to list resources: %w", err)
	}

	return list.Items, nil
}

// convertToResource converts an unstructured object to our Resource type
func (n *NamespaceScanner) convertToResource(obj unstructured.Unstructured, gvr schema.GroupVersionResource) Resource {
	return Resource{
		GVR:       gvr,
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
		Labels:    obj.GetLabels(),
		OwnerRefs: obj.GetOwnerReferences(),
	}
}

// matchesFilter checks if a resource matches the provided filter criteria
func (n *NamespaceScanner) matchesFilter(resource Resource, filter ResourceFilter) bool {
	// Check GVR inclusion/exclusion
	if len(filter.IncludeGVRs) > 0 {
		found := false
		for _, gvr := range filter.IncludeGVRs {
			if resource.GVR == gvr {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	for _, gvr := range filter.ExcludeGVRs {
		if resource.GVR == gvr {
			return false
		}
	}

	// Check label selector
	if filter.LabelSelector != "" {
		selector, err := labels.Parse(filter.LabelSelector)
		if err != nil {
			fmt.Printf("Warning: invalid label selector %s: %v\n", filter.LabelSelector, err)
			return true // Don't filter out due to invalid selector
		}

		labelSet := labels.Set(resource.Labels)
		if !selector.Matches(labelSet) {
			return false
		}
	}

	// Check namespace selector (simple string match for now)
	if filter.NamespaceSelector != "" {
		if !strings.Contains(resource.Namespace, filter.NamespaceSelector) {
			return false
		}
	}

	return true
}

// getSupportedGVRs returns the list of GVRs to scan based on the filter
func (n *NamespaceScanner) getSupportedGVRs(filter ResourceFilter) []schema.GroupVersionResource {
	defaultGVRs := []schema.GroupVersionResource{
		// Core resources
		{Group: "", Version: "v1", Resource: "pods"},
		{Group: "", Version: "v1", Resource: "services"},
		{Group: "", Version: "v1", Resource: "configmaps"},
		{Group: "", Version: "v1", Resource: "secrets"},
		{Group: "", Version: "v1", Resource: "persistentvolumeclaims"},
		{Group: "", Version: "v1", Resource: "events"},
		
		// Apps resources
		{Group: "apps", Version: "v1", Resource: "deployments"},
		{Group: "apps", Version: "v1", Resource: "replicasets"},
		{Group: "apps", Version: "v1", Resource: "statefulsets"},
		{Group: "apps", Version: "v1", Resource: "daemonsets"},
		
		// Networking resources
		{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"},
		{Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"},
		
		// Batch resources
		{Group: "batch", Version: "v1", Resource: "jobs"},
		{Group: "batch", Version: "v1", Resource: "cronjobs"},
	}

	// If specific GVRs are included in the filter, use those instead
	if len(filter.IncludeGVRs) > 0 {
		return filter.IncludeGVRs
	}

	return defaultGVRs
}

// discoverAccessibleNamespaces discovers all namespaces the user has access to
func (n *NamespaceScanner) discoverAccessibleNamespaces(ctx context.Context) ([]string, error) {
	namespaceList, err := n.kubeClient.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}

	var namespaces []string
	for _, ns := range namespaceList.Items {
		namespaces = append(namespaces, ns.Name)
	}

	return namespaces, nil
}

// isClusterScoped returns true if the resource is cluster-scoped
func (n *NamespaceScanner) isClusterScoped(gvr schema.GroupVersionResource) bool {
	clusterScopedResources := map[string]bool{
		"nodes":                      true,
		"persistentvolumes":          true,
		"storageclasses":             true,
		"clusterroles":               true,
		"clusterrolebindings":        true,
		"customresourcedefinitions":  true,
		"apiservices":                true,
		"mutatingwebhookconfigurations":   true,
		"validatingwebhookconfigurations": true,
		"priorityclasses":            true,
		"runtimeclasses":             true,
		"podsecuritypolicies":        true,
		"volumeattachments":          true,
		"csidrivers":                 true,
		"csinodes":                   true,
	}

	return clusterScopedResources[gvr.Resource]
}
