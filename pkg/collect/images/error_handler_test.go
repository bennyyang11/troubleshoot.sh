package images

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestErrorHandler_ClassifyError(t *testing.T) {
	handler := NewErrorHandler(3, time.Second, FallbackBestEffort)

	tests := []struct {
		name           string
		imageRef       string
		error          error
		expectedType   string
		expectedRetry  bool
	}{
		{
			name:          "authentication error",
			imageRef:      "private.registry.com/app:latest",
			error:         fmt.Errorf("authentication required"),
			expectedType:  "auth",
			expectedRetry: false,
		},
		{
			name:          "unauthorized error",
			imageRef:      "private.registry.com/app:latest",
			error:         fmt.Errorf("401 Unauthorized"),
			expectedType:  "auth",
			expectedRetry: false,
		},
		{
			name:          "network timeout",
			imageRef:      "registry.com/app:latest",
			error:         fmt.Errorf("connection timeout"),
			expectedType:  "network",
			expectedRetry: true,
		},
		{
			name:          "network connection error",
			imageRef:      "registry.com/app:latest",
			error:         fmt.Errorf("network unreachable"),
			expectedType:  "network",
			expectedRetry: true,
		},
		{
			name:          "image not found",
			imageRef:      "registry.com/nonexistent:latest",
			error:         fmt.Errorf("404 not found"),
			expectedType:  "manifest",
			expectedRetry: false,
		},
		{
			name:          "invalid manifest",
			imageRef:      "registry.com/app:latest",
			error:         fmt.Errorf("manifest invalid format"),
			expectedType:  "manifest",
			expectedRetry: false,
		},
		{
			name:          "config blob error",
			imageRef:      "registry.com/app:latest",
			error:         fmt.Errorf("failed to get config blob"),
			expectedType:  "config",
			expectedRetry: true,
		},
		{
			name:          "unknown error",
			imageRef:      "registry.com/app:latest",
			error:         fmt.Errorf("some random error"),
			expectedType:  "unknown",
			expectedRetry: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collectionErr := handler.classifyError(tt.imageRef, tt.error)

			if collectionErr.Type != tt.expectedType {
				t.Errorf("Expected error type %s, got %s", tt.expectedType, collectionErr.Type)
			}
			if collectionErr.Retryable != tt.expectedRetry {
				t.Errorf("Expected retryable %v, got %v", tt.expectedRetry, collectionErr.Retryable)
			}
			if collectionErr.ImageRef != tt.imageRef {
				t.Errorf("Expected imageRef %s, got %s", tt.imageRef, collectionErr.ImageRef)
			}
			if collectionErr.Message != tt.error.Error() {
				t.Errorf("Expected message %s, got %s", tt.error.Error(), collectionErr.Message)
			}
		})
	}
}

func TestErrorHandler_FallbackModes(t *testing.T) {
	tests := []struct {
		name         string
		fallbackMode FallbackMode
		imageRef     string
		error        error
		expectFacts  bool
		expectError  bool
	}{
		{
			name:         "no fallback",
			fallbackMode: FallbackNone,
			imageRef:     "nginx:latest",
			error:        fmt.Errorf("network error"),
			expectFacts:  false,
			expectError:  true,
		},
		{
			name:         "partial fallback",
			fallbackMode: FallbackPartial,
			imageRef:     "nginx:latest",
			error:        fmt.Errorf("network error"),
			expectFacts:  true,
			expectError:  false,
		},
		{
			name:         "best effort fallback",
			fallbackMode: FallbackBestEffort,
			imageRef:     "nginx:latest",
			error:        fmt.Errorf("network error"),
			expectFacts:  true,
			expectError:  false,
		},
		{
			name:         "cached fallback with no cache",
			fallbackMode: FallbackCached,
			imageRef:     "nginx:latest",
			error:        fmt.Errorf("network error"),
			expectFacts:  false,
			expectError:  true, // No cached data available
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewErrorHandler(0, time.Second, tt.fallbackMode) // No retries for testing

			collectionErr := handler.classifyError(tt.imageRef, tt.error)
			facts, err := handler.handleFallback(context.Background(), tt.imageRef, collectionErr)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if tt.expectFacts && facts == nil {
				t.Errorf("Expected facts but got nil")
			}
			if !tt.expectFacts && facts != nil {
				t.Errorf("Expected no facts but got: %+v", facts)
			}

			// If facts were created, validate they have error context
			if facts != nil {
				if _, hasError := facts.Labels["collection.error"]; !hasError {
					t.Errorf("Expected error context in facts labels")
				}
				if _, hasFallback := facts.Labels["collection.fallback"]; !hasFallback {
					t.Errorf("Expected fallback context in facts labels")
				}
			}
		})
	}
}

func TestErrorCollector_RecordError(t *testing.T) {
	collector := NewErrorCollector()

	// Record some errors
	errors := []CollectionError{
		{ImageRef: "nginx:latest", Type: "network", Message: "timeout", Retryable: true},
		{ImageRef: "alpine:latest", Type: "auth", Message: "unauthorized", Retryable: false},
		{ImageRef: "busybox:latest", Type: "network", Message: "connection failed", Retryable: true},
	}

	for _, err := range errors {
		collector.RecordError(err)
	}

	// Verify statistics
	stats := collector.GetErrorSummary()
	if stats.TotalErrors != 3 {
		t.Errorf("Expected 3 total errors, got %d", stats.TotalErrors)
	}
	if stats.RetryableErrors != 2 {
		t.Errorf("Expected 2 retryable errors, got %d", stats.RetryableErrors)
	}
	if stats.UnretryableErrors != 1 {
		t.Errorf("Expected 1 unretryable error, got %d", stats.UnretryableErrors)
	}
	if stats.ErrorsByType["network"] != 2 {
		t.Errorf("Expected 2 network errors, got %d", stats.ErrorsByType["network"])
	}
	if stats.ErrorsByType["auth"] != 1 {
		t.Errorf("Expected 1 auth error, got %d", stats.ErrorsByType["auth"])
	}
}

func TestErrorCollector_GetErrorsByType(t *testing.T) {
	collector := NewErrorCollector()

	// Record mixed error types
	collector.RecordError(CollectionError{Type: "network", ImageRef: "image1"})
	collector.RecordError(CollectionError{Type: "auth", ImageRef: "image2"})
	collector.RecordError(CollectionError{Type: "network", ImageRef: "image3"})

	// Get network errors
	networkErrors := collector.GetErrorsByType("network")
	if len(networkErrors) != 2 {
		t.Errorf("Expected 2 network errors, got %d", len(networkErrors))
	}

	// Get auth errors
	authErrors := collector.GetErrorsByType("auth")
	if len(authErrors) != 1 {
		t.Errorf("Expected 1 auth error, got %d", len(authErrors))
	}

	// Get non-existent error type
	configErrors := collector.GetErrorsByType("config")
	if len(configErrors) != 0 {
		t.Errorf("Expected 0 config errors, got %d", len(configErrors))
	}
}

func TestErrorCollector_ShouldApplyFallback(t *testing.T) {
	tests := []struct {
		name          string
		errors        []CollectionError
		threshold     ErrorThreshold
		shouldFallback bool
	}{
		{
			name: "high error count triggers fallback",
			errors: []CollectionError{
				{Type: "network", Retryable: true},
				{Type: "network", Retryable: true},
				{Type: "network", Retryable: true},
				{Type: "network", Retryable: true},
				{Type: "network", Retryable: true},
				{Type: "network", Retryable: true}, // 6 errors >= threshold of 5
			},
			threshold: ErrorThreshold{
				MaxErrorRate:   0.5, // 50%
				MaxConsecutive: 5,
			},
			shouldFallback: true, // 6 errors >= MaxConsecutive(5)
		},
		{
			name: "consecutive failures trigger fallback",
			errors: []CollectionError{
				{Type: "network"},
				{Type: "network"},
				{Type: "network"},
			},
			threshold: ErrorThreshold{
				MaxErrorRate:   0.9, // 90%
				MaxConsecutive: 2,   // 2 consecutive
			},
			shouldFallback: true, // 3 consecutive > 2
		},
		{
			name: "low error rate no fallback",
			errors: []CollectionError{
				{Type: "network"},
			},
			threshold: ErrorThreshold{
				MaxErrorRate:   0.5,
				MaxConsecutive: 5,
			},
			shouldFallback: false,
		},
		{
			name:       "no errors no fallback",
			errors:     []CollectionError{},
			threshold:  ErrorThreshold{MaxErrorRate: 0.1},
			shouldFallback: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector := NewErrorCollector()
			collector.SetThreshold(tt.threshold)

			// Record all errors
			for _, err := range tt.errors {
				collector.RecordError(err)
			}

			result := collector.ShouldApplyFallback()
			if result != tt.shouldFallback {
				t.Errorf("Expected shouldApplyFallback %v, got %v", tt.shouldFallback, result)
			}
		})
	}
}

func TestResilientImageCollector_CollectImageFacts(t *testing.T) {
	mockClient := &MockRegistryClient{
		digests: map[string]string{
			"nginx:latest":    "sha256:nginx123",
			"busybox:latest": "sha256:busybox456",
			// deliberately NOT including "definitely-does-not-exist:nowhere"
		},
	}
	
	errorHandler := NewErrorHandler(1, 100*time.Millisecond, FallbackBestEffort)
	collector := NewResilientImageCollector(mockClient, errorHandler, 5*time.Minute)

	tests := []struct {
		name         string
		imageRefs    []string
		options      ImageCollectionOptions
		expectSuccess int
		expectFailed  int
	}{
		{
			name:      "successful collection",
			imageRefs: []string{"nginx:latest", "busybox:latest"},
			options: ImageCollectionOptions{
				CacheEnabled: false,
				Timeout:      30 * time.Second,
			},
			expectSuccess: 2,
			expectFailed:  0,
		},
		{
			name:      "mixed success and failure",
			imageRefs: []string{"nginx:latest", "definitely-does-not-exist:nowhere"},
			options: ImageCollectionOptions{
				CacheEnabled: false,
			},
			expectSuccess: 1,
			expectFailed:  1,
		},
		{
			name:      "cache enabled",
			imageRefs: []string{"nginx:latest"},
			options: ImageCollectionOptions{
				CacheEnabled: true,
			},
			expectSuccess: 1,
			expectFailed:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			result, err := collector.CollectImageFacts(ctx, tt.imageRefs, tt.options)

			if err != nil {
				t.Errorf("Unexpected collection error: %v", err)
			}

			// Debug output
			t.Logf("Test %s: requested images: %v", tt.name, tt.imageRefs)
			t.Logf("  Successful: %d (expected %d)", result.Statistics.SuccessfulImages, tt.expectSuccess)
			t.Logf("  Failed: %d (expected %d)", result.Statistics.FailedImages, tt.expectFailed)
			t.Logf("  Facts: %d, Errors: %d", len(result.Facts), len(result.Errors))
			for imgRef, facts := range result.Facts {
				t.Logf("    Success: %s -> %s", imgRef, facts.Digest)
			}
			for imgRef, err := range result.Errors {
				t.Logf("    Error: %s -> %v", imgRef, err)
			}

			if result.Statistics.SuccessfulImages != tt.expectSuccess {
				t.Errorf("Expected %d successful images, got %d", tt.expectSuccess, result.Statistics.SuccessfulImages)
			}
			if result.Statistics.FailedImages != tt.expectFailed {
				t.Errorf("Expected %d failed images, got %d", tt.expectFailed, result.Statistics.FailedImages)
			}
			if result.Statistics.TotalImages != len(tt.imageRefs) {
				t.Errorf("Expected total %d images, got %d", len(tt.imageRefs), result.Statistics.TotalImages)
			}

			// Verify timing information
			if result.Duration <= 0 {
				t.Errorf("Expected positive duration")
			}
			if result.Timestamp.IsZero() {
				t.Errorf("Expected non-zero timestamp")
			}
		})
	}
}

func TestResilientImageCollector_Cache(t *testing.T) {
	mockClient := &MockRegistryClient{
		digests: map[string]string{
			"nginx:latest": "sha256:nginx123",
		},
	}
	
	errorHandler := NewErrorHandler(0, 0, FallbackNone)
	collector := NewResilientImageCollector(mockClient, errorHandler, 100*time.Millisecond)

	ctx := context.Background()
	imageRefs := []string{"nginx:latest"}
	
	// First collection - should miss cache
	options1 := ImageCollectionOptions{CacheEnabled: true}
	result1, err1 := collector.CollectImageFacts(ctx, imageRefs, options1)
	if err1 != nil {
		t.Fatalf("First collection failed: %v", err1)
	}
	if result1.Statistics.CacheMisses != 1 {
		t.Errorf("Expected 1 cache miss, got %d", result1.Statistics.CacheMisses)
	}
	if result1.Statistics.CacheHits != 0 {
		t.Errorf("Expected 0 cache hits, got %d", result1.Statistics.CacheHits)
	}

	// Second collection - should hit cache
	options2 := ImageCollectionOptions{CacheEnabled: true}
	result2, err2 := collector.CollectImageFacts(ctx, imageRefs, options2)
	if err2 != nil {
		t.Fatalf("Second collection failed: %v", err2)
	}
	if result2.Statistics.CacheHits != 1 {
		t.Errorf("Expected 1 cache hit, got %d", result2.Statistics.CacheHits)
	}
	if result2.Statistics.CacheMisses != 0 {
		t.Errorf("Expected 0 cache misses, got %d", result2.Statistics.CacheMisses)
	}

	// Wait for cache to expire
	time.Sleep(150 * time.Millisecond)

	// Third collection - should miss cache again
	options3 := ImageCollectionOptions{CacheEnabled: true}
	result3, err3 := collector.CollectImageFacts(ctx, imageRefs, options3)
	if err3 != nil {
		t.Fatalf("Third collection failed: %v", err3)
	}
	if result3.Statistics.CacheMisses != 1 {
		t.Errorf("Expected 1 cache miss after expiry, got %d", result3.Statistics.CacheMisses)
	}
}

func TestResilientImageCollector_IsImageAccessible(t *testing.T) {
	mockClient := &MockRegistryClient{
		digests: map[string]string{
			"accessible:latest": "sha256:abc123",
		},
	}
	
	errorHandler := NewErrorHandler(0, 0, FallbackNone)
	collector := NewResilientImageCollector(mockClient, errorHandler, 5*time.Minute)

	tests := []struct {
		name      string
		imageRef  string
		expected  bool
	}{
		{
			name:     "accessible image",
			imageRef: "accessible:latest",
			expected: true,
		},
		{
			name:     "inaccessible image",
			imageRef: "inaccessible:latest",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			result := collector.IsImageAccessible(ctx, tt.imageRef)
			
			if result != tt.expected {
				t.Errorf("Expected accessibility %v for %s, got %v", tt.expected, tt.imageRef, result)
			}
		})
	}
}

func TestErrorHandler_CreatePartialFacts(t *testing.T) {
	handler := NewErrorHandler(0, 0, FallbackPartial)

	collectionErr := CollectionError{
		ImageRef: "nginx:latest",
		Type:     "network",
		Message:  "connection timeout",
	}

	facts, err := handler.createPartialFacts("nginx:latest", collectionErr)

	if err != nil {
		t.Fatalf("createPartialFacts failed: %v", err)
	}

	// Verify partial facts structure
	if facts.Repository != "library/nginx" {
		t.Errorf("Wrong repository in partial facts")
	}
	if facts.Registry != "index.docker.io" {
		t.Errorf("Wrong registry in partial facts")
	}
	if facts.Tag != "latest" {
		t.Errorf("Wrong tag in partial facts")
	}

	// Verify error context is present
	if facts.Labels["collection.error"] != "network" {
		t.Errorf("Expected error type in labels")
	}
	if facts.Labels["collection.fallback"] != "partial" {
		t.Errorf("Expected fallback mode in labels")
	}
	if facts.Labels["collection.error.message"] != "connection timeout" {
		t.Errorf("Expected error message in labels")
	}
}

func TestErrorHandler_CreateBestEffortFacts(t *testing.T) {
	handler := NewErrorHandler(0, 0, FallbackBestEffort)

	collectionErr := CollectionError{
		ImageRef: "gcr.io/my-project/my-app:v1.0",
		Type:     "auth",
		Message:  "permission denied",
	}

	facts, err := handler.createBestEffortFacts("gcr.io/my-project/my-app:v1.0", collectionErr)

	if err != nil {
		t.Fatalf("createBestEffortFacts failed: %v", err)
	}

	// Verify best effort facts have reasonable defaults
	if facts.Platform.Architecture != "amd64" {
		t.Errorf("Expected default amd64 architecture")
	}
	if facts.Platform.OS != "linux" {
		t.Errorf("Expected default linux OS")
	}

	// Verify error context
	if facts.Labels["collection.error"] != "auth" {
		t.Errorf("Expected auth error type in labels")
	}
	if facts.Labels["collection.fallback"] != "best-effort" {
		t.Errorf("Expected best-effort fallback in labels")
	}

	// Verify inferred metadata
	if facts.Labels["registry.type"] != "gcr" {
		t.Errorf("Expected GCR registry type to be inferred")
	}
}

// Benchmark tests
func BenchmarkErrorHandler_ClassifyError(b *testing.B) {
	handler := NewErrorHandler(0, 0, FallbackNone)
	imageRef := "nginx:latest"
	err := fmt.Errorf("network connection timeout occurred during image pull")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = handler.classifyError(imageRef, err)
	}
}

func BenchmarkResilientImageCollector_CollectImageFacts(b *testing.B) {
	mockClient := &MockRegistryClient{
		digests: map[string]string{
			"nginx:latest": "sha256:nginx123",
		},
	}
	
	errorHandler := NewErrorHandler(0, 0, FallbackNone)
	collector := NewResilientImageCollector(mockClient, errorHandler, 5*time.Minute)

	imageRefs := []string{"nginx:latest"}
	options := ImageCollectionOptions{CacheEnabled: true}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := collector.CollectImageFacts(ctx, imageRefs, options)
		if err != nil {
			b.Fatalf("CollectImageFacts failed: %v", err)
		}
	}
}
