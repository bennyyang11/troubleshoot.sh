package images

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// AutoDiscoveryImageCollector integrates image collection with auto-discovery
type AutoDiscoveryImageCollector struct {
	registryClient   RegistryClient
	factsBuilder     FactsBuilder
	factsSerializer  *FactsSerializer
	errorHandler     *ErrorHandler
	dynamicClient    dynamic.Interface
	progressReporter ProgressReporter
}

// NewAutoDiscoveryImageCollector creates a new auto-discovery image collector
func NewAutoDiscoveryImageCollector(dynamicClient dynamic.Interface) *AutoDiscoveryImageCollector {
	registryClient := NewRegistryClient(30 * time.Second)
	digestResolver := NewDigestResolver(registryClient, 1*time.Hour)
	factsBuilder := NewFactsBuilder(registryClient, digestResolver)
	factsSerializer := NewFactsSerializer(true) // Pretty print by default
	errorHandler := NewErrorHandler(3, 2*time.Second, FallbackBestEffort)

	return &AutoDiscoveryImageCollector{
		registryClient:  registryClient,
		factsBuilder:    factsBuilder,
		factsSerializer: factsSerializer,
		errorHandler:    errorHandler,
		dynamicClient:   dynamicClient,
	}
}

// SetProgressReporter sets the progress reporter for image operations
func (adic *AutoDiscoveryImageCollector) SetProgressReporter(reporter ProgressReporter) {
	adic.progressReporter = reporter
	adic.factsBuilder.SetProgressReporter(reporter)
}

// SetRegistryCredentials configures registry authentication
func (adic *AutoDiscoveryImageCollector) SetRegistryCredentials(registryCredentials map[string]*RegistryCredentials) {
	if defaultClient, ok := adic.registryClient.(*DefaultRegistryClient); ok {
		for registry, creds := range registryCredentials {
			defaultClient.SetCredentials(registry, creds)
		}
	}
}

// CollectImageFactsFromPods discovers pods and collects image facts
func (adic *AutoDiscoveryImageCollector) CollectImageFactsFromPods(ctx context.Context, namespaces []string, options ImageCollectionOptions) (*ImageCollectionResult, error) {
	// Discover pods in the specified namespaces
	pods, err := adic.discoverPods(ctx, namespaces)
	if err != nil {
		return nil, fmt.Errorf("failed to discover pods: %w", err)
	}

	// Extract unique image references from all pods
	imageRefs := adic.extractImageRefsFromPods(pods)

	if adic.progressReporter != nil {
		adic.progressReporter.Start(len(imageRefs))
	}

	// Collect facts for all unique images
	resilientCollector := NewResilientImageCollector(adic.registryClient, adic.errorHandler, 1*time.Hour)
	result, err := resilientCollector.CollectImageFacts(ctx, imageRefs, options)
	if err != nil {
		return nil, fmt.Errorf("failed to collect image facts: %w", err)
	}

	if adic.progressReporter != nil {
		adic.progressReporter.Complete(result)
	}

	return result, nil
}

// CollectImageFactsFromResources collects image facts from discovered Kubernetes resources
func (adic *AutoDiscoveryImageCollector) CollectImageFactsFromResources(ctx context.Context, resources []AutoDiscoveryResource, options ImageCollectionOptions) (*ImageCollectionResult, error) {
	var allImageRefs []string

	// Extract image references from different resource types
	for _, resource := range resources {
		imageRefs, err := adic.extractImageRefsFromResource(ctx, resource)
		if err != nil {
			fmt.Printf("Warning: failed to extract images from %s/%s: %v\n", resource.Namespace, resource.Name, err)
			continue
		}
		allImageRefs = append(allImageRefs, imageRefs...)
	}

	// Deduplicate image references
	uniqueImageRefs := adic.deduplicateImageRefs(allImageRefs)

	if adic.progressReporter != nil {
		adic.progressReporter.Start(len(uniqueImageRefs))
	}

	// Collect facts
	resilientCollector := NewResilientImageCollector(adic.registryClient, adic.errorHandler, 1*time.Hour)
	result, err := resilientCollector.CollectImageFacts(ctx, uniqueImageRefs, options)
	if err != nil {
		return nil, fmt.Errorf("failed to collect image facts: %w", err)
	}

	if adic.progressReporter != nil {
		adic.progressReporter.Complete(result)
	}

	return result, nil
}

// GenerateFactsJSON generates the facts.json output
func (adic *AutoDiscoveryImageCollector) GenerateFactsJSON(facts map[string]*ImageFacts) ([]byte, error) {
	return adic.factsSerializer.SerializeToJSON(facts)
}

// SaveFactsToFile saves image facts to a file
func (adic *AutoDiscoveryImageCollector) SaveFactsToFile(facts map[string]*ImageFacts, filePath string) error {
	return adic.factsSerializer.SerializeToFile(facts, filePath)
}

// Helper methods

func (adic *AutoDiscoveryImageCollector) discoverPods(ctx context.Context, namespaces []string) ([]unstructured.Unstructured, error) {
	var allPods []unstructured.Unstructured

	podGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}

	for _, namespace := range namespaces {
		podList, err := adic.dynamicClient.Resource(podGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			fmt.Printf("Warning: failed to list pods in namespace %s: %v\n", namespace, err)
			continue
		}

		allPods = append(allPods, podList.Items...)
	}

	return allPods, nil
}

func (adic *AutoDiscoveryImageCollector) extractImageRefsFromPods(pods []unstructured.Unstructured) []string {
	var imageRefs []string

	for _, pod := range pods {
		// Extract from pod spec
		if spec, found, err := unstructured.NestedMap(pod.Object, "spec"); err == nil && found {
			refs := adic.factsBuilder.(*DefaultFactsBuilder).extractImageRefsFromPodSpec(spec)
			imageRefs = append(imageRefs, refs...)
		}
	}

	return adic.deduplicateImageRefs(imageRefs)
}

func (adic *AutoDiscoveryImageCollector) extractImageRefsFromResource(ctx context.Context, resource AutoDiscoveryResource) ([]string, error) {
	switch resource.GVR.Resource {
	case "pods":
		return adic.extractImageRefsFromPod(ctx, resource)
	case "deployments":
		return adic.extractImageRefsFromDeployment(ctx, resource)
	case "statefulsets":
		return adic.extractImageRefsFromStatefulSet(ctx, resource)
	case "daemonsets":
		return adic.extractImageRefsFromDaemonSet(ctx, resource)
	case "jobs":
		return adic.extractImageRefsFromJob(ctx, resource)
	case "cronjobs":
		return adic.extractImageRefsFromCronJob(ctx, resource)
	default:
		return []string{}, nil // No images for other resource types
	}
}

func (adic *AutoDiscoveryImageCollector) extractImageRefsFromPod(ctx context.Context, resource AutoDiscoveryResource) ([]string, error) {
	podGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	pod, err := adic.dynamicClient.Resource(podGVR).Namespace(resource.Namespace).Get(ctx, resource.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	if spec, found, err := unstructured.NestedMap(pod.Object, "spec"); err == nil && found {
		return adic.factsBuilder.(*DefaultFactsBuilder).extractImageRefsFromPodSpec(spec), nil
	}

	return []string{}, nil
}

func (adic *AutoDiscoveryImageCollector) extractImageRefsFromDeployment(ctx context.Context, resource AutoDiscoveryResource) ([]string, error) {
	deploymentGVR := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	deployment, err := adic.dynamicClient.Resource(deploymentGVR).Namespace(resource.Namespace).Get(ctx, resource.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	// Extract from deployment spec template
	if spec, found, err := unstructured.NestedMap(deployment.Object, "spec", "template", "spec"); err == nil && found {
		return adic.factsBuilder.(*DefaultFactsBuilder).extractImageRefsFromPodSpec(spec), nil
	}

	return []string{}, nil
}

func (adic *AutoDiscoveryImageCollector) extractImageRefsFromStatefulSet(ctx context.Context, resource AutoDiscoveryResource) ([]string, error) {
	statefulSetGVR := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}
	statefulSet, err := adic.dynamicClient.Resource(statefulSetGVR).Namespace(resource.Namespace).Get(ctx, resource.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	// Extract from statefulset spec template
	if spec, found, err := unstructured.NestedMap(statefulSet.Object, "spec", "template", "spec"); err == nil && found {
		return adic.factsBuilder.(*DefaultFactsBuilder).extractImageRefsFromPodSpec(spec), nil
	}

	return []string{}, nil
}

func (adic *AutoDiscoveryImageCollector) extractImageRefsFromDaemonSet(ctx context.Context, resource AutoDiscoveryResource) ([]string, error) {
	daemonSetGVR := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}
	daemonSet, err := adic.dynamicClient.Resource(daemonSetGVR).Namespace(resource.Namespace).Get(ctx, resource.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	// Extract from daemonset spec template
	if spec, found, err := unstructured.NestedMap(daemonSet.Object, "spec", "template", "spec"); err == nil && found {
		return adic.factsBuilder.(*DefaultFactsBuilder).extractImageRefsFromPodSpec(spec), nil
	}

	return []string{}, nil
}

func (adic *AutoDiscoveryImageCollector) extractImageRefsFromJob(ctx context.Context, resource AutoDiscoveryResource) ([]string, error) {
	jobGVR := schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}
	job, err := adic.dynamicClient.Resource(jobGVR).Namespace(resource.Namespace).Get(ctx, resource.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	// Extract from job spec template
	if spec, found, err := unstructured.NestedMap(job.Object, "spec", "template", "spec"); err == nil && found {
		return adic.factsBuilder.(*DefaultFactsBuilder).extractImageRefsFromPodSpec(spec), nil
	}

	return []string{}, nil
}

func (adic *AutoDiscoveryImageCollector) extractImageRefsFromCronJob(ctx context.Context, resource AutoDiscoveryResource) ([]string, error) {
	cronJobGVR := schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}
	cronJob, err := adic.dynamicClient.Resource(cronJobGVR).Namespace(resource.Namespace).Get(ctx, resource.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	// Extract from cronjob spec jobTemplate template
	if spec, found, err := unstructured.NestedMap(cronJob.Object, "spec", "jobTemplate", "spec", "template", "spec"); err == nil && found {
		return adic.factsBuilder.(*DefaultFactsBuilder).extractImageRefsFromPodSpec(spec), nil
	}

	return []string{}, nil
}

func (adic *AutoDiscoveryImageCollector) deduplicateImageRefs(imageRefs []string) []string {
	seen := make(map[string]bool)
	var unique []string

	for _, imageRef := range imageRefs {
		// Normalize the reference for deduplication
		normalized, err := adic.factsBuilder.NormalizeImageReference(imageRef)
		if err != nil {
			// Use original reference if normalization fails
			normalized = imageRef
		}

		if !seen[normalized] {
			seen[normalized] = true
			unique = append(unique, imageRef) // Use original reference
		}
	}

	return unique
}

// AutoDiscoveryResource represents a resource discovered by the auto-discovery system
// This should match the Resource type from the autodiscovery package
type AutoDiscoveryResource struct {
	GVR       schema.GroupVersionResource `json:"gvr"`
	Namespace string                      `json:"namespace"`
	Name      string                      `json:"name"`
	Labels    map[string]string           `json:"labels,omitempty"`
}
