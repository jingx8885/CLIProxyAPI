package executor

import (
	"crypto/sha256"
	"sync"
	"time"

	"github.com/google/uuid"
)

type codexCache struct {
	ID     string
	Expire time.Time
}

// codexCacheMap stores prompt cache IDs keyed by model+user_id.
// Protected by codexCacheMu. Entries expire after 1 hour.
var (
	codexCacheMap = make(map[string]codexCache)
	codexCacheMu  sync.RWMutex
)

// codexCacheCleanupInterval controls how often expired entries are purged.
const codexCacheCleanupInterval = 15 * time.Minute

// codexCacheCleanupOnce ensures the background cleanup goroutine starts only once.
var codexCacheCleanupOnce sync.Once

// startCodexCacheCleanup launches a background goroutine that periodically
// removes expired entries from codexCacheMap to prevent memory leaks.
func startCodexCacheCleanup() {
	go func() {
		ticker := time.NewTicker(codexCacheCleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			purgeExpiredCodexCache()
		}
	}()
}

// purgeExpiredCodexCache removes entries that have expired.
func purgeExpiredCodexCache() {
	now := time.Now()
	codexCacheMu.Lock()
	defer codexCacheMu.Unlock()
	for key, cache := range codexCacheMap {
		if cache.Expire.Before(now) {
			delete(codexCacheMap, key)
		}
	}
}

// getCodexCache retrieves a cached entry, returning ok=false if not found or expired.
func getCodexCache(key string) (codexCache, bool) {
	codexCacheCleanupOnce.Do(startCodexCacheCleanup)
	codexCacheMu.RLock()
	cache, ok := codexCacheMap[key]
	codexCacheMu.RUnlock()
	if !ok || cache.Expire.Before(time.Now()) {
		return codexCache{}, false
	}
	return cache, true
}

// setCodexCache stores a cache entry.
func setCodexCache(key string, cache codexCache) {
	codexCacheCleanupOnce.Do(startCodexCacheCleanup)
	codexCacheMu.Lock()
	codexCacheMap[key] = cache
	codexCacheMu.Unlock()
}

// generateDeterministicUUID generates a deterministic UUID v5 based on the input string.
// This ensures the same input always produces the same UUID, which is essential for
// cache key stability across requests from the same user/session.
func generateDeterministicUUID(input string) string {
	// Use SHA-256 to hash the input, then use the first 16 bytes to create a UUID
	hash := sha256.Sum256([]byte(input))
	// Create a UUID v5-like format using the hash bytes
	return uuid.NewSHA1(uuid.NameSpaceDNS, hash[:]).String()
}
