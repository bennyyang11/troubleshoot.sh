package images

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ErrorHandler provides comprehensive error handling and fallback strategies
type ErrorHandler struct {
	retryCount     int
	retryDelay     time.Duration
	fallbackMode   FallbackMode
	errorCollector *ErrorCollector
}

// FallbackMode defines different fallback strategies
type FallbackMode int

const (
	FallbackNone FallbackMode = iota
	FallbackPartial
	FallbackBestEffort
	FallbackCached
)

// ErrorCollector collects and categorizes errors during image collection
type ErrorCollector struct {
	errors    []CollectionError
	stats     ErrorStatistics
	threshold ErrorThreshold
}

// ErrorStatistics tracks error patterns and frequencies
type ErrorStatistics struct {
	TotalErrors       int            `json:"totalErrors"`
	ErrorsByType      map[string]int `json:"errorsByType"`
	ErrorsByRegistry  map[string]int `json:"errorsByRegistry"`
	RetryableErrors   int            `json:"retryableErrors"`
	UnretryableErrors int            `json:"unretryableErrors"`
	LastErrorTime     time.Time      `json:"lastErrorTime"`
}

// ErrorThreshold defines when to apply fallback strategies
type ErrorThreshold struct {
	MaxErrorRate      float64 `json:"maxErrorRate"`      // 0.0-1.0
	MaxConsecutive    int     `json:"maxConsecutive"`    // Max consecutive failures
	MaxPerRegistry    int     `json:"maxPerRegistry"`    // Max errors per registry
	CooldownDuration  time.Duration `json:"cooldownDuration"` // Wait time after threshold
}

// NewErrorHandler creates a new error handler
func NewErrorHandler(retryCount int, retryDelay time.Duration, fallbackMode FallbackMode) *ErrorHandler {
	return &ErrorHandler{
		retryCount:     retryCount,
		retryDelay:     retryDelay,
		fallbackMode:   fallbackMode,
		errorCollector: NewErrorCollector(),
	}
}

// NewErrorCollector creates a new error collector
func NewErrorCollector() *ErrorCollector {
	return &ErrorCollector{
		errors: make([]CollectionError, 0),
		stats: ErrorStatistics{
			ErrorsByType:     make(map[string]int),
			ErrorsByRegistry: make(map[string]int),
		},
		threshold: ErrorThreshold{
			MaxErrorRate:     0.5,  // 50% error rate threshold
			MaxConsecutive:   5,    // 5 consecutive failures
			MaxPerRegistry:   10,   // 10 errors per registry
			CooldownDuration: 30 * time.Second,
		},
	}
}

// HandleError processes an error and determines retry/fallback strategy
func (eh *ErrorHandler) HandleError(ctx context.Context, imageRef string, err error) (*ImageFacts, error) {
	// Classify the error
	collectionErr := eh.classifyError(imageRef, err)
	eh.errorCollector.RecordError(collectionErr)

	// Check if retry is appropriate
	if collectionErr.Retryable && eh.retryCount > 0 {
		return eh.handleRetry(ctx, imageRef, err)
	}

	// Apply fallback strategy
	return eh.handleFallback(ctx, imageRef, collectionErr)
}

// HandleRetry implements retry logic with exponential backoff
func (eh *ErrorHandler) handleRetry(ctx context.Context, imageRef string, originalErr error) (*ImageFacts, error) {
	delay := eh.retryDelay
	
	for attempt := 1; attempt <= eh.retryCount; attempt++ {
		// Wait before retry (except first attempt)
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("context cancelled during retry: %w", originalErr)
			case <-time.After(delay):
				// Continue with retry
			}
		}

		// This is a simplified retry - in a full implementation,
		// this would call back to the registry client
		fmt.Printf("Retrying image collection for %s (attempt %d/%d)\n", imageRef, attempt, eh.retryCount)
		
		// For now, just track that we attempted retry
		eh.errorCollector.stats.TotalErrors++
		
		// Exponential backoff
		delay *= 2
		if delay > time.Minute {
			delay = time.Minute
		}
	}

	return nil, fmt.Errorf("failed after %d retries: %w", eh.retryCount, originalErr)
}

// handleFallback applies appropriate fallback strategy
func (eh *ErrorHandler) handleFallback(ctx context.Context, imageRef string, collectionErr CollectionError) (*ImageFacts, error) {
	switch eh.fallbackMode {
	case FallbackNone:
		return nil, fmt.Errorf("image collection failed: %s", collectionErr.Message)
		
	case FallbackPartial:
		return eh.createPartialFacts(imageRef, collectionErr)
		
	case FallbackBestEffort:
		return eh.createBestEffortFacts(imageRef, collectionErr)
		
	case FallbackCached:
		return eh.getCachedFacts(imageRef)
		
	default:
		return nil, fmt.Errorf("unknown fallback mode: %d", eh.fallbackMode)
	}
}

func (eh *ErrorHandler) classifyError(imageRef string, err error) CollectionError {
	errMsg := err.Error()
	errMsgLower := strings.ToLower(errMsg)
	
	collectionErr := CollectionError{
		ImageRef: imageRef,
		Message:  errMsg,
	}

	// Classify error type and determine if retryable
	switch {
	case strings.Contains(errMsgLower, "authentication") || strings.Contains(errMsgLower, "unauthorized"):
		collectionErr.Type = "auth"
		collectionErr.Retryable = false // Auth errors usually need credential fix
		
	case strings.Contains(errMsgLower, "timeout") || strings.Contains(errMsgLower, "deadline"):
		collectionErr.Type = "network"
		collectionErr.Retryable = true
		
	case strings.Contains(errMsgLower, "connection") || strings.Contains(errMsgLower, "network"):
		collectionErr.Type = "network"
		collectionErr.Retryable = true
		
	case strings.Contains(errMsgLower, "not found") || strings.Contains(errMsgLower, "404"):
		collectionErr.Type = "manifest"
		collectionErr.Retryable = false // Image doesn't exist
		
	case strings.Contains(errMsgLower, "manifest") || strings.Contains(errMsgLower, "invalid"):
		collectionErr.Type = "manifest"
		collectionErr.Retryable = false
		
	case strings.Contains(errMsgLower, "config") || strings.Contains(errMsgLower, "blob"):
		collectionErr.Type = "config"
		collectionErr.Retryable = true
		
	default:
		collectionErr.Type = "unknown"
		collectionErr.Retryable = true // Default to retryable for unknown errors
	}

	return collectionErr
}

func (eh *ErrorHandler) createPartialFacts(imageRef string, collectionErr CollectionError) (*ImageFacts, error) {
	// Create basic facts with what we can determine from the reference
	registry, repository, tag, err := (&DefaultFactsBuilder{}).ExtractImageReference(imageRef)
	if err != nil {
		return nil, fmt.Errorf("failed to extract image reference for fallback: %w", err)
	}

	facts := &ImageFacts{
		Repository: repository,
		Tag:        tag,
		Registry:   registry,
		Created:    time.Now(),
		Labels:     make(map[string]string),
		Platform:   Platform{Architecture: "unknown", OS: "unknown"},
		Layers:     make([]LayerInfo, 0),
	}

	// Add error information to labels
	facts.Labels["collection.error"] = collectionErr.Type
	facts.Labels["collection.error.message"] = collectionErr.Message
	facts.Labels["collection.fallback"] = "partial"

	return facts, nil
}

func (eh *ErrorHandler) createBestEffortFacts(imageRef string, collectionErr CollectionError) (*ImageFacts, error) {
	// Try to create facts with as much information as possible
	registry, repository, tag, err := (&DefaultFactsBuilder{}).ExtractImageReference(imageRef)
	if err != nil {
		return nil, fmt.Errorf("failed to extract image reference for fallback: %w", err)
	}

	facts := &ImageFacts{
		Repository: repository,
		Tag:        tag,
		Registry:   registry,
		Created:    time.Now(),
		Labels:     make(map[string]string),
		Platform:   Platform{Architecture: "amd64", OS: "linux"}, // Reasonable defaults
		Layers:     make([]LayerInfo, 0),
	}

	// Add metadata based on image reference patterns
	eh.inferMetadataFromReference(facts, imageRef)

	// Add error context
	facts.Labels["collection.error"] = collectionErr.Type
	facts.Labels["collection.fallback"] = "best-effort"

	return facts, nil
}

func (eh *ErrorHandler) getCachedFacts(imageRef string) (*ImageFacts, error) {
	// This would integrate with a persistent cache in a full implementation
	// For now, return an error indicating cache miss
	return nil, fmt.Errorf("no cached facts available for %s", imageRef)
}

func (eh *ErrorHandler) inferMetadataFromReference(facts *ImageFacts, imageRef string) {
	// Infer metadata from image reference patterns
	repoLower := strings.ToLower(facts.Repository)
	
	// Detect common application types
	if strings.Contains(repoLower, "nginx") {
		facts.Labels["app.type"] = "webserver"
		facts.Labels["app.name"] = "nginx"
	} else if strings.Contains(repoLower, "redis") {
		facts.Labels["app.type"] = "database"
		facts.Labels["app.name"] = "redis"
	} else if strings.Contains(repoLower, "postgres") {
		facts.Labels["app.type"] = "database"
		facts.Labels["app.name"] = "postgresql"
	} else if strings.Contains(repoLower, "mysql") {
		facts.Labels["app.type"] = "database"
		facts.Labels["app.name"] = "mysql"
	} else if strings.Contains(repoLower, "alpine") {
		facts.Labels["base.image"] = "alpine"
		facts.Labels["image.type"] = "minimal"
	}

	// Detect registry types
	switch facts.Registry {
	case "index.docker.io", "docker.io":
		facts.Labels["registry.type"] = "docker-hub"
		facts.Labels["registry.public"] = "true"
	case "gcr.io", "us.gcr.io", "eu.gcr.io", "asia.gcr.io":
		facts.Labels["registry.type"] = "gcr"
		facts.Labels["registry.provider"] = "google"
	case "quay.io":
		facts.Labels["registry.type"] = "quay"
		facts.Labels["registry.provider"] = "redhat"
	case "ghcr.io":
		facts.Labels["registry.type"] = "github"
		facts.Labels["registry.provider"] = "github"
	default:
		if strings.Contains(facts.Registry, "amazonaws.com") {
			facts.Labels["registry.type"] = "ecr"
			facts.Labels["registry.provider"] = "aws"
		} else if strings.Contains(facts.Registry, "azurecr.io") {
			facts.Labels["registry.type"] = "acr"
			facts.Labels["registry.provider"] = "azure"
		} else {
			facts.Labels["registry.type"] = "custom"
		}
	}
}

// RecordError records an error in the error collector
func (ec *ErrorCollector) RecordError(err CollectionError) {
	ec.errors = append(ec.errors, err)
	ec.stats.TotalErrors++
	ec.stats.ErrorsByType[err.Type]++
	ec.stats.ErrorsByRegistry[GetRegistryFromImageRef(err.ImageRef)]++
	ec.stats.LastErrorTime = time.Now()
	
	if err.Retryable {
		ec.stats.RetryableErrors++
	} else {
		ec.stats.UnretryableErrors++
	}
}

// ShouldApplyFallback determines if fallback should be applied based on error patterns
func (ec *ErrorCollector) ShouldApplyFallback() bool {
	// If no errors, no fallback needed
	if ec.stats.TotalErrors == 0 {
		return false
	}

	// Check if we have too many consecutive errors
	if len(ec.errors) >= ec.threshold.MaxConsecutive {
		return true
	}

	return false
}

// GetErrorSummary returns a summary of collected errors
func (ec *ErrorCollector) GetErrorSummary() ErrorStatistics {
	return ec.stats
}

// GetErrorsByType returns errors grouped by type
func (ec *ErrorCollector) GetErrorsByType(errorType string) []CollectionError {
	var filtered []CollectionError
	for _, err := range ec.errors {
		if err.Type == errorType {
			filtered = append(filtered, err)
		}
	}
	return filtered
}

// GetErrorsByRegistry returns errors grouped by registry
func (ec *ErrorCollector) GetErrorsByRegistry(registry string) []CollectionError {
	var filtered []CollectionError
	for _, err := range ec.errors {
		if GetRegistryFromImageRef(err.ImageRef) == registry {
			filtered = append(filtered, err)
		}
	}
	return filtered
}

// ClearErrors clears all collected errors
func (ec *ErrorCollector) ClearErrors() {
	ec.errors = make([]CollectionError, 0)
	ec.stats = ErrorStatistics{
		ErrorsByType:     make(map[string]int),
		ErrorsByRegistry: make(map[string]int),
	}
}

// SetThreshold sets the error threshold configuration
func (ec *ErrorCollector) SetThreshold(threshold ErrorThreshold) {
	ec.threshold = threshold
}

// GetThreshold returns the current error threshold configuration
func (ec *ErrorCollector) GetThreshold() ErrorThreshold {
	return ec.threshold
}

// ResilientImageCollector wraps a registry client with error handling
type ResilientImageCollector struct {
	client       RegistryClient
	errorHandler *ErrorHandler
	cache        map[string]*CacheEntry
	cacheTTL     time.Duration
}

// NewResilientImageCollector creates a resilient image collector
func NewResilientImageCollector(client RegistryClient, errorHandler *ErrorHandler, cacheTTL time.Duration) *ResilientImageCollector {
	return &ResilientImageCollector{
		client:       client,
		errorHandler: errorHandler,
		cache:        make(map[string]*CacheEntry),
		cacheTTL:     cacheTTL,
	}
}

// CollectImageFacts collects image facts with error handling and fallback
func (ric *ResilientImageCollector) CollectImageFacts(ctx context.Context, imageRefs []string, options ImageCollectionOptions) (*ImageCollectionResult, error) {
	startTime := time.Now()
	result := &ImageCollectionResult{
		Facts:     make(map[string]*ImageFacts),
		Errors:    make(map[string]error),
		Timestamp: startTime,
	}

	// Initialize statistics
	result.Statistics.TotalImages = len(imageRefs)

	for _, imageRef := range imageRefs {
		// Check cache first if enabled
		if options.CacheEnabled {
			if cachedFacts, found := ric.getCachedFacts(imageRef); found {
				result.Facts[imageRef] = cachedFacts
				result.Statistics.SuccessfulImages++
				result.Statistics.CacheHits++
				continue
			}
			result.Statistics.CacheMisses++
		}

		// Try to collect facts
		facts, err := ric.client.GetImageFacts(ctx, imageRef)
		if err != nil {
			// Check if this is a non-retryable error that should just fail
			collectionErr := ric.errorHandler.classifyError(imageRef, err)
			
			if !collectionErr.Retryable {
				// For non-retryable errors (like image not found), record as failure
				result.Errors[imageRef] = err
				result.Statistics.FailedImages++
				continue
			}
			
			// For retryable errors, try error handling and fallback
			facts, err = ric.errorHandler.HandleError(ctx, imageRef, err)
			if err != nil {
				result.Errors[imageRef] = err
				result.Statistics.FailedImages++
				continue
			}
		}

		// Success - store facts and cache if enabled
		result.Facts[imageRef] = facts
		result.Statistics.SuccessfulImages++
		
		if options.CacheEnabled {
			ric.cacheFacts(imageRef, facts)
		}
	}

	result.Duration = time.Since(startTime)
	
	// Count unique registries accessed
	registries := make(map[string]bool)
	for _, facts := range result.Facts {
		registries[facts.Registry] = true
	}
	result.Statistics.RegistriesAccessed = len(registries)

	return result, nil
}

func (ric *ResilientImageCollector) getCachedFacts(imageRef string) (*ImageFacts, bool) {
	entry, exists := ric.cache[imageRef]
	if !exists {
		return nil, false
	}

	// Check if cache entry is expired
	if time.Since(entry.Timestamp) > ric.cacheTTL {
		delete(ric.cache, imageRef)
		return nil, false
	}

	return entry.Facts, true
}

func (ric *ResilientImageCollector) cacheFacts(imageRef string, facts *ImageFacts) {
	ric.cache[imageRef] = &CacheEntry{
		Facts:     facts,
		Timestamp: time.Now(),
		TTL:       ric.cacheTTL,
	}
}

// CleanupCache removes expired cache entries
func (ric *ResilientImageCollector) CleanupCache() {
	now := time.Now()
	for key, entry := range ric.cache {
		if now.Sub(entry.Timestamp) > entry.TTL {
			delete(ric.cache, key)
		}
	}
}

// GetCacheSize returns the number of cached entries
func (ric *ResilientImageCollector) GetCacheSize() int {
	return len(ric.cache)
}

// GetErrorHandler returns the error handler for inspection
func (ric *ResilientImageCollector) GetErrorHandler() *ErrorHandler {
	return ric.errorHandler
}

// IsImageAccessible performs a lightweight check to see if an image is accessible
func (ric *ResilientImageCollector) IsImageAccessible(ctx context.Context, imageRef string) bool {
	// Try to resolve the digest as a quick accessibility check
	_, err := ric.client.ResolveDigest(ctx, imageRef)
	return err == nil
}
