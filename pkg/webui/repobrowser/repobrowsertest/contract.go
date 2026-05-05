// Package repobrowsertest provides a contract test suite that every
// repobrowser.Browser adapter must pass.
//
// Adapters call RunContract from a *_test.go file with a Seeder that
// installs the standard fixture into the adapter's concrete backend. The
// fake adapter is the reference implementation and is exercised in this
// package's own tests.
package repobrowsertest

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/charmbracelet/soft-serve/pkg/webui/repobrowser"
)

// Fixture describes the repositories a contract Browser must surface.
type Fixture struct {
	Repos []FixtureRepo
}

// FixtureRepo is one repository in a contract fixture.
type FixtureRepo struct {
	Name          string
	ProjectName   string
	Description   string
	DefaultBranch string
	UpdatedAt     time.Time
	Files         map[string][]byte
	Commits       []repobrowser.CommitInfo
	Refs          []repobrowser.RefInfo
}

// Seeder installs a Fixture and returns a Browser bound to it.
type Seeder func(t *testing.T, f Fixture) repobrowser.Browser

// StandardFixture is the canonical fixture used by RunContract.
func StandardFixture() Fixture {
	updated := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	return Fixture{
		Repos: []FixtureRepo{
			{
				Name:          "alpha",
				ProjectName:   "Alpha Project",
				Description:   "first test repo",
				DefaultBranch: "main",
				UpdatedAt:     updated,
				Files: map[string][]byte{
					"README.md":      []byte("# Alpha\n\nhello world\n"),
					"src/main.go":    []byte("package main\n\nfunc main() {}\n"),
					"src/util.go":    []byte("package main\n"),
					"docs/intro.txt": []byte("intro\n"),
					"image.bin":      append([]byte{0x00, 0x01, 0x02}, bytes.Repeat([]byte{0xff}, 32)...),
				},
				Commits: []repobrowser.CommitInfo{
					{Hash: "c0ffee1", Author: "Ada", AuthorEmail: "ada@example.com", When: updated, Subject: "initial commit"},
				},
				Refs: []repobrowser.RefInfo{
					{Name: "main", Kind: repobrowser.RefBranch, Hash: "c0ffee1"},
					{Name: "v0.1.0", Kind: repobrowser.RefTag, Hash: "c0ffee1"},
				},
			},
			{
				Name:          "beta",
				ProjectName:   "Beta",
				Description:   "second test repo",
				DefaultBranch: "main",
				UpdatedAt:     updated.Add(-24 * time.Hour),
				Files: map[string][]byte{
					"hello.txt": []byte("beta\n"),
				},
				Commits: []repobrowser.CommitInfo{
					{Hash: "deadbee", Author: "Linus", AuthorEmail: "l@example.com", When: updated.Add(-24 * time.Hour), Subject: "init"},
				},
				Refs: []repobrowser.RefInfo{
					{Name: "main", Kind: repobrowser.RefBranch, Hash: "deadbee"},
				},
			},
		},
	}
}

// RunContract exercises every method on Browser against the standard fixture.
func RunContract(t *testing.T, seed Seeder) {
	t.Helper()
	fx := StandardFixture()
	ctx := context.Background()

	t.Run("ListRepos returns all seeded repos", func(t *testing.T) {
		b := seed(t, fx)
		got, err := b.ListRepos(ctx)
		if err != nil {
			t.Fatalf("ListRepos: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("want 2 repos, got %d", len(got))
		}
		names := []string{got[0].Name, got[1].Name}
		sort.Strings(names)
		if names[0] != "alpha" || names[1] != "beta" {
			t.Fatalf("want [alpha beta], got %v", names)
		}
	})

	t.Run("GetRepo returns metadata for known repo", func(t *testing.T) {
		b := seed(t, fx)
		got, err := b.GetRepo(ctx, "alpha")
		if err != nil {
			t.Fatalf("GetRepo: %v", err)
		}
		if got.Name != "alpha" {
			t.Errorf("Name = %q, want alpha", got.Name)
		}
		if got.ProjectName != "Alpha Project" {
			t.Errorf("ProjectName = %q", got.ProjectName)
		}
		if got.Description != "first test repo" {
			t.Errorf("Description = %q", got.Description)
		}
		if got.DefaultBranch != "main" {
			t.Errorf("DefaultBranch = %q", got.DefaultBranch)
		}
	})

	t.Run("GetRepo returns ErrRepoNotFound for unknown repo", func(t *testing.T) {
		b := seed(t, fx)
		_, err := b.GetRepo(ctx, "nope")
		if !errors.Is(err, repobrowser.ErrRepoNotFound) {
			t.Fatalf("want ErrRepoNotFound, got %v", err)
		}
	})

	t.Run("ListRefs returns branches and tags", func(t *testing.T) {
		b := seed(t, fx)
		refs, err := b.ListRefs(ctx, "alpha")
		if err != nil {
			t.Fatalf("ListRefs: %v", err)
		}
		var branches, tags int
		for _, r := range refs {
			switch r.Kind {
			case repobrowser.RefBranch:
				branches++
			case repobrowser.RefTag:
				tags++
			}
		}
		if branches < 1 || tags < 1 {
			t.Fatalf("want >=1 branch and >=1 tag, got %d branches %d tags", branches, tags)
		}
	})

	t.Run("ListTree at root returns top-level entries", func(t *testing.T) {
		b := seed(t, fx)
		entries, err := b.ListTree(ctx, "alpha", "", "")
		if err != nil {
			t.Fatalf("ListTree: %v", err)
		}
		kind := map[string]repobrowser.EntryKind{}
		for _, e := range entries {
			kind[e.Name] = e.Kind
		}
		if kind["README.md"] != repobrowser.EntryFile {
			t.Errorf("README.md kind = %v, want file", kind["README.md"])
		}
		if kind["src"] != repobrowser.EntryDir {
			t.Errorf("src kind = %v, want dir", kind["src"])
		}
		if kind["docs"] != repobrowser.EntryDir {
			t.Errorf("docs kind = %v, want dir", kind["docs"])
		}
	})

	t.Run("ListTree of subdirectory returns its entries", func(t *testing.T) {
		b := seed(t, fx)
		entries, err := b.ListTree(ctx, "alpha", "", "src")
		if err != nil {
			t.Fatalf("ListTree(src): %v", err)
		}
		var names []string
		for _, e := range entries {
			names = append(names, e.Name)
		}
		sort.Strings(names)
		if len(names) != 2 || names[0] != "main.go" || names[1] != "util.go" {
			t.Fatalf("got %v, want [main.go util.go]", names)
		}
	})

	t.Run("ListTree returns ErrPathNotFound for missing path", func(t *testing.T) {
		b := seed(t, fx)
		_, err := b.ListTree(ctx, "alpha", "", "does/not/exist")
		if !errors.Is(err, repobrowser.ErrPathNotFound) {
			t.Fatalf("want ErrPathNotFound, got %v", err)
		}
	})

	t.Run("ReadBlob returns full content under cap", func(t *testing.T) {
		b := seed(t, fx)
		blob, err := b.ReadBlob(ctx, "alpha", "", "README.md", 1024)
		if err != nil {
			t.Fatalf("ReadBlob: %v", err)
		}
		if blob.IsBinary {
			t.Errorf("IsBinary = true")
		}
		if blob.Truncated {
			t.Errorf("Truncated = true")
		}
		if string(blob.Content) != "# Alpha\n\nhello world\n" {
			t.Errorf("Content = %q", blob.Content)
		}
	})

	t.Run("ReadBlob caps content at maxBytes", func(t *testing.T) {
		b := seed(t, fx)
		blob, err := b.ReadBlob(ctx, "alpha", "", "README.md", 4)
		if err != nil {
			t.Fatalf("ReadBlob: %v", err)
		}
		if !blob.Truncated {
			t.Errorf("Truncated = false")
		}
		if len(blob.Content) != 4 {
			t.Errorf("len(Content) = %d, want 4", len(blob.Content))
		}
	})

	t.Run("ReadBlob flags binary files", func(t *testing.T) {
		b := seed(t, fx)
		blob, err := b.ReadBlob(ctx, "alpha", "", "image.bin", 0)
		if err != nil {
			t.Fatalf("ReadBlob: %v", err)
		}
		if !blob.IsBinary {
			t.Errorf("IsBinary = false")
		}
	})

	t.Run("ReadBlob returns ErrNotAFile for a directory", func(t *testing.T) {
		b := seed(t, fx)
		_, err := b.ReadBlob(ctx, "alpha", "", "src", 0)
		if !errors.Is(err, repobrowser.ErrNotAFile) {
			t.Fatalf("want ErrNotAFile, got %v", err)
		}
	})

	t.Run("ListCommits returns commits for default ref", func(t *testing.T) {
		b := seed(t, fx)
		commits, err := b.ListCommits(ctx, "alpha", "", 1, 10)
		if err != nil {
			t.Fatalf("ListCommits: %v", err)
		}
		if len(commits) == 0 {
			t.Fatal("want >0 commits, got 0")
		}
		if commits[0].Hash == "" || commits[0].Subject == "" {
			t.Errorf("commit missing fields: %+v", commits[0])
		}
	})
}
