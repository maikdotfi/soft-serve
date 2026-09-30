package dircache_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/soft-serve/pkg/acme"
	"github.com/charmbracelet/soft-serve/pkg/acme/acmetest"
	"github.com/charmbracelet/soft-serve/pkg/acme/adapters/dircache"
)

func TestDirCache_Contract(t *testing.T) {
	acmetest.RunCacheContract(t, func(t *testing.T) acme.Cache {
		return dircache.New(filepath.Join(t.TempDir(), "acme"))
	})
}

func TestDirCache_Put_StoresPrivateFilesInPrivateDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "acme")
	if err := dircache.New(dir).Put(context.Background(), "git.example.com", []byte("key")); err != nil {
		t.Fatalf("Put() = %v", err)
	}
	for path, want := range map[string]os.FileMode{
		dir:                                   0o700,
		filepath.Join(dir, "git.example.com"): 0o600,
	} {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != want {
			t.Errorf("%s mode = %o, want %o", path, got, want)
		}
	}
}

func TestDirCache_Put_ConfinesKeysToDir(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "acme")
	if err := dircache.New(dir).Put(context.Background(), "../escape", []byte("v")); err != nil {
		t.Fatalf("Put() = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "escape")); !os.IsNotExist(err) {
		t.Fatalf("key escaped the cache dir: stat err = %v", err)
	}
}
