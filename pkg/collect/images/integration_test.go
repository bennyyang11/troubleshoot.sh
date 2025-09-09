package images

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	corev1 "k8s.io/api/core/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
)

func TestAutoDiscoveryImageCollector_CollectImageFactsFromPods(t *testing.T) {
	// Create test pods with various image configurations
	pods := []runtime.Object{
		&unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Pod",
				"metadata": map[string]interface{}{
					"name":      "web-pod",
					"namespace": "default",
				},
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{
							"name":  "web",
							"image": "nginx:1.21",
						},
						map[string]interface{}{
							"name":  "sidecar",
							"image": "busybox:latest",
						},
					},
				},
			},
		},
		&unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Pod",
				"metadata": map[string]interface{}{
					"name":      "db-pod",
					"namespace": "default",
				},
				"spec": map[string]interface{}{
					"initContainers": []interface{}{
						map[string]interface{}{
							"name":  "init-db",
							"image": "postgres:13",
						},
					},
					"containers": []interface{}{
						map[string]interface{}{
							"name":  "database",
							"image": "postgres:13",
						},
					},
				},
			},
		},
	}

	// Create test dynamic client
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme, pods...)

	// Create collector with mock registry client
	collector := NewAutoDiscoveryImageCollector(dynamicClient)

	// Set mock registry client
	mockClient := &MockRegistryClient{
		digests: map[string]string{
			"nginx:1.21":    "sha256:nginx121",
			"busybox:latest": "sha256:busybox456",
			"postgres:13":   "sha256:postgres13",
		},
	}
	collector.registryClient = mockClient

	ctx := context.Background()
	options := ImageCollectionOptions{
		IncludeManifests: true,
		IncludeLayers:    true,
		CacheEnabled:    false,
		Timeout:         30 * time.Second,
	}

	result, err := collector.CollectImageFactsFromPods(ctx, []string{"default"}, options)
	if err != nil {
		t.Fatalf("CollectImageFactsFromPods failed: %v", err)
	}

	// Verify results
	expectedImages := 3 // nginx:1.21, busybox:latest, postgres:13 (deduplicated)
	if result.Statistics.TotalImages != expectedImages {
		t.Errorf("Expected %d images, got %d", expectedImages, result.Statistics.TotalImages)
	}

	if result.Statistics.SuccessfulImages != expectedImages {
		t.Errorf("Expected %d successful images, got %d", expectedImages, result.Statistics.SuccessfulImages)
	}

	// Verify specific image facts
	for imageRef, expectedDigest := range mockClient.digests {
		if facts, exists := result.Facts[imageRef]; !exists {
			t.Errorf("Facts not found for image %s", imageRef)
		} else {
			if facts.Digest != expectedDigest {
				t.Errorf("Expected digest %s for %s, got %s", expectedDigest, imageRef, facts.Digest)
			}
		}
	}
}

func TestAutoDiscoveryImageCollector_CollectImageFactsFromResources(t *testing.T) {
	// Create test resources
	resources := []AutoDiscoveryResource{
		{
			GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
			Namespace: "default",
			Name:      "web-pod",
		},
		{
			GVR:       schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
			Namespace: "default",
			Name:      "web-deployment",
		},
		{
			GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"},
			Namespace: "default",
			Name:      "web-service", // Services don't have images
		},
	}

	// Create corresponding Kubernetes objects
	objects := []runtime.Object{
		&unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Pod",
				"metadata": map[string]interface{}{
					"name":      "web-pod",
					"namespace": "default",
				},
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{
							"name":  "web",
							"image": "nginx:latest",
						},
					},
				},
			},
		},
		&unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"metadata": map[string]interface{}{
					"name":      "web-deployment",
					"namespace": "default",
				},
				"spec": map[string]interface{}{
					"template": map[string]interface{}{
						"spec": map[string]interface{}{
							"containers": []interface{}{
								map[string]interface{}{
									"name":  "web",
									"image": "nginx:1.20",
								},
							},
						},
					},
				},
			},
		},
	}

	// Create test clients
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	appsv1.AddToScheme(scheme)
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme, objects...)

	collector := NewAutoDiscoveryImageCollector(dynamicClient)

	// Set mock registry client
	mockClient := &MockRegistryClient{
		digests: map[string]string{
			"nginx:latest": "sha256:nginx123",
			"nginx:1.20":   "sha256:nginx120",
		},
	}
	collector.registryClient = mockClient

	ctx := context.Background()
	options := ImageCollectionOptions{
		CacheEnabled: true,
	}

	result, err := collector.CollectImageFactsFromResources(ctx, resources, options)
	if err != nil {
		t.Fatalf("CollectImageFactsFromResources failed: %v", err)
	}

	// Verify we collected facts from both pod and deployment
	expectedImages := 2 // nginx:latest from pod, nginx:1.20 from deployment
	if result.Statistics.SuccessfulImages != expectedImages {
		t.Errorf("Expected %d successful images, got %d", expectedImages, result.Statistics.SuccessfulImages)
	}

	// Verify specific facts
	if _, exists := result.Facts["nginx:latest"]; !exists {
		t.Errorf("Expected facts for nginx:latest from pod")
	}
	if _, exists := result.Facts["nginx:1.20"]; !exists {
		t.Errorf("Expected facts for nginx:1.20 from deployment")
	}
}

func TestFactsSpecification_ValidateFactsJSON(t *testing.T) {
	tests := []struct {
		name        string
		factsJSON   string
		expectError bool
	}{
		{
			name: "valid complete facts",
			factsJSON: `{
				"version": "v1",
				"timestamp": "2023-01-15T10:30:00Z",
				"facts": {
					"nginx:latest": {
						"repository": "library/nginx",
						"tag": "latest",
						"digest": "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
						"registry": "index.docker.io",
						"size": 142857280,
						"created": "2023-01-15T10:30:00Z",
						"labels": {"version": "1.21.0"},
						"platform": {
							"architecture": "amd64",
							"os": "linux"
						},
						"layers": [
							{
								"digest": "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
								"size": 71428640,
								"mediaType": "application/vnd.docker.image.rootfs.diff.tar.gzip"
							}
						]
					}
				},
				"summary": {
					"totalImages": 1,
					"registries": {"index.docker.io": 1},
					"platforms": {"linux/amd64": 1},
					"totalSize": 142857280
				}
			}`,
			expectError: false,
		},
		{
			name: "minimal valid facts",
			factsJSON: `{
				"version": "v1",
				"timestamp": "2023-01-15T10:30:00Z",
				"facts": {
					"alpine:latest": {
						"repository": "library/alpine",
						"registry": "index.docker.io",
						"platform": {
							"architecture": "amd64",
							"os": "linux"
						}
					}
				},
				"summary": {
					"totalImages": 1,
					"registries": {"index.docker.io": 1},
					"platforms": {"linux/amd64": 1},
					"totalSize": 0
				}
			}`,
			expectError: false,
		},
		{
			name: "invalid version",
			factsJSON: `{
				"version": "v2",
				"facts": {}
			}`,
			expectError: true,
		},
		{
			name: "missing required fields",
			factsJSON: `{
				"version": "v1",
				"facts": {
					"nginx:latest": {
						"tag": "latest"
					}
				}
			}`,
			expectError: true,
		},
		{
			name:        "invalid JSON",
			factsJSON:   `{"invalid": json}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFactsJSON([]byte(tt.factsJSON))

			if tt.expectError && err == nil {
				t.Errorf("Expected validation error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected validation error: %v", err)
			}
		})
	}
}

func TestGetFactsJSONSpecification(t *testing.T) {
	spec := GetFactsJSONSpecification()

	// Verify specification structure
	if spec.Version != "v1" {
		t.Errorf("Expected version v1, got %s", spec.Version)
	}

	if spec.Description == "" {
		t.Errorf("Description should not be empty")
	}

	if spec.Schema == nil {
		t.Errorf("Schema should not be nil")
	}

	if len(spec.Examples) == 0 {
		t.Errorf("Examples should not be empty")
	}

	// Verify schema has required top-level properties
	properties, ok := spec.Schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("Schema should have properties")
	}

	requiredProps := []string{"version", "timestamp", "facts"}
	for _, prop := range requiredProps {
		if _, exists := properties[prop]; !exists {
			t.Errorf("Schema missing required property: %s", prop)
		}
	}

	// Verify definitions exist
	definitions, ok := spec.Schema["definitions"].(map[string]interface{})
	if !ok {
		t.Fatalf("Schema should have definitions")
	}

	requiredDefs := []string{"ImageFacts", "Platform", "LayerInfo", "ImageConfig"}
	for _, def := range requiredDefs {
		if _, exists := definitions[def]; !exists {
			t.Errorf("Schema missing required definition: %s", def)
		}
	}
}

func TestGenerateFactsJSONExample(t *testing.T) {
	example, err := GenerateFactsJSONExample()
	if err != nil {
		t.Fatalf("GenerateFactsJSONExample failed: %v", err)
	}

	// Verify it's valid JSON
	var parsed ImageFactsOutput
	if err := json.Unmarshal(example, &parsed); err != nil {
		t.Errorf("Generated example is invalid JSON: %v", err)
	}

	// Verify example structure
	if parsed.Version != "v1" {
		t.Errorf("Example should have version v1")
	}

	if len(parsed.Facts) == 0 {
		t.Errorf("Example should have facts")
	}

	// Verify example validates against spec
	if err := ValidateFactsJSON(example); err != nil {
		t.Errorf("Generated example doesn't validate against spec: %v", err)
	}
}

func TestBundleImageCollector_Integration(t *testing.T) {
	// Create test dynamic client with resources
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	
	pods := []runtime.Object{
		&unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Pod",
				"metadata": map[string]interface{}{
					"name":      "test-pod",
					"namespace": "default",
				},
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{
							"name":  "app",
							"image": "myapp:v1.0",
						},
					},
				},
			},
		},
	}

	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme, pods...)
	imageCollector := NewAutoDiscoveryImageCollector(dynamicClient)

	// Set mock client
	mockClient := &MockRegistryClient{
		digests: map[string]string{
			"myapp:v1.0": "sha256:myapp123",
		},
	}
	imageCollector.registryClient = mockClient

	// Create temporary directory for test
	tmpDir, err := os.MkdirTemp("", "bundle-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	
	bundleCollector := NewBundleImageCollector(tmpDir, imageCollector)

	// Test collection
	resources := []AutoDiscoveryResource{
		{
			GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
			Namespace: "default",
			Name:      "test-pod",
		},
	}

	ctx := context.Background()
	options := ImageCollectionOptions{
		IncludeManifests: true,
		CacheEnabled:    true,
	}

	result, err := bundleCollector.CollectAndSerialize(ctx, resources, options)
	if err != nil {
		t.Fatalf("Bundle collection failed: %v", err)
	}

	// Verify bundle result
	if result.FactsCount != 1 {
		t.Errorf("Expected 1 facts count, got %d", result.FactsCount)
	}
	if result.ErrorsCount != 0 {
		t.Errorf("Expected 0 errors count, got %d", result.ErrorsCount)
	}
	if result.CollectionTime <= 0 {
		t.Errorf("Expected positive collection time")
	}
}

func TestBundleImageCollector_ErrorHandling(t *testing.T) {
	// Test with failing registry client
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	
	pods := []runtime.Object{
		&unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Pod",
				"metadata": map[string]interface{}{
					"name":      "failing-pod",
					"namespace": "default",
				},
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{
							"name":  "app",
							"image": "inaccessible:latest",
						},
					},
				},
			},
		},
	}

	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme, pods...)
	imageCollector := NewAutoDiscoveryImageCollector(dynamicClient)

	// Set failing mock client
	mockClient := &MockRegistryClient{
		digests: map[string]string{}, // No digests available
	}
	imageCollector.registryClient = mockClient

	// Create temporary directory for test
	tmpDir, err := os.MkdirTemp("", "bundle-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	
	bundleCollector := NewBundleImageCollector(tmpDir, imageCollector)

	ctx := context.Background()
	options := ImageCollectionOptions{
		CacheEnabled: false,
	}

	resources := []AutoDiscoveryResource{
		{
			GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
			Namespace: "default",
			Name:      "failing-pod",
		},
	}

	result, err := bundleCollector.CollectAndSerialize(ctx, resources, options)
	if err != nil {
		t.Fatalf("Bundle collection should handle errors gracefully: %v", err)
	}

	// Verify error handling
	if result.ErrorsCount != 1 {
		t.Errorf("Expected 1 error, got %d", result.ErrorsCount)
	}
	if result.FactsCount != 0 {
		t.Errorf("Expected 0 successful facts, got %d", result.FactsCount)
	}
	if result.ErrorsPath == "" {
		t.Errorf("Expected errors path to be set")
	}
}

func TestAutoDiscoveryImageCollector_ExtractImageRefsFromResource(t *testing.T) {
	// Create test objects for different resource types
	objects := []runtime.Object{
		// Deployment
		&unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"metadata": map[string]interface{}{
					"name":      "web-deployment",
					"namespace": "default",
				},
				"spec": map[string]interface{}{
					"template": map[string]interface{}{
						"spec": map[string]interface{}{
							"containers": []interface{}{
								map[string]interface{}{
									"name":  "web",
									"image": "nginx:deployment",
								},
							},
						},
					},
				},
			},
		},
		// StatefulSet
		&unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "StatefulSet",
				"metadata": map[string]interface{}{
					"name":      "db-statefulset",
					"namespace": "default",
				},
				"spec": map[string]interface{}{
					"template": map[string]interface{}{
						"spec": map[string]interface{}{
							"containers": []interface{}{
								map[string]interface{}{
									"name":  "database",
									"image": "postgres:statefulset",
								},
							},
						},
					},
				},
			},
		},
		// Job
		&unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "batch/v1",
				"kind":       "Job",
				"metadata": map[string]interface{}{
					"name":      "migration-job",
					"namespace": "default",
				},
				"spec": map[string]interface{}{
					"template": map[string]interface{}{
						"spec": map[string]interface{}{
							"containers": []interface{}{
								map[string]interface{}{
									"name":  "migration",
									"image": "migration:v1.0",
								},
							},
						},
					},
				},
			},
		},
	}

	// Set up test environment
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	appsv1.AddToScheme(scheme)
	batchv1.AddToScheme(scheme)
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme, objects...)

	collector := NewAutoDiscoveryImageCollector(dynamicClient)

	tests := []struct {
		name         string
		resource     AutoDiscoveryResource
		expectedRefs []string
	}{
		{
			name: "deployment resource",
			resource: AutoDiscoveryResource{
				GVR:       schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
				Namespace: "default",
				Name:      "web-deployment",
			},
			expectedRefs: []string{"nginx:deployment"},
		},
		{
			name: "statefulset resource",
			resource: AutoDiscoveryResource{
				GVR:       schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"},
				Namespace: "default",
				Name:      "db-statefulset",
			},
			expectedRefs: []string{"postgres:statefulset"},
		},
		{
			name: "job resource",
			resource: AutoDiscoveryResource{
				GVR:       schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"},
				Namespace: "default",
				Name:      "migration-job",
			},
			expectedRefs: []string{"migration:v1.0"},
		},
		{
			name: "non-image resource",
			resource: AutoDiscoveryResource{
				GVR:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"},
				Namespace: "default",
				Name:      "web-service",
			},
			expectedRefs: []string{}, // Services don't have images
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			refs, err := collector.extractImageRefsFromResource(ctx, tt.resource)
			
			if err != nil {
				t.Errorf("Unexpected error extracting image refs: %v", err)
			}

			if len(refs) != len(tt.expectedRefs) {
				t.Errorf("Expected %d image refs, got %d: %v", len(tt.expectedRefs), len(refs), refs)
			}

			// Check each expected ref is present
			refMap := make(map[string]bool)
			for _, ref := range refs {
				refMap[ref] = true
			}

			for _, expectedRef := range tt.expectedRefs {
				if !refMap[expectedRef] {
					t.Errorf("Expected image ref %s not found", expectedRef)
				}
			}
		})
	}
}

// Benchmark tests
func BenchmarkAutoDiscoveryImageCollector_CollectImageFactsFromPods(b *testing.B) {
	// Create test environment with many pods
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	
	var pods []runtime.Object
	for i := 0; i < 100; i++ {
		pods = append(pods, &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Pod",
				"metadata": map[string]interface{}{
					"name":      fmt.Sprintf("pod-%d", i),
					"namespace": "default",
				},
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{
							"name":  "app",
							"image": fmt.Sprintf("app:v%d", i%5), // 5 unique images
						},
					},
				},
			},
		})
	}

	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme, pods...)
	collector := NewAutoDiscoveryImageCollector(dynamicClient)

	// Mock client with all needed digests
	digests := make(map[string]string)
	for i := 0; i < 5; i++ {
		digests[fmt.Sprintf("app:v%d", i)] = fmt.Sprintf("sha256:app%d123", i)
	}
	collector.registryClient = &MockRegistryClient{digests: digests}

	ctx := context.Background()
	options := ImageCollectionOptions{CacheEnabled: true}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := collector.CollectImageFactsFromPods(ctx, []string{"default"}, options)
		if err != nil {
			b.Fatalf("CollectImageFactsFromPods failed: %v", err)
		}
	}
}
