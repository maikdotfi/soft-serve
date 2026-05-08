package memstore_test

import (
	"testing"

	"github.com/charmbracelet/soft-serve/pkg/ci/adapters/memstore"
	"github.com/charmbracelet/soft-serve/pkg/ci/citest"
)

func TestMemStore_Contract(t *testing.T) {
	citest.RunStoreContract(t, memstore.New())
}
