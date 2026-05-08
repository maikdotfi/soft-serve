// Package cryptotokens is a ci.TokenGenerator backed by crypto/rand.
package cryptotokens

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// Generator implements ci.TokenGenerator. Tokens are 32 random bytes
// hex-encoded, giving 256 bits of entropy and a 64-character ASCII
// representation that is safe to put in HTTP headers and JSON.
type Generator struct {
	bytes int
}

// New constructs a Generator with the default token length.
func New() *Generator {
	return &Generator{bytes: 32}
}

// NewToken returns a fresh random token.
func (g *Generator) NewToken() (string, error) {
	buf := make([]byte, g.bytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
