package auth

import (
	"context"
	"sync"
	"time"
)

// QuotaInfo represents the quota information fetched from a provider.
type QuotaInfo struct {
	// Total is the total quota amount (e.g., tokens per period). -1 means unknown.
	Total int64 `json:"total"`
	// Remaining is the remaining quota amount. -1 means unknown.
	Remaining int64 `json:"remaining"`
	// ResetAt is when the quota resets.
	ResetAt time.Time `json:"reset_at"`
	// Source indicates where the quota info came from (e.g., "api", "429", "header").
	Source string `json:"source"`
	// FetchedAt is when this info was fetched.
	FetchedAt time.Time `json:"fetched_at"`
}

// QuotaFetcher is an optional interface that provider executors can implement
// to proactively fetch quota information from the upstream provider.
type QuotaFetcher interface {
	// FetchQuota retrieves quota information for a specific auth and model.
	// Returns QuotaInfo with Total/Remaining set to -1 if unknown.
	FetchQuota(ctx context.Context, auth *Auth, model string) (QuotaInfo, error)
}

// quotaCacheKey constructs a unique key for quota cache entries.
func quotaCacheKey(provider, authID, model string) string {
	return provider + ":" + authID + ":" + model
}

// quotaCacheEntry holds cached quota information with TTL.
type quotaCacheEntry struct {
	info      QuotaInfo
	expiresAt time.Time
}

// QuotaCache provides in-memory caching for quota information.
type QuotaCache struct {
	mu      sync.RWMutex
	entries map[string]*quotaCacheEntry
	ttl     time.Duration
}

// NewQuotaCache creates a new QuotaCache with the specified TTL.
func NewQuotaCache(ttl time.Duration) *QuotaCache {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &QuotaCache{
		entries: make(map[string]*quotaCacheEntry),
		ttl:     ttl,
	}
}

// Get retrieves cached quota info if available and not expired.
func (c *QuotaCache) Get(provider, authID, model string) (QuotaInfo, bool) {
	if c == nil {
		return QuotaInfo{}, false
	}
	key := quotaCacheKey(provider, authID, model)
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok || entry == nil {
		return QuotaInfo{}, false
	}
	if time.Now().After(entry.expiresAt) {
		return QuotaInfo{}, false
	}
	return entry.info, true
}

// Set stores quota info in the cache.
func (c *QuotaCache) Set(provider, authID, model string, info QuotaInfo) {
	if c == nil {
		return
	}
	key := quotaCacheKey(provider, authID, model)
	c.mu.Lock()
	c.entries[key] = &quotaCacheEntry{
		info:      info,
		expiresAt: time.Now().Add(c.ttl),
	}
	c.mu.Unlock()
}

// Invalidate removes a specific cache entry.
func (c *QuotaCache) Invalidate(provider, authID, model string) {
	if c == nil {
		return
	}
	key := quotaCacheKey(provider, authID, model)
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}

// Cleanup removes expired entries from the cache.
func (c *QuotaCache) Cleanup() {
	if c == nil {
		return
	}
	now := time.Now()
	c.mu.Lock()
	for key, entry := range c.entries {
		if entry == nil || now.After(entry.expiresAt) {
			delete(c.entries, key)
		}
	}
	c.mu.Unlock()
}

// IsQuotaExhausted checks if the quota state indicates exhaustion.
// Returns (exhausted, resetTime) where exhausted is true if remaining is 0
// and resetTime is when the quota resets.
func IsQuotaExhausted(q *QuotaState, now time.Time) (bool, time.Time) {
	if q == nil {
		return false, time.Time{}
	}

	// If explicitly marked as exceeded
	if q.Exceeded {
		// Prefer ResetAt over NextRecoverAt if available
		resetTime := q.ResetAt
		if resetTime.IsZero() {
			resetTime = q.NextRecoverAt
		}
		// Check if reset time has passed
		if !resetTime.IsZero() && resetTime.After(now) {
			return true, resetTime
		}
		// Reset time passed, quota should be available
		if !resetTime.IsZero() && !resetTime.After(now) {
			return false, time.Time{}
		}
		// No reset time, treat as exhausted
		return true, time.Time{}
	}

	// Check if remaining is explicitly 0
	if q.Remaining == 0 && q.Total > 0 {
		resetTime := q.ResetAt
		if resetTime.IsZero() {
			resetTime = q.NextRecoverAt
		}
		if !resetTime.IsZero() && resetTime.After(now) {
			return true, resetTime
		}
		// Reset time passed
		if !resetTime.IsZero() {
			return false, time.Time{}
		}
		// No reset time but remaining is 0
		return true, time.Time{}
	}

	return false, time.Time{}
}

// GetEffectiveResetTime returns the most accurate reset time from QuotaState.
// It prefers ResetAt over NextRecoverAt.
func GetEffectiveResetTime(q *QuotaState) time.Time {
	if q == nil {
		return time.Time{}
	}
	if !q.ResetAt.IsZero() {
		return q.ResetAt
	}
	return q.NextRecoverAt
}

// UpdateQuotaFromInfo updates a QuotaState with information from QuotaInfo.
func UpdateQuotaFromInfo(q *QuotaState, info QuotaInfo) {
	if q == nil {
		return
	}
	q.Total = info.Total
	q.Remaining = info.Remaining
	q.ResetAt = info.ResetAt
	q.UpdatedAt = info.FetchedAt
	q.Source = info.Source

	// Update exceeded status based on remaining
	if info.Remaining == 0 && info.Total > 0 {
		q.Exceeded = true
		q.Reason = "quota_exhausted"
		if q.NextRecoverAt.IsZero() && !info.ResetAt.IsZero() {
			q.NextRecoverAt = info.ResetAt
		}
	}
}

// UpdateQuotaFrom429 updates QuotaState when a 429 error is received.
func UpdateQuotaFrom429(q *QuotaState, retryAfter *time.Duration, now time.Time, backoffLevel int) {
	if q == nil {
		return
	}
	q.Exceeded = true
	q.Reason = "quota"
	q.Remaining = 0
	q.UpdatedAt = now
	q.Source = "429"

	var next time.Time
	if retryAfter != nil {
		next = now.Add(*retryAfter)
	} else {
		cooldown, nextLevel := nextQuotaCooldown(backoffLevel)
		if cooldown > 0 {
			next = now.Add(cooldown)
		}
		q.BackoffLevel = nextLevel
	}
	q.NextRecoverAt = next
	q.ResetAt = next
}

// ClearQuotaState resets a QuotaState to indicate available quota.
func ClearQuotaState(q *QuotaState, now time.Time) {
	if q == nil {
		return
	}
	q.Exceeded = false
	q.Reason = ""
	q.NextRecoverAt = time.Time{}
	q.BackoffLevel = 0
	q.Remaining = -1
	q.Total = -1
	q.ResetAt = time.Time{}
	q.UpdatedAt = now
	q.Source = ""
}
