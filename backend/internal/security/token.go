package security

import (
	"crypto/rand"
	"encoding/base64"
)

// RandomToken returns a URL-safe, unpadded base64 token from nBytes of entropy
// (mirrors Python's secrets.token_urlsafe).
func RandomToken(nBytes int) string {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
