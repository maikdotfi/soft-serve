package memstore_test

import (
	"testing"

	"github.com/charmbracelet/soft-serve/pkg/workitem/adapters/memstore"
	"github.com/charmbracelet/soft-serve/pkg/workitem/workitemtest"
)

func TestStore_Contract(t *testing.T) {
	workitemtest.RunStoreContract(t, memstore.New())
}
