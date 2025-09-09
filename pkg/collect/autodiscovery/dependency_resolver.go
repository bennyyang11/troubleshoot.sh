package autodiscovery

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// DependencyResolver identifies and resolves resource dependencies
type DependencyResolver struct {
	dynamicClient dynamic.Interface
	maxDepth      int
}

// NewDependencyResolver creates a new DependencyResolver
func NewDependencyResolver(dynamicClient dynamic.Interface, maxDepth int) *DependencyResolver {
	return &DependencyResolver{
		dynamicClient: dynamicClient,
		maxDepth:      maxDepth,
	}
}

// ResolveDependencies finds related resources and returns expanded resource list
func (dr *DependencyResolver) ResolveDependencies(ctx context.Context, resources []Resource) ([]Resource, error) {
	visited := make(map[string]bool)
	result := make([]Resource, len(resources))
	copy(result, resources)
	
	// Mark initial resources as visited
	for _, resource := range resources {
		key := dr.resourceKey(resource)
		visited[key] = true
	}

	// Resolve dependencies up to maxDepth
	for depth := 0; depth < dr.maxDepth; depth++ {
		newResources := []Resource{}
		
		for _, resource := range result {
			dependencies, err := dr.findResourceDependencies(ctx, resource)
			if err != nil {
				// Log warning but continue
				fmt.Printf("Warning: failed to resolve dependencies for %s/%s: %v\n", resource.Namespace, resource.Name, err)
				continue
			}
			
			for _, dep := range dependencies {
				key := dr.resourceKey(dep)
				if !visited[key] {
					visited[key] = true
					newResources = append(newResources, dep)
				}
			}
		}
		
		if len(newResources) == 0 {
			break // No more dependencies found
		}
		
		result = append(result, newResources...)
	}

	return result, nil
}

// findResourceDependencies identifies dependencies for a specific resource
func (dr *DependencyResolver) findResourceDependencies(ctx context.Context, resource Resource) ([]Resource, error) {
	switch resource.GVR.Resource {
	case "pods":
		return dr.resolvePodDependencies(ctx, resource)
	case "deployments":
		return dr.resolveDeploymentDependencies(ctx, resource)
	case "statefulsets":
		return dr.resolveStatefulSetDependencies(ctx, resource)
	case "services":
		return dr.resolveServiceDependencies(ctx, resource)
	case "ingresses":
		return dr.resolveIngressDependencies(ctx, resource)
	default:
		return []Resource{}, nil // No known dependencies
	}
}

// resolvePodDependencies finds ConfigMaps, Secrets, PVCs, and Services referenced by a Pod
func (dr *DependencyResolver) resolvePodDependencies(ctx context.Context, resource Resource) ([]Resource, error) {
	// Get the actual pod object
	podGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	pod, err := dr.dynamicClient.Resource(podGVR).Namespace(resource.Namespace).Get(ctx, resource.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod: %w", err)
	}

	var dependencies []Resource

	// Find ConfigMaps and Secrets from volumes and env vars
	spec, found, err := unstructured.NestedMap(pod.Object, "spec")
	if err != nil || !found {
		return dependencies, nil
	}

	// Check volumes
	if volumes, found, err := unstructured.NestedSlice(spec, "volumes"); err == nil && found {
		for _, vol := range volumes {
			volume := vol.(map[string]interface{})
			
			// ConfigMap volumes
			if cm, found, err := unstructured.NestedMap(volume, "configMap"); err == nil && found {
				if name, found, err := unstructured.NestedString(cm, "name"); err == nil && found {
					dependencies = append(dependencies, Resource{
						GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"},
						Namespace: resource.Namespace,
						Name:      name,
					})
				}
			}
			
			// Secret volumes
			if secret, found, err := unstructured.NestedMap(volume, "secret"); err == nil && found {
				if name, found, err := unstructured.NestedString(secret, "secretName"); err == nil && found {
					dependencies = append(dependencies, Resource{
						GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"},
						Namespace: resource.Namespace,
						Name:      name,
					})
				}
			}
			
			// PVC volumes
			if pvc, found, err := unstructured.NestedMap(volume, "persistentVolumeClaim"); err == nil && found {
				if name, found, err := unstructured.NestedString(pvc, "claimName"); err == nil && found {
					dependencies = append(dependencies, Resource{
						GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "persistentvolumeclaims"},
						Namespace: resource.Namespace,
						Name:      name,
					})
				}
			}
		}
	}

	// Check containers for env references
	if containers, found, err := unstructured.NestedSlice(spec, "containers"); err == nil && found {
		for _, cont := range containers {
			container := cont.(map[string]interface{})
			dependencies = append(dependencies, dr.extractEnvDependencies(container, resource.Namespace)...)
		}
	}

	// Check init containers
	if initContainers, found, err := unstructured.NestedSlice(spec, "initContainers"); err == nil && found {
		for _, cont := range initContainers {
			container := cont.(map[string]interface{})
			dependencies = append(dependencies, dr.extractEnvDependencies(container, resource.Namespace)...)
		}
	}

	// Find associated services (pods with matching labels)
	services, err := dr.findServicesForPod(ctx, resource, pod)
	if err == nil {
		dependencies = append(dependencies, services...)
	}

	return dependencies, nil
}

// extractEnvDependencies extracts ConfigMap and Secret references from container env
func (dr *DependencyResolver) extractEnvDependencies(container map[string]interface{}, namespace string) []Resource {
	var dependencies []Resource

	if env, found, err := unstructured.NestedSlice(container, "env"); err == nil && found {
		for _, e := range env {
			envVar := e.(map[string]interface{})
			
			if valueFrom, found, err := unstructured.NestedMap(envVar, "valueFrom"); err == nil && found {
				// ConfigMap env references
				if cmRef, found, err := unstructured.NestedMap(valueFrom, "configMapKeyRef"); err == nil && found {
					if name, found, err := unstructured.NestedString(cmRef, "name"); err == nil && found {
						dependencies = append(dependencies, Resource{
							GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"},
							Namespace: namespace,
							Name:      name,
						})
					}
				}
				
				// Secret env references
				if secretRef, found, err := unstructured.NestedMap(valueFrom, "secretKeyRef"); err == nil && found {
					if name, found, err := unstructured.NestedString(secretRef, "name"); err == nil && found {
						dependencies = append(dependencies, Resource{
							GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"},
							Namespace: namespace,
							Name:      name,
						})
					}
				}
			}
		}
	}

	// Check envFrom references
	if envFrom, found, err := unstructured.NestedSlice(container, "envFrom"); err == nil && found {
		for _, ef := range envFrom {
			envFromSource := ef.(map[string]interface{})
			
			if cmRef, found, err := unstructured.NestedMap(envFromSource, "configMapRef"); err == nil && found {
				if name, found, err := unstructured.NestedString(cmRef, "name"); err == nil && found {
					dependencies = append(dependencies, Resource{
						GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"},
						Namespace: namespace,
						Name:      name,
					})
				}
			}
			
			if secretRef, found, err := unstructured.NestedMap(envFromSource, "secretRef"); err == nil && found {
				if name, found, err := unstructured.NestedString(secretRef, "name"); err == nil && found {
					dependencies = append(dependencies, Resource{
						GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"},
						Namespace: namespace,
						Name:      name,
					})
				}
			}
		}
	}

	return dependencies
}

// resolveDeploymentDependencies finds pods and other resources managed by a Deployment
func (dr *DependencyResolver) resolveDeploymentDependencies(ctx context.Context, resource Resource) ([]Resource, error) {
	var dependencies []Resource

	// Find ReplicaSets owned by this Deployment
	rsGVR := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}
	rsList, err := dr.dynamicClient.Resource(rsGVR).Namespace(resource.Namespace).List(ctx, metav1.ListOptions{})
	if err == nil {
		for _, rs := range rsList.Items {
			if dr.isOwnedBy(rs, resource) {
				dependencies = append(dependencies, Resource{
					GVR:       rsGVR,
					Namespace: rs.GetNamespace(),
					Name:      rs.GetName(),
				})
				
				// Find pods owned by this ReplicaSet
				podDeps, err := dr.findPodsOwnedBy(ctx, Resource{
					GVR:       rsGVR,
					Namespace: rs.GetNamespace(),
					Name:      rs.GetName(),
				})
				if err == nil {
					dependencies = append(dependencies, podDeps...)
				}
			}
		}
	}

	return dependencies, nil
}

// resolveStatefulSetDependencies finds pods and PVCs managed by a StatefulSet
func (dr *DependencyResolver) resolveStatefulSetDependencies(ctx context.Context, resource Resource) ([]Resource, error) {
	var dependencies []Resource

	// Find pods owned by this StatefulSet
	podDeps, err := dr.findPodsOwnedBy(ctx, resource)
	if err == nil {
		dependencies = append(dependencies, podDeps...)
	}

	// Find PVCs created by StatefulSet
	pvcGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "persistentvolumeclaims"}
	pvcList, err := dr.dynamicClient.Resource(pvcGVR).Namespace(resource.Namespace).List(ctx, metav1.ListOptions{})
	if err == nil {
		for _, pvc := range pvcList.Items {
			// Check if PVC name follows StatefulSet pattern
			pvcName := pvc.GetName()
			if strings.Contains(pvcName, resource.Name+"-") {
				dependencies = append(dependencies, Resource{
					GVR:       pvcGVR,
					Namespace: pvc.GetNamespace(),
					Name:      pvcName,
				})
			}
		}
	}

	return dependencies, nil
}

// resolveServiceDependencies finds endpoints and pods targeted by a Service
func (dr *DependencyResolver) resolveServiceDependencies(ctx context.Context, resource Resource) ([]Resource, error) {
	var dependencies []Resource

	// Find endpoints for this service
	endpointsGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "endpoints"}
	if endpoints, err := dr.dynamicClient.Resource(endpointsGVR).Namespace(resource.Namespace).Get(ctx, resource.Name, metav1.GetOptions{}); err == nil {
		dependencies = append(dependencies, Resource{
			GVR:       endpointsGVR,
			Namespace: endpoints.GetNamespace(),
			Name:      endpoints.GetName(),
		})
	}

	// Get service to find selector
	serviceGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}
	service, err := dr.dynamicClient.Resource(serviceGVR).Namespace(resource.Namespace).Get(ctx, resource.Name, metav1.GetOptions{})
	if err != nil {
		return dependencies, nil
	}

	// Find pods matching the service selector
	if spec, found, err := unstructured.NestedMap(service.Object, "spec"); err == nil && found {
		if selector, found, err := unstructured.NestedStringMap(spec, "selector"); err == nil && found && len(selector) > 0 {
			pods, err := dr.findPodsWithLabels(ctx, resource.Namespace, selector)
			if err == nil {
				dependencies = append(dependencies, pods...)
			}
		}
	}

	return dependencies, nil
}

// resolveIngressDependencies finds services referenced by an Ingress
func (dr *DependencyResolver) resolveIngressDependencies(ctx context.Context, resource Resource) ([]Resource, error) {
	var dependencies []Resource

	ingressGVR := schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}
	ingress, err := dr.dynamicClient.Resource(ingressGVR).Namespace(resource.Namespace).Get(ctx, resource.Name, metav1.GetOptions{})
	if err != nil {
		return dependencies, nil
	}

	// Parse ingress spec for service references
	if spec, found, err := unstructured.NestedMap(ingress.Object, "spec"); err == nil && found {
		serviceNames := dr.extractServiceNamesFromIngressSpec(spec)
		for _, serviceName := range serviceNames {
			dependencies = append(dependencies, Resource{
				GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"},
				Namespace: resource.Namespace,
				Name:      serviceName,
			})
		}
	}

	return dependencies, nil
}

// Helper functions

func (dr *DependencyResolver) resourceKey(resource Resource) string {
	return fmt.Sprintf("%s/%s/%s/%s/%s", resource.GVR.Group, resource.GVR.Version, resource.GVR.Resource, resource.Namespace, resource.Name)
}

func (dr *DependencyResolver) isOwnedBy(child unstructured.Unstructured, parent Resource) bool {
	ownerRefs := child.GetOwnerReferences()
	for _, ref := range ownerRefs {
		if ref.Name == parent.Name && ref.Kind == dr.gvrToKind(parent.GVR) {
			return true
		}
	}
	return false
}

func (dr *DependencyResolver) gvrToKind(gvr schema.GroupVersionResource) string {
	// Simple mapping - in production this would use discovery API
	kindMap := map[string]string{
		"deployments":  "Deployment",
		"replicasets":  "ReplicaSet", 
		"statefulsets": "StatefulSet",
		"daemonsets":   "DaemonSet",
		"pods":         "Pod",
		"services":     "Service",
	}
	return kindMap[gvr.Resource]
}

func (dr *DependencyResolver) findPodsOwnedBy(ctx context.Context, owner Resource) ([]Resource, error) {
	var pods []Resource
	
	podGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	podList, err := dr.dynamicClient.Resource(podGVR).Namespace(owner.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return pods, err
	}

	for _, pod := range podList.Items {
		if dr.isOwnedBy(pod, owner) {
			pods = append(pods, Resource{
				GVR:       podGVR,
				Namespace: pod.GetNamespace(),
				Name:      pod.GetName(),
			})
		}
	}

	return pods, nil
}

func (dr *DependencyResolver) findPodsWithLabels(ctx context.Context, namespace string, labels map[string]string) ([]Resource, error) {
	var pods []Resource
	
	// Build label selector
	var selectorParts []string
	for key, value := range labels {
		selectorParts = append(selectorParts, fmt.Sprintf("%s=%s", key, value))
	}
	labelSelector := strings.Join(selectorParts, ",")

	podGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	podList, err := dr.dynamicClient.Resource(podGVR).Namespace(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return pods, err
	}

	for _, pod := range podList.Items {
		pods = append(pods, Resource{
			GVR:       podGVR,
			Namespace: pod.GetNamespace(),
			Name:      pod.GetName(),
		})
	}

	return pods, nil
}

func (dr *DependencyResolver) findServicesForPod(ctx context.Context, podResource Resource, pod *unstructured.Unstructured) ([]Resource, error) {
	var services []Resource
	
	podLabels := pod.GetLabels()
	if len(podLabels) == 0 {
		return services, nil
	}

	serviceGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}
	serviceList, err := dr.dynamicClient.Resource(serviceGVR).Namespace(podResource.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return services, err
	}

	for _, service := range serviceList.Items {
		if spec, found, err := unstructured.NestedMap(service.Object, "spec"); err == nil && found {
			if selector, found, err := unstructured.NestedStringMap(spec, "selector"); err == nil && found {
				// Check if pod labels match service selector
				matches := true
				for sKey, sValue := range selector {
					if podValue, exists := podLabels[sKey]; !exists || podValue != sValue {
						matches = false
						break
					}
				}
				
				if matches {
					services = append(services, Resource{
						GVR:       serviceGVR,
						Namespace: service.GetNamespace(),
						Name:      service.GetName(),
					})
				}
			}
		}
	}

	return services, nil
}

func (dr *DependencyResolver) extractServiceNamesFromIngressSpec(spec map[string]interface{}) []string {
	var serviceNames []string
	
	// Check default backend
	if defaultBackend, found, err := unstructured.NestedMap(spec, "defaultBackend"); err == nil && found {
		if service, found, err := unstructured.NestedMap(defaultBackend, "service"); err == nil && found {
			if name, found, err := unstructured.NestedString(service, "name"); err == nil && found {
				serviceNames = append(serviceNames, name)
			}
		}
	}
	
	// Check rules
	if rules, found, err := unstructured.NestedSlice(spec, "rules"); err == nil && found {
		for _, rule := range rules {
			ruleMap := rule.(map[string]interface{})
			serviceNames = append(serviceNames, dr.extractServiceNamesFromIngressRule(ruleMap)...)
		}
	}
	
	return serviceNames
}

func (dr *DependencyResolver) extractServiceNamesFromIngressRule(rule map[string]interface{}) []string {
	var serviceNames []string
	
	if http, found, err := unstructured.NestedMap(rule, "http"); err == nil && found {
		if paths, found, err := unstructured.NestedSlice(http, "paths"); err == nil && found {
			for _, path := range paths {
				pathMap := path.(map[string]interface{})
				if backend, found, err := unstructured.NestedMap(pathMap, "backend"); err == nil && found {
					if service, found, err := unstructured.NestedMap(backend, "service"); err == nil && found {
						if name, found, err := unstructured.NestedString(service, "name"); err == nil && found {
							serviceNames = append(serviceNames, name)
						}
					}
				}
			}
		}
	}
	
	return serviceNames
}
