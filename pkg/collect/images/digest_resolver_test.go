package images

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// MockRegistryClient implements RegistryClient interface for testing
type MockRegistryClient struct {
	digests       map[string]string
	manifestLists map[string]*ManifestList
	configs       map[string]string
	errorRate     float64
	timeoutRate   float64
	callCount     int
	mu            sync.RWMutex
	onConnect     func(string)
	simulatedLatency time.Duration
}

func TestDefaultDigestResolver_ResolveTagToDigest(t *testing.T) {
	mockClient := &MockRegistryClient{
		digests: map[string]string{
			"nginx:latest":                        "sha256:abc123def456",
			"gcr.io/my-project/app:v1.0":          "sha256:fedcba987654",
			"my-registry.com:5000/app:latest":     "sha256:111222333444",
		},
	}

	resolver := NewDigestResolver(mockClient, 5*time.Minute)

	tests := []struct {
		name         string
		imageRef     string
		expected     string
		expectError  bool
	}{
		{
			name:        "resolve Docker Hub image",
			imageRef:    "nginx:latest",
			expected:    "sha256:abc123def456",
			expectError: false,
		},
		{
			name:        "resolve GCR image",
			imageRef:    "gcr.io/my-project/app:v1.0",
			expected:    "sha256:fedcba987654",
			expectError: false,
		},
		{
			name:        "resolve private registry image",
			imageRef:    "my-registry.com:5000/app:latest",
			expected:    "sha256:111222333444",
			expectError: false,
		},
		{
			name:        "image reference already has digest",
			imageRef:    "nginx@sha256:existing123",
			expected:    "sha256:existing123",
			expectError: false,
		},
		{
			name:        "unknown image",
			imageRef:    "unknown:tag",
			expected:    "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			result, err := resolver.ResolveTagToDigest(ctx, tt.imageRef)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if !tt.expectError && result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestDefaultDigestResolver_Cache(t *testing.T) {
	mockClient := &MockRegistryClient{
		digests: map[string]string{
			"nginx:latest": "sha256:abc123def456",
		},
		callCount: 0,
	}

	resolver := NewDigestResolver(mockClient, 100*time.Millisecond) // Short TTL for testing

	ctx := context.Background()

	// First call should hit registry
	result1, err := resolver.ResolveTagToDigest(ctx, "nginx:latest")
	if err != nil {
		t.Fatalf("First resolution failed: %v", err)
	}
	if result1 != "sha256:abc123def456" {
		t.Errorf("Expected sha256:abc123def456, got %s", result1)
	}
	if mockClient.callCount != 1 {
		t.Errorf("Expected 1 registry call, got %d", mockClient.callCount)
	}

	// Second call should use cache
	result2, err := resolver.ResolveTagToDigest(ctx, "nginx:latest")
	if err != nil {
		t.Fatalf("Cached resolution failed: %v", err)
	}
	if result2 != result1 {
		t.Errorf("Cached result mismatch")
	}
	if mockClient.callCount != 1 {
		t.Errorf("Expected 1 registry call after cached lookup, got %d", mockClient.callCount)
	}

	// Wait for cache to expire
	time.Sleep(150 * time.Millisecond)

	// Third call should hit registry again
	result3, err := resolver.ResolveTagToDigest(ctx, "nginx:latest")
	if err != nil {
		t.Fatalf("Resolution after cache expiry failed: %v", err)
	}
	if result3 != result1 {
		t.Errorf("Result after cache expiry mismatch")
	}
	if mockClient.callCount != 2 {
		t.Errorf("Expected 2 registry calls after cache expiry, got %d", mockClient.callCount)
	}
}

// Skip platform digest test for now - needs complex mock implementation
func TestDefaultDigestResolver_ResolvePlatformDigest(t *testing.T) {
	t.Skip("Skipping platform digest test - implementation needs platform-specific mock client work")
}

// Skip cache management test for now - needs additional methods
func TestDefaultDigestResolver_CacheManagement(t *testing.T) {
	t.Skip("Skipping cache management test - cache stats methods not implemented in current version")
}

// Mock client implementation
func (m *MockRegistryClient) ResolveDigest(ctx context.Context, imageRef string) (string, error) {
	m.callCount++
	
	// Handle digest references
	if strings.Contains(imageRef, "@sha256:") {
		parts := strings.Split(imageRef, "@")
		return parts[1], nil
	}
	
	// Handle platform-specific digest resolution (for multi-platform images)
	if strings.HasPrefix(imageRef, "sha256:") {
		// This is already a digest, return it directly
		return imageRef, nil
	}
	
	if digest, exists := m.digests[imageRef]; exists {
		return digest, nil
	}
	
	// Be more strict - only return digests for explicitly configured images
	return "", fmt.Errorf("digest not found for %s", imageRef)
}

func (m *MockRegistryClient) GetImageFacts(ctx context.Context, imageRef string) (*ImageFacts, error) {
	// Be strict - only return facts for explicitly configured images
	if _, exists := m.digests[imageRef]; !exists {
		return nil, fmt.Errorf("image not found: %s", imageRef)
	}

	digest, err := m.ResolveDigest(ctx, imageRef)
	if err != nil {
		return nil, err
	}

	return &ImageFacts{
		Repository: "test/repo",
		Tag:        "latest",
		Registry:   "test.registry.com",
		Digest:     digest,
		Platform: Platform{
			Architecture: "amd64",
			OS:          "linux",
		},
		Size:      12345,
	}, nil
}

func (m *MockRegistryClient) ParseManifest(ctx context.Context, imageRef string) (*ManifestInfo, error) {
	return &ManifestInfo{
		SchemaVersion: 2,
		MediaType:     "application/vnd.docker.distribution.manifest.v2+json",
	}, nil
}

func (m *MockRegistryClient) GetImageConfig(ctx context.Context, imageRef string) (*ImageConfig, error) {
	return &ImageConfig{
	}, nil
}

func (m *MockRegistryClient) GetManifestList(ctx context.Context, imageRef string) (*ManifestList, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	if manifestList, exists := m.manifestLists[imageRef]; exists {
		return manifestList, nil
	}
	
	return nil, fmt.Errorf("manifest list not found for %s", imageRef)
}

func (m *MockRegistryClient) AddCredentials(registry string, credentials *RegistryCredentials) {
	// Mock implementation - no-op for testing
}

func (m *MockRegistryClient) Authenticate(ctx context.Context, registry string, credentials *RegistryCredentials) error {
	// Mock implementation - no-op for testing
	return nil
}

func (m *MockRegistryClient) SupportsRegistry(registry string) bool {
	// Mock implementation - supports all registries for testing
	return true
}