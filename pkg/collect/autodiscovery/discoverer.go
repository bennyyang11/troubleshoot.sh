package autodiscovery

import (
	"context"
	"fmt"
	"sort"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Discoverer implements the AutoCollector interface
type Discoverer struct {
	kubeClient    kubernetes.Interface
	dynamicClient dynamic.Interface
	restConfig    *rest.Config
	rbacChecker   *RBACChecker
	nsScanner     *NamespaceScanner
	expander      *ResourceExpander
}

// NewDiscoverer creates a new Discoverer instance
func NewDiscoverer(config *rest.Config) (*Discoverer, error) {
	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	rbacChecker := NewRBACChecker(kubeClient)
	nsScanner := NewNamespaceScanner(kubeClient, dynamicClient)
	expander := NewResourceExpanderWithDependencies(dynamicClient, 3) // Default max depth of 3

	return &Discoverer{
		kubeClient:    kubeClient,
		dynamicClient: dynamicClient,
		restConfig:    config,
		rbacChecker:   rbacChecker,
		nsScanner:     nsScanner,
		expander:      expander,
	}, nil
}

// Discover performs auto-discovery of resources and generates collector specifications
func (d *Discoverer) Discover(ctx context.Context, opts DiscoveryOptions) ([]CollectorSpec, error) {
	// Step 1: Scan for resources in specified namespaces
	resources, err := d.nsScanner.ScanNamespaces(ctx, opts.Namespaces, ResourceFilter{})
	if err != nil {
		return nil, fmt.Errorf("failed to scan namespaces: %w", err)
	}

	// Step 2: Validate RBAC permissions if requested
	if opts.RBACCheck {
		allowedResources, err := d.ValidatePermissions(ctx, resources)
		if err != nil {
			return nil, fmt.Errorf("failed to validate permissions: %w", err)
		}
		resources = allowedResources
	}

	// Step 3: Expand resources into collector specifications
	collectors, err := d.expander.ExpandToCollectors(ctx, resources, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to expand resources to collectors: %w", err)
	}

	// Step 4: Sort collectors by priority
	sort.Slice(collectors, func(i, j int) bool {
		return collectors[i].Priority > collectors[j].Priority
	})

	// Log discovery statistics (in a real implementation, this might be returned or stored)
	// TODO: Add metadata collection and logging

	return collectors, nil
}

// ValidatePermissions checks if the user has permissions to access the specified resources
func (d *Discoverer) ValidatePermissions(ctx context.Context, resources []Resource) ([]Resource, error) {
	return d.rbacChecker.FilterByPermissions(ctx, resources)
}

// getClusterInfo retrieves basic cluster information
func (d *Discoverer) getClusterInfo(ctx context.Context) string {
	version, err := d.kubeClient.Discovery().ServerVersion()
	if err != nil {
		return "unknown"
	}
	return fmt.Sprintf("kubernetes-%s", version.String())
}

// GetSupportedResourceTypes returns the list of resource types that can be auto-discovered
func (d *Discoverer) GetSupportedResourceTypes() []schema.GroupVersionResource {
	return []schema.GroupVersionResource{
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
		
		// Storage resources
		{Group: "storage.k8s.io", Version: "v1", Resource: "storageclasses"},
		
		// Batch resources
		{Group: "batch", Version: "v1", Resource: "jobs"},
		{Group: "batch", Version: "v1", Resource: "cronjobs"},
		
		// Custom resources (these would be discovered dynamically)
		{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"},
	}
}

// DiscoverWithFilter performs discovery with custom resource filtering
func (d *Discoverer) DiscoverWithFilter(ctx context.Context, opts DiscoveryOptions, filter ResourceFilter) ([]CollectorSpec, error) {
	resources, err := d.nsScanner.ScanNamespaces(ctx, opts.Namespaces, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to scan namespaces with filter: %w", err)
	}

	if opts.RBACCheck {
		allowedResources, err := d.ValidatePermissions(ctx, resources)
		if err != nil {
			return nil, fmt.Errorf("failed to validate permissions: %w", err)
		}
		resources = allowedResources
	}

	collectors, err := d.expander.ExpandToCollectors(ctx, resources, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to expand resources to collectors: %w", err)
	}

	sort.Slice(collectors, func(i, j int) bool {
		return collectors[i].Priority > collectors[j].Priority
	})

	return collectors, nil
}

// DiscoverWithImageCollection performs discovery and optionally collects image metadata
func (d *Discoverer) DiscoverWithImageCollection(ctx context.Context, opts DiscoveryOptions, collectImages bool) (*DiscoveryResultWithImages, error) {
	// Perform normal discovery
	collectors, err := d.Discover(ctx, opts)
	if err != nil {
		return nil, err
	}

	result := &DiscoveryResultWithImages{
		Collectors: collectors,
		ImageFacts: make(map[string]interface{}), // Will be filled if image collection is enabled
	}

	// Collect image metadata if requested
	if collectImages && opts.IncludeImages {
		// This integration point would use the image collection system
		fmt.Printf("Image collection would be triggered here for discovered pods\n")
		
		// In a real implementation, this would:
		// 1. Create AutoDiscoveryImageCollector
		// 2. Extract image refs from discovered pod resources
		// 3. Collect image facts using the image collection system
		// 4. Add facts to the result
	}

	return result, nil
}

// DiscoveryResultWithImages extends normal discovery result with image metadata
type DiscoveryResultWithImages struct {
	Collectors []CollectorSpec            `json:"collectors"`
	ImageFacts map[string]interface{}     `json:"imageFacts,omitempty"`
}
