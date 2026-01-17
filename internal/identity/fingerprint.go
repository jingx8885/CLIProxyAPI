// Package identity provides request fingerprint management for OAuth accounts.
// It ensures that multiple users sharing the same OAuth account appear as a single user
// by unifying request headers and metadata.
package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"
)

// Precompiled regex patterns
var (
	// Matches user_id format: user_{64-char hex}_account__session_{uuid}
	userIDRegex = regexp.MustCompile(`^user_[a-f0-9]{64}_account__session_([a-f0-9-]{36})$`)
	// Matches User-Agent version: xxx/x.y.z
	userAgentVersionRegex = regexp.MustCompile(`/(\d+)\.(\d+)\.(\d+)`)
)

// Default fingerprint values (used when client doesn't provide them)
var defaultFingerprint = Fingerprint{
	UserAgent:               "claude-cli/2.0.62 (external, cli)",
	StainlessLang:           "js",
	StainlessPackageVersion: "0.52.0",
	StainlessOS:             "Linux",
	StainlessArch:           "x64",
	StainlessRuntime:        "node",
	StainlessRuntimeVersion: "v22.14.0",
}

// Fingerprint represents account fingerprint data
type Fingerprint struct {
	ClientID                string `json:"client_id"`
	UserAgent               string `json:"user_agent"`
	StainlessLang           string `json:"stainless_lang"`
	StainlessPackageVersion string `json:"stainless_package_version"`
	StainlessOS             string `json:"stainless_os"`
	StainlessArch           string `json:"stainless_arch"`
	StainlessRuntime        string `json:"stainless_runtime"`
	StainlessRuntimeVersion string `json:"stainless_runtime_version"`
}

// generateClientID generates a 64-character hex client ID (32 random bytes)
func generateClientID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// Extremely rare case, use timestamp as fallback
		log.Warnf("crypto/rand.Read failed: %v, using fallback", err)
		h := sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
		return hex.EncodeToString(h[:])
	}
	return hex.EncodeToString(b)
}

// generateUUIDFromSeed generates a deterministic UUID v4 format string from a seed
func generateUUIDFromSeed(seed string) string {
	hash := sha256.Sum256([]byte(seed))
	bytes := hash[:16]

	// Set UUID v4 version and variant bits
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80

	return fmt.Sprintf("%x-%x-%x-%x-%x",
		bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16])
}

// createFingerprintFromHeaders creates a fingerprint from request headers
func createFingerprintFromHeaders(headers http.Header) *Fingerprint {
	fp := &Fingerprint{}

	// Get User-Agent
	if ua := headers.Get("User-Agent"); ua != "" {
		fp.UserAgent = ua
	} else {
		fp.UserAgent = defaultFingerprint.UserAgent
	}

	// Get x-stainless-* headers, use defaults if not present
	fp.StainlessLang = getHeaderOrDefault(headers, "X-Stainless-Lang", defaultFingerprint.StainlessLang)
	fp.StainlessPackageVersion = getHeaderOrDefault(headers, "X-Stainless-Package-Version", defaultFingerprint.StainlessPackageVersion)
	fp.StainlessOS = getHeaderOrDefault(headers, "X-Stainless-OS", defaultFingerprint.StainlessOS)
	fp.StainlessArch = getHeaderOrDefault(headers, "X-Stainless-Arch", defaultFingerprint.StainlessArch)
	fp.StainlessRuntime = getHeaderOrDefault(headers, "X-Stainless-Runtime", defaultFingerprint.StainlessRuntime)
	fp.StainlessRuntimeVersion = getHeaderOrDefault(headers, "X-Stainless-Runtime-Version", defaultFingerprint.StainlessRuntimeVersion)

	return fp
}

// getHeaderOrDefault gets header value, returns default if not present
func getHeaderOrDefault(headers http.Header, key, defaultValue string) string {
	if v := headers.Get(key); v != "" {
		return v
	}
	return defaultValue
}

// ApplyToRequest applies the fingerprint to request headers (overwrites existing x-stainless-* headers)
func (fp *Fingerprint) ApplyToRequest(req *http.Request) {
	if fp == nil || req == nil {
		return
	}

	// Set user-agent
	if fp.UserAgent != "" {
		req.Header.Set("User-Agent", fp.UserAgent)
	}

	// Set x-stainless-* headers
	if fp.StainlessLang != "" {
		req.Header.Set("X-Stainless-Lang", fp.StainlessLang)
	}
	if fp.StainlessPackageVersion != "" {
		req.Header.Set("X-Stainless-Package-Version", fp.StainlessPackageVersion)
	}
	if fp.StainlessOS != "" {
		req.Header.Set("X-Stainless-OS", fp.StainlessOS)
	}
	if fp.StainlessArch != "" {
		req.Header.Set("X-Stainless-Arch", fp.StainlessArch)
	}
	if fp.StainlessRuntime != "" {
		req.Header.Set("X-Stainless-Runtime", fp.StainlessRuntime)
	}
	if fp.StainlessRuntimeVersion != "" {
		req.Header.Set("X-Stainless-Runtime-Version", fp.StainlessRuntimeVersion)
	}
}

// RewriteUserID rewrites the metadata.user_id in the request body
// Input format: user_{clientId}_account__session_{sessionUUID}
// Output format: user_{cachedClientID}_account_{accountUUID}_session_{newHash}
func RewriteUserID(body []byte, authID, accountUUID, cachedClientID string) ([]byte, error) {
	if len(body) == 0 || accountUUID == "" || cachedClientID == "" {
		return body, nil
	}

	// Parse JSON
	var reqMap map[string]any
	if err := json.Unmarshal(body, &reqMap); err != nil {
		return body, nil
	}

	metadata, ok := reqMap["metadata"].(map[string]any)
	if !ok {
		return body, nil
	}

	userID, ok := metadata["user_id"].(string)
	if !ok || userID == "" {
		return body, nil
	}

	// Match format: user_{64-char hex}_account__session_{uuid}
	matches := userIDRegex.FindStringSubmatch(userID)
	if matches == nil {
		return body, nil
	}

	sessionTail := matches[1] // Original session UUID

	// Generate new session hash: SHA256(authID::sessionTail) -> UUID format
	seed := fmt.Sprintf("%s::%s", authID, sessionTail)
	newSessionHash := generateUUIDFromSeed(seed)

	// Build new user_id
	// Format: user_{cachedClientID}_account_{account_uuid}_session_{newSessionHash}
	newUserID := fmt.Sprintf("user_%s_account_%s_session_%s", cachedClientID, accountUUID, newSessionHash)

	metadata["user_id"] = newUserID
	reqMap["metadata"] = metadata

	return json.Marshal(reqMap)
}

// parseUserAgentVersion parses user-agent version number
// Example: claude-cli/2.0.62 -> (2, 0, 62)
func parseUserAgentVersion(ua string) (major, minor, patch int, ok bool) {
	matches := userAgentVersionRegex.FindStringSubmatch(ua)
	if len(matches) != 4 {
		return 0, 0, 0, false
	}
	major, _ = strconv.Atoi(matches[1])
	minor, _ = strconv.Atoi(matches[2])
	patch, _ = strconv.Atoi(matches[3])
	return major, minor, patch, true
}

// isNewerVersion compares versions, returns true if newUA is newer than cachedUA
func isNewerVersion(newUA, cachedUA string) bool {
	newMajor, newMinor, newPatch, newOk := parseUserAgentVersion(newUA)
	cachedMajor, cachedMinor, cachedPatch, cachedOk := parseUserAgentVersion(cachedUA)

	if !newOk || !cachedOk {
		return false
	}

	// Compare version numbers
	if newMajor > cachedMajor {
		return true
	}
	if newMajor < cachedMajor {
		return false
	}

	if newMinor > cachedMinor {
		return true
	}
	if newMinor < cachedMinor {
		return false
	}

	return newPatch > cachedPatch
}

// Clone returns a deep copy of the fingerprint
func (fp *Fingerprint) Clone() *Fingerprint {
	if fp == nil {
		return nil
	}
	return &Fingerprint{
		ClientID:                fp.ClientID,
		UserAgent:               fp.UserAgent,
		StainlessLang:           fp.StainlessLang,
		StainlessPackageVersion: fp.StainlessPackageVersion,
		StainlessOS:             fp.StainlessOS,
		StainlessArch:           fp.StainlessArch,
		StainlessRuntime:        fp.StainlessRuntime,
		StainlessRuntimeVersion: fp.StainlessRuntimeVersion,
	}
}

// UpdateUserAgent updates the user-agent if the new one is a newer version
func (fp *Fingerprint) UpdateUserAgent(newUA string) bool {
	if fp == nil || newUA == "" {
		return false
	}
	if isNewerVersion(newUA, fp.UserAgent) {
		fp.UserAgent = newUA
		return true
	}
	return false
}
