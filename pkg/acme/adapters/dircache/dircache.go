// Package dircache is an acme.Cache adapter that stores ACME account keys
// and certificates as private files in a directory.
package dircache

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/charmbracelet/soft-serve/pkg/acme"
)

// Cache stores each key as a 0600 file in a 0700 directory.
type Cache struct {
	dir string
}

var _ acme.Cache = (*Cache)(nil)

// New returns a Cache rooted at dir. The directory is created on first Put.
func New(dir string) *Cache {
	return &Cache{dir: dir}
}

// path confines key to the cache directory.
func (c *Cache) path(key string) string {
	return filepath.Join(c.dir, filepath.Clean("/"+key))
}

// Get implements acme.Cache.
func (c *Cache) Get(_ context.Context, key string) ([]byte, error) {
	data, err := os.ReadFile(c.path(key))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, acme.ErrCacheMiss
	}
	if err != nil {
		return nil, fmt.Errorf("read acme cache %q: %w", key, err)
	}
	return data, nil
}

// Put implements acme.Cache. It writes atomically so a crash never leaves
// a truncated certificate behind.
func (c *Cache) Put(_ context.Context, key string, data []byte) error {
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return fmt.Errorf("create acme cache dir: %w", err)
	}
	tmp, err := os.CreateTemp(c.dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("write acme cache %q: %w", key, err)
	}
	defer os.Remove(tmp.Name()) //nolint: errcheck
	if _, err := tmp.Write(data); err != nil {
		tmp.Close() //nolint: errcheck
		return fmt.Errorf("write acme cache %q: %w", key, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write acme cache %q: %w", key, err)
	}
	if err := os.Rename(tmp.Name(), c.path(key)); err != nil {
		return fmt.Errorf("write acme cache %q: %w", key, err)
	}
	return nil
}

// Delete implements acme.Cache.
func (c *Cache) Delete(_ context.Context, key string) error {
	if err := os.Remove(c.path(key)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("delete acme cache %q: %w", key, err)
	}
	return nil
}
