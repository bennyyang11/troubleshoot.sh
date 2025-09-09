package autodiscovery

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// PermissionCache provides caching for RBAC permission checks to improve performance
type PermissionCache struct {
	cache map[string]*CacheEntry
	mutex sync.RWMutex
	ttl   time.Duration
}

// CacheEntry represents a cached permission check result
type CacheEntry struct {
	Result    bool
	Timestamp time.Time
	Error     error
}

// PermissionKey uniquely identifies a permission check
type PermissionKey struct {
	Namespace string
	Verb      string
	GVR       schema.GroupVersionResource
	Name      string
}

// NewPermissionCache creates a new permission cache with the specified TTL
func NewPermissionCache(ttl time.Duration) *PermissionCache {
	return &PermissionCache{
		cache: make(map[string]*CacheEntry),
		ttl:   ttl,
	}
}

// Get retrieves a cached permission check result
func (pc *PermissionCache) Get(key PermissionKey) (bool, bool, error) {
	pc.mutex.RLock()
	defer pc.mutex.RUnlock()

	keyStr := pc.keyToString(key)
	entry, exists := pc.cache[keyStr]
	
	if !exists {
		return false, false, nil
	}

	// Check if entry has expired
	if time.Since(entry.Timestamp) > pc.ttl {
		// Entry expired, remove it
		delete(pc.cache, keyStr)
		return false, false, nil
	}

	return entry.Result, true, entry.Error
}

// Set stores a permission check result in the cache
func (pc *PermissionCache) Set(key PermissionKey, result bool, err error) {
	pc.mutex.Lock()
	defer pc.mutex.Unlock()

	keyStr := pc.keyToString(key)
	pc.cache[keyStr] = &CacheEntry{
		Result:    result,
		Timestamp: time.Now(),
		Error:     err,
	}
}

// Clear removes all entries from the cache
func (pc *PermissionCache) Clear() {
	pc.mutex.Lock()
	defer pc.mutex.Unlock()
	
	pc.cache = make(map[string]*CacheEntry)
}

// Size returns the number of entries in the cache
func (pc *PermissionCache) Size() int {
	pc.mutex.RLock()
	defer pc.mutex.RUnlock()
	
	return len(pc.cache)
}

// Cleanup removes expired entries from the cache
func (pc *PermissionCache) Cleanup() {
	pc.mutex.Lock()
	defer pc.mutex.Unlock()

	now := time.Now()
	for key, entry := range pc.cache {
		if now.Sub(entry.Timestamp) > pc.ttl {
			delete(pc.cache, key)
		}
	}
}

// StartCleanupTimer starts a background goroutine that periodically cleans up expired entries
func (pc *PermissionCache) StartCleanupTimer(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pc.Cleanup()
			}
		}
	}()
}

// keyToString converts a permission key to a string for use as a map key
func (pc *PermissionCache) keyToString(key PermissionKey) string {
	return fmt.Sprintf("%s/%s/%s/%s/%s/%s", 
		key.Namespace, key.Verb, key.GVR.Group, key.GVR.Version, key.GVR.Resource, key.Name)
}

// GetStats returns cache statistics
func (pc *PermissionCache) GetStats() CacheStats {
	pc.mutex.RLock()
	defer pc.mutex.RUnlock()

	stats := CacheStats{
		Size:    len(pc.cache),
		TTL:     pc.ttl,
		Entries: make([]CacheEntryInfo, 0, len(pc.cache)),
	}

	now := time.Now()
	for key, entry := range pc.cache {
		stats.Entries = append(stats.Entries, CacheEntryInfo{
			Key:       key,
			Result:    entry.Result,
			Age:       now.Sub(entry.Timestamp),
			Expired:   now.Sub(entry.Timestamp) > pc.ttl,
			HasError:  entry.Error != nil,
		})
	}

	return stats
}

// CacheStats provides information about cache performance
type CacheStats struct {
	Size    int                `json:"size"`
	TTL     time.Duration      `json:"ttl"`
	Entries []CacheEntryInfo   `json:"entries,omitempty"`
}

// CacheEntryInfo provides details about a cache entry
type CacheEntryInfo struct {
	Key      string        `json:"key"`
	Result   bool          `json:"result"`
	Age      time.Duration `json:"age"`
	Expired  bool          `json:"expired"`
	HasError bool          `json:"hasError"`
}
