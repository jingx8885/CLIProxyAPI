package identity

import (
	"net/http"
	"sync"

	log "github.com/sirupsen/logrus"
)

// Service manages OAuth account request identity fingerprints.
// It ensures that multiple users sharing the same OAuth account
// appear as a single user by caching and reusing fingerprints.
type Service struct {
	mu    sync.RWMutex
	cache map[string]*Fingerprint // keyed by auth ID
}

// NewService creates a new identity service
func NewService() *Service {
	return &Service{
		cache: make(map[string]*Fingerprint),
	}
}

// GetOrCreateFingerprint gets or creates a fingerprint for the given auth ID.
// If cached, checks if client's user-agent is newer and updates if so.
// If not cached, creates a new fingerprint from headers with a random ClientID.
func (s *Service) GetOrCreateFingerprint(authID string, headers http.Header) *Fingerprint {
	if authID == "" {
		return nil
	}

	// Try to get from cache
	s.mu.RLock()
	cached, exists := s.cache[authID]
	s.mu.RUnlock()

	if exists && cached != nil {
		// Check if client's user-agent is a newer version
		clientUA := headers.Get("User-Agent")
		if clientUA != "" && isNewerVersion(clientUA, cached.UserAgent) {
			s.mu.Lock()
			// Double-check after acquiring write lock
			if current, ok := s.cache[authID]; ok && current != nil {
				current.UserAgent = clientUA
				log.Debugf("Updated fingerprint user-agent for auth %s: %s", authID, clientUA)
			}
			s.mu.Unlock()
		}
		return cached.Clone()
	}

	// Cache doesn't exist, create new fingerprint
	fp := createFingerprintFromHeaders(headers)

	// Generate random ClientID
	fp.ClientID = generateClientID()

	// Save to cache
	s.mu.Lock()
	// Double-check in case another goroutine created it
	if existing, ok := s.cache[authID]; ok && existing != nil {
		s.mu.Unlock()
		return existing.Clone()
	}
	s.cache[authID] = fp
	s.mu.Unlock()

	log.Debugf("Created new fingerprint for auth %s with client_id: %s", authID, fp.ClientID)
	return fp.Clone()
}

// GetFingerprint gets the cached fingerprint for the given auth ID.
// Returns nil if not found.
func (s *Service) GetFingerprint(authID string) *Fingerprint {
	if authID == "" {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if fp, ok := s.cache[authID]; ok && fp != nil {
		return fp.Clone()
	}
	return nil
}

// SetFingerprint sets the fingerprint for the given auth ID.
func (s *Service) SetFingerprint(authID string, fp *Fingerprint) {
	if authID == "" || fp == nil {
		return
	}

	s.mu.Lock()
	s.cache[authID] = fp.Clone()
	s.mu.Unlock()
}

// DeleteFingerprint removes the fingerprint for the given auth ID.
func (s *Service) DeleteFingerprint(authID string) {
	if authID == "" {
		return
	}

	s.mu.Lock()
	delete(s.cache, authID)
	s.mu.Unlock()
}

// Clear removes all cached fingerprints.
func (s *Service) Clear() {
	s.mu.Lock()
	s.cache = make(map[string]*Fingerprint)
	s.mu.Unlock()
}

// Count returns the number of cached fingerprints.
func (s *Service) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.cache)
}

// Global singleton instance
var (
	globalService     *Service
	globalServiceOnce sync.Once
)

// GetGlobalService returns the global identity service singleton.
func GetGlobalService() *Service {
	globalServiceOnce.Do(func() {
		globalService = NewService()
	})
	return globalService
}
