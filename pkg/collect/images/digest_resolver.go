package images

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// DefaultDigestResolver implements DigestResolver interface
type DefaultDigestResolver struct {
	registryClient RegistryClient
	cache          map[string]CacheEntry
	cacheTTL       time.Duration
}

// NewDigestResolver creates a new digest resolver
func NewDigestResolver(registryClient RegistryClient, cacheTTL time.Duration) *DefaultDigestResolver {
	return &DefaultDigestResolver{
		registryClient: registryClient,
		cache:          make(map[string]CacheEntry),
		cacheTTL:       cacheTTL,
	}
}

// ResolveTagToDigest resolves an image tag to its digest
func (dr *DefaultDigestResolver) ResolveTagToDigest(ctx context.Context, imageRef string) (string, error) {
	// Check if already a digest
	if strings.Contains(imageRef, "@sha256:") {
		parts := strings.Split(imageRef, "@")
		return parts[1], nil
	}

	// Check cache first
	if cachedDigest, found := dr.getCachedDigest(imageRef); found {
		return cachedDigest, nil
	}

	// Get digest from registry
	digest, err := dr.getDigestFromRegistry(ctx, imageRef)
	if err != nil {
		return "", fmt.Errorf("failed to get digest from registry: %w", err)
	}

	// Cache the result
	dr.cacheDigest(imageRef, digest)

	return digest, nil
}

func (dr *DefaultDigestResolver) getDigestFromRegistry(ctx context.Context, imageRef string) (string, error) {
	return dr.registryClient.ResolveDigest(ctx, imageRef)
}

func (dr *DefaultDigestResolver) getCachedDigest(imageRef string) (string, bool) {
	entry, exists := dr.cache[imageRef]
	if !exists {
		return "", false
	}

	// Check if cache entry is still valid
	if time.Since(entry.Timestamp) > dr.cacheTTL {
		delete(dr.cache, imageRef)
		return "", false
	}

	// Extract digest from cached facts
	if entry.Facts != nil {
		return entry.Facts.Digest, true
	}

	return "", false
}

func (dr *DefaultDigestResolver) cacheDigest(imageRef, digest string) {
	dr.cache[imageRef] = CacheEntry{
		Facts: &ImageFacts{Digest: digest},
		Timestamp: time.Now(),
	}
}

// ResolvePlatformDigest resolves a multi-platform image to a platform-specific digest
func (dr *DefaultDigestResolver) ResolvePlatformDigest(ctx context.Context, imageRef string, platform Platform) (string, error) {
	// For now, use regular digest resolution (platform-specific logic would need more complex implementation)
	return dr.ResolveTagToDigest(ctx, imageRef)
}

// GetManifestList retrieves a manifest list for multi-platform images
func (dr *DefaultDigestResolver) GetManifestList(ctx context.Context, imageRef string) (*ManifestList, error) {
	// Simplified implementation - in production would query registry
	return nil, fmt.Errorf("manifest list not found for %s", imageRef)
}