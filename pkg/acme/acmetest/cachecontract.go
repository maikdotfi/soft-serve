// Package acmetest provides a contract test suite that every acme.Cache
// adapter must pass. The fake adapter is the reference implementation.
package acmetest

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/charmbracelet/soft-serve/pkg/acme"
)

// RunCacheContract runs the acme.Cache contract against the cache returned
// by newCache. newCache must return an empty cache on every call.
func RunCacheContract(t *testing.T, newCache func(t *testing.T) acme.Cache) {
	t.Helper()
	ctx := context.Background()

	t.Run("Get_MissingKeyReturnsErrCacheMiss", func(t *testing.T) {
		_, err := newCache(t).Get(ctx, "git.example.com")
		if !errors.Is(err, acme.ErrCacheMiss) {
			t.Fatalf("Get() err = %v, want ErrCacheMiss", err)
		}
	})

	t.Run("Get_ReturnsWhatPutStored", func(t *testing.T) {
		c := newCache(t)
		if err := c.Put(ctx, "git.example.com", []byte("cert")); err != nil {
			t.Fatalf("Put() = %v", err)
		}
		got, err := c.Get(ctx, "git.example.com")
		if err != nil || !bytes.Equal(got, []byte("cert")) {
			t.Fatalf("Get() = %q, %v; want %q", got, err, "cert")
		}
	})

	t.Run("Put_OverwritesExistingKey", func(t *testing.T) {
		c := newCache(t)
		_ = c.Put(ctx, "k", []byte("old"))
		if err := c.Put(ctx, "k", []byte("new")); err != nil {
			t.Fatalf("Put() = %v", err)
		}
		got, _ := c.Get(ctx, "k")
		if !bytes.Equal(got, []byte("new")) {
			t.Fatalf("Get() = %q, want %q", got, "new")
		}
	})

	t.Run("Delete_RemovesKey", func(t *testing.T) {
		c := newCache(t)
		_ = c.Put(ctx, "k", []byte("v"))
		if err := c.Delete(ctx, "k"); err != nil {
			t.Fatalf("Delete() = %v", err)
		}
		if _, err := c.Get(ctx, "k"); !errors.Is(err, acme.ErrCacheMiss) {
			t.Fatalf("Get() after Delete err = %v, want ErrCacheMiss", err)
		}
	})

	t.Run("Delete_MissingKeyIsNotAnError", func(t *testing.T) {
		if err := newCache(t).Delete(ctx, "absent"); err != nil {
			t.Fatalf("Delete() = %v", err)
		}
	})

	t.Run("Get_ReturnsCopyNotAlias", func(t *testing.T) {
		c := newCache(t)
		data := []byte("v")
		_ = c.Put(ctx, "k", data)
		data[0] = 'x'
		got, _ := c.Get(ctx, "k")
		if !bytes.Equal(got, []byte("v")) {
			t.Fatalf("Get() = %q, want %q", got, "v")
		}
	})
}
