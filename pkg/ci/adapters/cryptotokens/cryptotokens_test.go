package cryptotokens_test

import (
	"regexp"
	"testing"

	"github.com/charmbracelet/soft-serve/pkg/ci/adapters/cryptotokens"
)

func TestGenerator_TokensAreHex64AndUnique(t *testing.T) {
	hex64 := regexp.MustCompile(`^[0-9a-f]{64}$`)
	gen := cryptotokens.New()
	seen := make(map[string]bool, 8)
	for i := 0; i < 8; i++ {
		token, err := gen.NewToken()
		if err != nil {
			t.Fatalf("new token: %v", err)
		}
		if !hex64.MatchString(token) {
			t.Fatalf("token %q does not match 64 hex chars", token)
		}
		if seen[token] {
			t.Fatalf("token %q repeated", token)
		}
		seen[token] = true
	}
}
