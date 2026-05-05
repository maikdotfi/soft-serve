// Package repobrowser is a domain port for read-only Git repository browsing.
//
// It exists so the read-only web UI in pkg/webui can be developed and tested
// without depending on any concrete Git implementation. The domain expresses
// what the UI needs in domain terms; concrete adapters (e.g. softserveadapter)
// translate these calls onto specific Git backends.
//
// Per AGENTS.md, callers in the domain or UI never import an adapter directly:
// the composition root constructs a Browser and injects it.
package repobrowser

import (
	"context"
	"errors"
	"time"
)

// Sentinel errors returned by a Browser. Callers match with errors.Is; they
// must not match against adapter-specific errors directly.
var (
	ErrRepoNotFound = errors.New("repobrowser: repository not found")
	ErrRefNotFound  = errors.New("repobrowser: reference not found")
	ErrPathNotFound = errors.New("repobrowser: path not found")
	ErrNotAFile     = errors.New("repobrowser: path is not a file")
	ErrNotATree     = errors.New("repobrowser: path is not a tree")
)

// EntryKind classifies a tree entry.
type EntryKind int

const (
	// EntryDir is a sub-directory (tree).
	EntryDir EntryKind = iota
	// EntryFile is a regular file blob.
	EntryFile
	// EntrySubmodule is a gitlink commit.
	EntrySubmodule
	// EntrySymlink is a symbolic link.
	EntrySymlink
)

// RefKind classifies a reference.
type RefKind int

const (
	// RefBranch is a heads/* reference.
	RefBranch RefKind = iota
	// RefTag is a tags/* reference.
	RefTag
)

// RepoInfo summarises a repository for listing or overview.
type RepoInfo struct {
	Name          string
	ProjectName   string
	Description   string
	UpdatedAt     time.Time
	DefaultBranch string
	Empty         bool
}

// RefInfo describes a branch or tag.
type RefInfo struct {
	Name string
	Kind RefKind
	Hash string
}

// TreeEntry is one row in a directory listing.
type TreeEntry struct {
	Name string
	Path string
	Kind EntryKind
	Size int64
	Mode string
}

// Blob is the materialised content of a file at a ref. Content is capped at
// the maxBytes argument supplied to ReadBlob; if Truncated is true the caller
// rendered only a prefix.
type Blob struct {
	Path      string
	Size      int64
	IsBinary  bool
	Content   []byte
	Truncated bool
}

// CommitInfo summarises a single commit for log views.
type CommitInfo struct {
	Hash        string
	Author      string
	AuthorEmail string
	When        time.Time
	Subject     string
}

// Browser is the read-only port the web UI consumes.
//
// All methods take a context and return domain types only (no git-module,
// no SQL, no SDK types). Adapters translate sentinel errors above so callers
// can match on them with errors.Is.
type Browser interface {
	// ListRepos returns all repositories the UI is allowed to display.
	// The adapter is responsible for any visibility filtering.
	ListRepos(ctx context.Context) ([]RepoInfo, error)

	// GetRepo returns metadata for a single repository.
	// Returns ErrRepoNotFound if the name is unknown.
	GetRepo(ctx context.Context, name string) (RepoInfo, error)

	// ListRefs returns the branches and tags of a repository.
	// Returns ErrRepoNotFound if the name is unknown.
	ListRefs(ctx context.Context, name string) ([]RefInfo, error)

	// ListTree lists entries of a directory at the given ref. An empty ref
	// means the repository's default branch; an empty path means the root.
	// Returns ErrPathNotFound if path does not exist at ref.
	ListTree(ctx context.Context, name, ref, path string) ([]TreeEntry, error)

	// ReadBlob returns the contents of a file at the given ref and path.
	// If the file is larger than maxBytes only the first maxBytes are
	// returned and Truncated is true. If maxBytes is 0 no cap is applied.
	// Returns ErrNotAFile if the path is a directory or submodule.
	ReadBlob(ctx context.Context, name, ref, path string, maxBytes int64) (Blob, error)

	// ListCommits returns up to size commits starting at the given page (1-indexed)
	// reachable from ref. An empty ref means the default branch.
	ListCommits(ctx context.Context, name, ref string, page, size int) ([]CommitInfo, error)
}
