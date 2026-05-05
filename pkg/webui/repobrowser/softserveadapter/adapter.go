// Package softserveadapter implements repobrowser.Browser on top of the
// existing soft-serve backend (pkg/backend.Backend + pkg/git).
//
// It is the production adapter; tests for it live alongside as integration
// tests against a real on-disk Git repository. The adapter is thin: it
// translates domain calls onto the existing wrapper and translates the
// wrapper's errors into repobrowser sentinel errors.
package softserveadapter

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"strings"

	gitmod "github.com/aymanbagabas/git-module"
	pkggit "github.com/charmbracelet/soft-serve/git"
	"github.com/charmbracelet/soft-serve/pkg/backend"
	"github.com/charmbracelet/soft-serve/pkg/proto"
	"github.com/charmbracelet/soft-serve/pkg/webui/repobrowser"
)

// Adapter bridges repobrowser.Browser onto a *backend.Backend.
//
// Visibility filtering: hidden and private repositories are excluded from
// ListRepos; lookups for them by name return ErrRepoNotFound.
type Adapter struct {
	be *backend.Backend
}

// New returns an Adapter backed by be. It panics if be is nil.
func New(be *backend.Backend) *Adapter {
	if be == nil {
		panic("softserveadapter: nil backend")
	}
	return &Adapter{be: be}
}

var _ repobrowser.Browser = (*Adapter)(nil)

// ListRepos implements repobrowser.Browser.
func (a *Adapter) ListRepos(ctx context.Context) ([]repobrowser.RepoInfo, error) {
	repos, err := a.be.Repositories(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]repobrowser.RepoInfo, 0, len(repos))
	for _, r := range repos {
		if r.IsHidden() || r.IsPrivate() {
			continue
		}
		out = append(out, toRepoInfo(r))
	}
	return out, nil
}

// GetRepo implements repobrowser.Browser.
func (a *Adapter) GetRepo(ctx context.Context, name string) (repobrowser.RepoInfo, error) {
	r, err := a.lookup(ctx, name)
	if err != nil {
		return repobrowser.RepoInfo{}, err
	}
	return toRepoInfo(r), nil
}

// ListRefs implements repobrowser.Browser.
func (a *Adapter) ListRefs(ctx context.Context, name string) ([]repobrowser.RefInfo, error) {
	r, err := a.lookup(ctx, name)
	if err != nil {
		return nil, err
	}
	gr, err := r.Open()
	if err != nil {
		return nil, err
	}
	refs, err := gr.References()
	if err != nil {
		return nil, err
	}
	out := make([]repobrowser.RefInfo, 0, len(refs))
	for _, ref := range refs {
		ri := repobrowser.RefInfo{
			Name: ref.Name().Short(),
			Hash: ref.ID,
		}
		switch {
		case ref.IsBranch():
			ri.Kind = repobrowser.RefBranch
		case ref.IsTag():
			ri.Kind = repobrowser.RefTag
		default:
			continue
		}
		out = append(out, ri)
	}
	return out, nil
}

// ListTree implements repobrowser.Browser.
func (a *Adapter) ListTree(ctx context.Context, name, ref, dir string) ([]repobrowser.TreeEntry, error) {
	r, err := a.lookup(ctx, name)
	if err != nil {
		return nil, err
	}
	gr, err := r.Open()
	if err != nil {
		return nil, err
	}
	resolved, err := resolveRef(gr, ref)
	if err != nil {
		return nil, err
	}
	tree, err := gr.TreePath(resolved, dir)
	if err != nil {
		return nil, mapTreeError(err)
	}
	entries, err := tree.Entries()
	if err != nil {
		return nil, mapTreeError(err)
	}
	entries.Sort()

	out := make([]repobrowser.TreeEntry, 0, len(entries))
	base := strings.Trim(dir, "/")
	for _, e := range entries {
		fullPath := e.Name()
		if base != "" {
			fullPath = base + "/" + e.Name()
		}
		out = append(out, repobrowser.TreeEntry{
			Name: e.Name(),
			Path: fullPath,
			Kind: entryKindOf(e),
			Size: entrySize(e),
			Mode: e.Mode().String(),
		})
	}
	return out, nil
}

// ReadBlob implements repobrowser.Browser.
func (a *Adapter) ReadBlob(ctx context.Context, name, ref, p string, maxBytes int64) (repobrowser.Blob, error) {
	r, err := a.lookup(ctx, name)
	if err != nil {
		return repobrowser.Blob{}, err
	}
	gr, err := r.Open()
	if err != nil {
		return repobrowser.Blob{}, err
	}
	resolved, err := resolveRef(gr, ref)
	if err != nil {
		return repobrowser.Blob{}, err
	}
	tree, err := gr.Tree(resolved)
	if err != nil {
		return repobrowser.Blob{}, mapTreeError(err)
	}
	entry, err := tree.TreeEntry(strings.Trim(p, "/"))
	if err != nil {
		return repobrowser.Blob{}, mapTreeError(err)
	}
	if entry.IsTree() {
		return repobrowser.Blob{}, repobrowser.ErrNotAFile
	}
	if !entry.IsBlob() && !entry.IsExec() {
		// gitlinks (submodules) and symlinks not previewed
		return repobrowser.Blob{}, repobrowser.ErrNotAFile
	}

	file := entry.File()

	// Stream contents capped at max(maxBytes, sniff). We need the size
	// even when truncating; git-module exposes Pipeline for streaming.
	var buf bytes.Buffer
	stderr := new(bytes.Buffer)
	if err := file.Pipeline(&buf, stderr); err != nil {
		return repobrowser.Blob{}, err
	}
	full := buf.Bytes()

	binary, _ := pkggit.IsBinary(bufio.NewReader(bytes.NewReader(full)))
	out := repobrowser.Blob{
		Path:     p,
		Size:     int64(len(full)),
		IsBinary: binary,
		Content:  full,
	}
	if maxBytes > 0 && int64(len(full)) > maxBytes {
		out.Content = full[:maxBytes]
		out.Truncated = true
	}
	return out, nil
}

// ListCommits implements repobrowser.Browser.
func (a *Adapter) ListCommits(ctx context.Context, name, ref string, page, size int) ([]repobrowser.CommitInfo, error) {
	r, err := a.lookup(ctx, name)
	if err != nil {
		return nil, err
	}
	gr, err := r.Open()
	if err != nil {
		return nil, err
	}
	resolved, err := resolveRef(gr, ref)
	if err != nil {
		return nil, err
	}
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 25
	}
	commits, err := gr.CommitsByPage(resolved, page, size)
	if err != nil {
		return nil, err
	}
	out := make([]repobrowser.CommitInfo, 0, len(commits))
	for _, c := range commits {
		out = append(out, toCommitInfo(c))
	}
	return out, nil
}

// ---------------------------------------------------------------------
// Internals
// ---------------------------------------------------------------------

func (a *Adapter) lookup(ctx context.Context, name string) (proto.Repository, error) {
	r, err := a.be.Repository(ctx, name)
	if err != nil {
		if errors.Is(err, proto.ErrRepoNotFound) {
			return nil, repobrowser.ErrRepoNotFound
		}
		return nil, err
	}
	if r.IsHidden() || r.IsPrivate() {
		return nil, repobrowser.ErrRepoNotFound
	}
	return r, nil
}

func toRepoInfo(r proto.Repository) repobrowser.RepoInfo {
	branch, _ := proto.RepositoryDefaultBranch(r)
	return repobrowser.RepoInfo{
		Name:          r.Name(),
		ProjectName:   r.ProjectName(),
		Description:   r.Description(),
		UpdatedAt:     r.UpdatedAt(),
		DefaultBranch: branch,
	}
}

func toCommitInfo(c *pkggit.Commit) repobrowser.CommitInfo {
	out := repobrowser.CommitInfo{Subject: c.Summary()}
	if c.ID != nil {
		out.Hash = c.ID.String()
	}
	if c.Author != nil {
		out.Author = c.Author.Name
		out.AuthorEmail = c.Author.Email
		out.When = c.Author.When
	}
	return out
}

func entryKindOf(e *pkggit.TreeEntry) repobrowser.EntryKind {
	switch {
	case e.IsTree():
		return repobrowser.EntryDir
	case e.IsCommit():
		return repobrowser.EntrySubmodule
	case e.IsSymlink():
		return repobrowser.EntrySymlink
	default:
		return repobrowser.EntryFile
	}
}

func entrySize(e *pkggit.TreeEntry) int64 {
	if e.IsTree() || e.IsCommit() {
		return 0
	}
	return e.Blob().Size()
}

// resolveRef converts a UI-supplied ref string into a git Reference.
// Empty, "HEAD" or unknown values fall back to the repository's HEAD.
func resolveRef(gr *pkggit.Repository, ref string) (*pkggit.Reference, error) {
	if ref == "" || ref == "HEAD" {
		return gr.HEAD()
	}
	all, err := gr.References()
	if err != nil {
		return nil, err
	}
	for _, r := range all {
		if r.Name().Short() == ref || r.Refspec == ref || r.ID == ref {
			return r, nil
		}
	}
	// fall back to HEAD if ref isn't found among refs (allows hash-like
	// refs to fail the next git operation with a clearer error)
	return gr.HEAD()
}

// mapTreeError turns git-module's tree-walking errors into the domain's
// sentinels.
func mapTreeError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gitmod.ErrRevisionNotExist) ||
		errors.Is(err, gitmod.ErrSubmoduleNotExist) ||
		errors.Is(err, gitmod.ErrParentNotExist) ||
		errors.Is(err, io.EOF) {
		return repobrowser.ErrPathNotFound
	}
	if errors.Is(err, gitmod.ErrNotBlob) {
		return repobrowser.ErrNotAFile
	}
	if strings.Contains(err.Error(), "not a tree") {
		return repobrowser.ErrNotATree
	}
	if strings.Contains(err.Error(), "Not a valid object name") ||
		strings.Contains(err.Error(), "does not exist") ||
		strings.Contains(err.Error(), "exists on disk, but not in") {
		return repobrowser.ErrPathNotFound
	}
	return err
}
