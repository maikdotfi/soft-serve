package fake_test

import (
	"testing"

	"github.com/charmbracelet/soft-serve/pkg/acme"
	"github.com/charmbracelet/soft-serve/pkg/acme/acmetest"
	"github.com/charmbracelet/soft-serve/pkg/acme/adapters/fake"
)

func TestCache_Contract(t *testing.T) {
	acmetest.RunCacheContract(t, func(*testing.T) acme.Cache { return fake.NewCache() })
}
