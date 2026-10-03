package config

import (
	"crypto/sha256"
	"crypto/subtle"
	"strings"
)

// APIToken is the optional API token (UC1007, environment variable apiToken). Only its SHA-256
// digest is kept, and it never prints: String and GoString only tell whether it is set.
type APIToken struct {
	digest  [sha256.Size]byte
	enabled bool
}

// NewAPIToken returns the token for raw; an empty or blank raw value disables authentication.
// Leading and trailing spaces are ignored: HTTP header values cannot carry them.
func NewAPIToken(raw string) APIToken {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return APIToken{}
	}
	return APIToken{digest: sha256.Sum256([]byte(raw)), enabled: true}
}

// Enabled reports whether requests must be authenticated.
func (t APIToken) Enabled() bool { return t.enabled }

// Matches compares candidate with the token in constant time, whatever their lengths
// (fixed-size digests are compared). Always false when the token is disabled.
func (t APIToken) Matches(candidate string) bool {
	if !t.enabled {
		return false
	}
	sum := sha256.Sum256([]byte(candidate))
	return subtle.ConstantTimeCompare(sum[:], t.digest[:]) == 1
}

// String never returns the token nor its digest.
func (t APIToken) String() string {
	if t.enabled {
		return "set"
	}
	return "unset"
}

// GoString keeps %#v from printing the digest.
func (t APIToken) GoString() string { return "config.APIToken{" + t.String() + "}" }

// CurrentAPIToken returns the configured token; disabled while the configuration is not loaded
// (tests).
func CurrentAPIToken() APIToken {
	if AppConfig == nil {
		return APIToken{}
	}
	return AppConfig.APIToken
}
