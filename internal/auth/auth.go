// Package auth generates and hashes the API keys organizations use to
// authenticate to the Engine.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

const keyPrefix = "anchor_"

// Returns a new random API key and its hash.
func GenerateAPIKey() (key string, hash []byte, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	key = keyPrefix + base64.RawURLEncoding.EncodeToString(raw)
	return key, HashKey(key), nil
}

func HashKey(key string) []byte {
	h := sha256.Sum256([]byte(key))
	return h[:]
}
