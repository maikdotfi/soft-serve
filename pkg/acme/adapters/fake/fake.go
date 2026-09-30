// Package fake provides an in-memory acme.Cache for tests.
package fake

import (
	"bytes"
	"context"
	"sync"

	"github.com/charmbracelet/soft-serve/pkg/acme"
)

// Cache is an in-memory acme.Cache.
type Cache struct {
	mu   sync.Mutex
	data map[string][]byte
}

var _ acme.Cache = (*Cache)(nil)

// NewCache returns an empty Cache.
func NewCache() *Cache {
	return &Cache{data: map[string][]byte{}}
}

// Get implements acme.Cache.
func (c *Cache) Get(_ context.Context, key string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.data[key]
	if !ok {
		return nil, acme.ErrCacheMiss
	}
	return bytes.Clone(v), nil
}

// Put implements acme.Cache.
func (c *Cache) Put(_ context.Context, key string, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[key] = bytes.Clone(data)
	return nil
}

// Delete implements acme.Cache.
func (c *Cache) Delete(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, key)
	return nil
}
