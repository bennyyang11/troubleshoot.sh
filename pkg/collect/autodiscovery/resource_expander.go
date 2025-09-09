package autodiscovery

import (
	"context"
	"fmt"
	"strings"
	
	"k8s.io/client-go/dynamic"
)

// ResourceExpander converts discovered resources to collector specifications
type ResourceExpander struct {
	collectorMappings map[string]CollectorMapping
	dependencyResolver *DependencyResolver
}

// CollectorMapping defines how to convert a resource type to collector specs
type CollectorMapping struct {
	CollectorType string
	Priority      int
	ParameterBuilder func(Resource) map[string]interface{}
}

// NewResourceExpander creates a new ResourceExpander with default mappings
func NewResourceExpander() *ResourceExpander {
	expander := &ResourceExpander{
		collectorMappings: make(map[string]CollectorMapping),
	}
	expander.initializeDefaultMappings()
	return expander
}

// NewResourceExpanderWithDependencies creates a ResourceExpander with dependency resolution
func NewResourceExpanderWithDependencies(dynamicClient dynamic.Interface, maxDepth int) *ResourceExpander {
	expander := &ResourceExpander{
		collectorMappings: make(map[string]CollectorMapping),
		dependencyResolver: NewDependencyResolver(dynamicClient, maxDepth),
	}
	expander.initializeDefaultMappings()
	return expander
}

// ExpandToCollectors converts resources to collector specifications
func (r *ResourceExpander) ExpandToCollectors(ctx context.Context, resources []Resource, opts DiscoveryOptions) ([]CollectorSpec, error) {
	// Resolve dependencies if dependency resolver is available
	expandedResources := resources
	if r.dependencyResolver != nil && opts.MaxDepth > 0 {
		var err error
		expandedResources, err = r.dependencyResolver.ResolveDependencies(ctx, resources)
		if err != nil {
			// Log warning but continue with original resources
			fmt.Printf("Warning: failed to resolve dependencies: %v\n", err)
			expandedResources = resources
		}
	}

	var collectors []CollectorSpec
	resourceGroups := r.groupResourcesByType(expandedResources)

	for resourceKey, resourceList := range resourceGroups {
		mapping, exists := r.collectorMappings[resourceKey]
		if !exists {
			// Create a generic cluster-resources collector for unknown types
			mapping = r.getGenericMapping()
		}

		// Generate collectors based on the mapping
		newCollectors := r.generateCollectors(resourceList, mapping, opts)
		collectors = append(collectors, newCollectors...)
	}

	return collectors, nil
}

// groupResourcesByType groups resources by their GVR for efficient collector generation
func (r *ResourceExpander) groupResourcesByType(resources []Resource) map[string][]Resource {
	groups := make(map[string][]Resource)
	
	for _, resource := range resources {
		key := r.getResourceKey(resource)
		groups[key] = append(groups[key], resource)
	}
	
	return groups
}

// getResourceKey creates a unique key for grouping resources
func (r *ResourceExpander) getResourceKey(resource Resource) string {
	return fmt.Sprintf("%s_%s_%s", resource.GVR.Group, resource.GVR.Version, resource.GVR.Resource)
}

// generateCollectors creates collector specs for a group of resources
func (r *ResourceExpander) generateCollectors(resources []Resource, mapping CollectorMapping, opts DiscoveryOptions) []CollectorSpec {
	var collectors []CollectorSpec

	switch mapping.CollectorType {
	case "logs":
		collectors = r.generateLogCollectors(resources, mapping, opts)
	case "cluster-resources":
		collectors = r.generateClusterResourceCollectors(resources, mapping, opts)
		// Also check if we should generate network diagnostics for networking resources
		if r.hasNetworkingResources(resources) {
			networkCollectors := r.generateNetworkDiagnosticCollectors(resources, opts)
			collectors = append(collectors, networkCollectors...)
		}
	case "exec":
		collectors = r.generateExecCollectors(resources, mapping, opts)
	case "copy":
		collectors = r.generateCopyCollectors(resources, mapping, opts)
	case "run-pod":
		collectors = r.generateRunPodCollectors(resources, mapping, opts)
	default:
		// Fallback to cluster-resources collector with network diagnostics
		collectors = r.generateClusterResourceCollectors(resources, mapping, opts)
		if r.hasNetworkingResources(resources) {
			networkCollectors := r.generateNetworkDiagnosticCollectors(resources, opts)
			collectors = append(collectors, networkCollectors...)
		}
	}

	return collectors
}

// generateLogCollectors creates log collectors for pod resources
func (r *ResourceExpander) generateLogCollectors(resources []Resource, mapping CollectorMapping, opts DiscoveryOptions) []CollectorSpec {
	var collectors []CollectorSpec

	// Group by namespace for efficient log collection
	namespaceGroups := make(map[string][]Resource)
	for _, resource := range resources {
		if resource.GVR.Resource == "pods" {
			namespaceGroups[resource.Namespace] = append(namespaceGroups[resource.Namespace], resource)
		}
	}

	for namespace, pods := range namespaceGroups {
		// Create a logs collector for each namespace
		collectorSpec := CollectorSpec{
			Type:      "logs",
			Name:      fmt.Sprintf("auto-logs-%s", namespace),
			Namespace: namespace,
			Priority:  mapping.Priority,
			Parameters: map[string]interface{}{
				"selector":  []string{fmt.Sprintf("namespace=%s", namespace)},
				"namespace": namespace,
				"limits": map[string]interface{}{
					"maxAge": "72h",
					"maxLines": 10000,
				},
			},
		}
		collectors = append(collectors, collectorSpec)

		// If there are specific pods with issues, create targeted collectors
		for _, pod := range pods {
			if r.shouldCreateTargetedLogCollector(pod) {
				targetedSpec := CollectorSpec{
					Type:      "logs",
					Name:      fmt.Sprintf("auto-logs-pod-%s", pod.Name),
					Namespace: namespace,
					Priority:  int(PriorityCritical), // Highest priority for targeted collection
					Parameters: map[string]interface{}{
						"name": pod.Name,
						"namespace": namespace,
						"limits": map[string]interface{}{
							"maxAge": "24h",
							"maxLines": 1000,
						},
					},
				}
				collectors = append(collectors, targetedSpec)
			}
		}
	}

	return collectors
}

// generateClusterResourceCollectors creates cluster-resources collectors
func (r *ResourceExpander) generateClusterResourceCollectors(resources []Resource, mapping CollectorMapping, opts DiscoveryOptions) []CollectorSpec {
	if len(resources) == 0 {
		return nil
	}

	// Group resources by GVR
	gvrGroups := make(map[string][]Resource)
	for _, resource := range resources {
		gvrKey := fmt.Sprintf("%s/%s", resource.GVR.Group, resource.GVR.Resource)
		if resource.GVR.Group == "" {
			gvrKey = resource.GVR.Resource
		}
		gvrGroups[gvrKey] = append(gvrGroups[gvrKey], resource)
	}

	var collectors []CollectorSpec
	for gvrKey, resourceList := range gvrGroups {
		resource := resourceList[0] // Use first resource as template
		
		collectorSpec := CollectorSpec{
			Type:     "cluster-resources",
			Name:     fmt.Sprintf("auto-resources-%s", strings.ReplaceAll(gvrKey, "/", "-")),
			Priority: mapping.Priority,
			Parameters: map[string]interface{}{
				"group":    resource.GVR.Group,
				"version":  resource.GVR.Version,
				"resource": resource.GVR.Resource,
			},
		}

		// Add namespace filtering if resources are namespaced
		if resource.Namespace != "" {
			namespaces := r.getUniqueNamespaces(resourceList)
			if len(namespaces) > 0 {
				collectorSpec.Parameters["namespaces"] = namespaces
			}
		}

		collectors = append(collectors, collectorSpec)
	}

	return collectors
}

// generateExecCollectors creates exec collectors for diagnostic commands
func (r *ResourceExpander) generateExecCollectors(resources []Resource, mapping CollectorMapping, opts DiscoveryOptions) []CollectorSpec {
	var collectors []CollectorSpec

	// Generate exec collectors for pods that might need diagnostic commands
	for _, resource := range resources {
		if resource.GVR.Resource == "pods" && r.shouldCreateExecCollector(resource) {
			collectorSpec := CollectorSpec{
				Type:      "exec",
				Name:      fmt.Sprintf("auto-exec-%s", resource.Name),
				Namespace: resource.Namespace,
				Priority:  mapping.Priority,
				Parameters: map[string]interface{}{
					"name":      resource.Name,
					"namespace": resource.Namespace,
					"container": r.getMainContainer(resource),
					"command":   []string{"ps", "aux"},
					"timeout":   "30s",
				},
			}
			collectors = append(collectors, collectorSpec)
		}
	}

	return collectors
}

// generateCopyCollectors creates copy collectors for important files
func (r *ResourceExpander) generateCopyCollectors(resources []Resource, mapping CollectorMapping, opts DiscoveryOptions) []CollectorSpec {
	var collectors []CollectorSpec

	// Generate copy collectors for pods that might have important config files
	for _, resource := range resources {
		if resource.GVR.Resource == "pods" && r.shouldCreateCopyCollector(resource) {
			collectorSpec := CollectorSpec{
				Type:      "copy",
				Name:      fmt.Sprintf("auto-copy-%s", resource.Name),
				Namespace: resource.Namespace,
				Priority:  mapping.Priority,
				Parameters: map[string]interface{}{
					"name":      resource.Name,
					"namespace": resource.Namespace,
					"container": r.getMainContainer(resource),
					"containerPath": "/etc/",
					"extractArchive": true,
				},
			}
			collectors = append(collectors, collectorSpec)
		}
	}

	return collectors
}

// generateRunPodCollectors creates run-pod collectors for diagnostic pods
func (r *ResourceExpander) generateRunPodCollectors(resources []Resource, mapping CollectorMapping, opts DiscoveryOptions) []CollectorSpec {
	var collectors []CollectorSpec

	// Generate run-pod collectors for network diagnostics in namespaces with networking resources
	namespaces := r.getNamespacesWithNetworkingResources(resources)
	for _, namespace := range namespaces {
		collectorSpec := CollectorSpec{
			Type:      "run-pod",
			Name:      fmt.Sprintf("auto-network-diag-%s", namespace),
			Namespace: namespace,
			Priority:  mapping.Priority,
			Parameters: map[string]interface{}{
				"name":      fmt.Sprintf("network-diagnostic-%s", namespace),
				"namespace": namespace,
				"podSpec": map[string]interface{}{
					"containers": []map[string]interface{}{
						{
							"name":  "diagnostic",
							"image": "nicolaka/netshoot:latest",
							"command": []string{"sh", "-c"},
							"args":    []string{"nslookup kubernetes.default.svc.cluster.local && curl -k https://kubernetes.default.svc.cluster.local"},
						},
					},
					"restartPolicy": "Never",
				},
				"timeout": "60s",
			},
		}
		collectors = append(collectors, collectorSpec)
	}

	return collectors
}

// Helper functions for collector generation decisions

func (r *ResourceExpander) shouldCreateTargetedLogCollector(resource Resource) bool {
	// Check if resource has labels indicating it might be problematic
	if labels := resource.Labels; labels != nil {
		// Look for common labels that might indicate issues
		if app, ok := labels["app"]; ok && strings.Contains(strings.ToLower(app), "failed") {
			return true
		}
		if status, ok := labels["status"]; ok && strings.Contains(strings.ToLower(status), "error") {
			return true
		}
	}
	return false
}

func (r *ResourceExpander) shouldCreateExecCollector(resource Resource) bool {
	// Create exec collectors for certain types of pods
	if labels := resource.Labels; labels != nil {
		if app, ok := labels["app"]; ok {
			// Common applications that benefit from process information
			commonApps := []string{"database", "cache", "queue", "worker"}
			appLower := strings.ToLower(app)
			for _, commonApp := range commonApps {
				if strings.Contains(appLower, commonApp) {
					return true
				}
			}
		}
	}
	return false
}

func (r *ResourceExpander) shouldCreateCopyCollector(resource Resource) bool {
	// Create copy collectors for pods that likely have important config files
	if labels := resource.Labels; labels != nil {
		if app, ok := labels["app"]; ok {
			// Applications that commonly have important config files
			configApps := []string{"nginx", "apache", "database", "redis"}
			appLower := strings.ToLower(app)
			for _, configApp := range configApps {
				if strings.Contains(appLower, configApp) {
					return true
				}
			}
		}
	}
	return false
}

func (r *ResourceExpander) getMainContainer(resource Resource) string {
	// Simple heuristic - return empty string to collect from all containers
	// In a full implementation, this would inspect the pod spec
	return ""
}

func (r *ResourceExpander) getUniqueNamespaces(resources []Resource) []string {
	namespaceSet := make(map[string]bool)
	for _, resource := range resources {
		if resource.Namespace != "" {
			namespaceSet[resource.Namespace] = true
		}
	}
	
	var namespaces []string
	for ns := range namespaceSet {
		namespaces = append(namespaces, ns)
	}
	
	return namespaces
}

func (r *ResourceExpander) getNamespacesWithNetworkingResources(resources []Resource) []string {
	namespaceSet := make(map[string]bool)
	for _, resource := range resources {
		if resource.GVR.Group == "networking.k8s.io" || resource.GVR.Resource == "services" {
			if resource.Namespace != "" {
				namespaceSet[resource.Namespace] = true
			}
		}
	}
	
	var namespaces []string
	for ns := range namespaceSet {
		namespaces = append(namespaces, ns)
	}
	
	return namespaces
}

func (r *ResourceExpander) hasNetworkingResources(resources []Resource) bool {
	for _, resource := range resources {
		if resource.GVR.Group == "networking.k8s.io" || resource.GVR.Resource == "services" {
			return true
		}
	}
	return false
}

func (r *ResourceExpander) generateNetworkDiagnosticCollectors(resources []Resource, opts DiscoveryOptions) []CollectorSpec {
	namespaces := r.getNamespacesWithNetworkingResources(resources)
	var collectors []CollectorSpec
	
	for _, namespace := range namespaces {
		collectorSpec := CollectorSpec{
			Type:      "run-pod",
			Name:      fmt.Sprintf("auto-network-diag-%s", namespace),
			Namespace: namespace,
			Priority:  int(PriorityNormal),
			Parameters: map[string]interface{}{
				"name":      fmt.Sprintf("network-diagnostic-%s", namespace),
				"namespace": namespace,
				"podSpec": map[string]interface{}{
					"containers": []map[string]interface{}{
						{
							"name":  "diagnostic",
							"image": "nicolaka/netshoot:latest",
							"command": []string{"sh", "-c"},
							"args":    []string{"nslookup kubernetes.default.svc.cluster.local && curl -k https://kubernetes.default.svc.cluster.local"},
						},
					},
					"restartPolicy": "Never",
				},
				"timeout": "60s",
			},
		}
		collectors = append(collectors, collectorSpec)
	}
	
	return collectors
}

// initializeDefaultMappings sets up the default resource-to-collector mappings
func (r *ResourceExpander) initializeDefaultMappings() {
	r.collectorMappings = map[string]CollectorMapping{
		"_v1_pods": {
			CollectorType: "logs",
			Priority:      int(PriorityHigh),
		},
		"_v1_services": {
			CollectorType: "cluster-resources",
			Priority:      int(PriorityNormal),
		},
		"_v1_configmaps": {
			CollectorType: "cluster-resources",
			Priority:      int(PriorityNormal),
		},
		"_v1_secrets": {
			CollectorType: "cluster-resources",
			Priority:      int(PriorityNormal),
		},
		"_v1_events": {
			CollectorType: "cluster-resources",
			Priority:      int(PriorityHigh),
		},
		"apps_v1_deployments": {
			CollectorType: "cluster-resources",
			Priority:      int(PriorityHigh),
		},
		"apps_v1_replicasets": {
			CollectorType: "cluster-resources",
			Priority:      int(PriorityNormal),
		},
		"apps_v1_statefulsets": {
			CollectorType: "cluster-resources",
			Priority:      int(PriorityHigh),
		},
		"apps_v1_daemonsets": {
			CollectorType: "cluster-resources",
			Priority:      int(PriorityHigh),
		},
		"networking.k8s.io_v1_ingresses": {
			CollectorType: "cluster-resources",
			Priority:      int(PriorityNormal),
		},
		"batch_v1_jobs": {
			CollectorType: "cluster-resources",
			Priority:      int(PriorityNormal),
		},
	}
}

// getGenericMapping returns a default mapping for unknown resource types
func (r *ResourceExpander) getGenericMapping() CollectorMapping {
	return CollectorMapping{
		CollectorType: "cluster-resources",
		Priority:      int(PriorityLow),
	}
}
