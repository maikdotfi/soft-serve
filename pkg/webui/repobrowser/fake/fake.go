// Package fake is the in-memory reference implementation of
// repobrowser.Browser. It is the contract-test target that keeps the port
// honest: every test in repobrowsertest must pass against this fake.
//
// The fake is also a useful seam for handler tests: callers can build a
// fixture with New, hand the fake to the HTTP layer, and assert on the
// rendered output without ever touching git.
package fake

import (
	"bytes"
	"context"
	"path"
	"sort"
	"strings"

	"github.com/charmbracelet/soft-serve/pkg/webui/repobrowser"
)

// Repo is the in-memory representation of a fake repository.
type Repo struct {
	Info    repobrowser.RepoInfo
	Files   map[string][]byte
	Commits []repobrowser.CommitInfo
	Refs    []repobrowser.RefInfo
}

// Browser is an in-memory repobrowser.Browser. The zero value is invalid;
// use New.
type Browser struct {
	repos map[string]*Repo
}

// New returns a Browser with the supplied repos installed.
func New(repos []*Repo) *Browser {
	m := make(map[string]*Repo, len(repos))
	for _, r := range repos {
		m[r.Info.Name] = r
	}
	return &Browser{repos: m}
}

// ListRepos implements repobrowser.Browser.
func (b *Browser) ListRepos(_ context.Context) ([]repobrowser.RepoInfo, error) {
	out := make([]repobrowser.RepoInfo, 0, len(b.repos))
	for _, r := range b.repos {
		out = append(out, r.Info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// GetRepo implements repobrowser.Browser.
func (b *Browser) GetRepo(_ context.Context, name string) (repobrowser.RepoInfo, error) {
	r, ok := b.repos[name]
	if !ok {
		return repobrowser.RepoInfo{}, repobrowser.ErrRepoNotFound
	}
	return r.Info, nil
}

// ListRefs implements repobrowser.Browser.
func (b *Browser) ListRefs(_ context.Context, name string) ([]repobrowser.RefInfo, error) {
	r, ok := b.repos[name]
	if !ok {
		return nil, repobrowser.ErrRepoNotFound
	}
	out := make([]repobrowser.RefInfo, len(r.Refs))
	copy(out, r.Refs)
	return out, nil
}

// ListTree implements repobrowser.Browser.
func (b *Browser) ListTree(_ context.Context, name, _, dir string) ([]repobrowser.TreeEntry, error) {
	r, ok := b.repos[name]
	if !ok {
		return nil, repobrowser.ErrRepoNotFound
	}
	dir = strings.Trim(path.Clean("/"+dir), "/")

	seen := map[string]repobrowser.TreeEntry{}
	matched := false
	for filePath, content := range r.Files {
		if dir == "" {
			matched = true
		}
		rest, ok := childOf(dir, filePath)
		if !ok {
			continue
		}
		matched = true
		if rest == "" {
			continue
		}
		parts := strings.SplitN(rest, "/", 2)
		head := parts[0]
		entryPath := head
		if dir != "" {
			entryPath = dir + "/" + head
		}
		if len(parts) == 1 {
			seen[head] = repobrowser.TreeEntry{
				Name: head,
				Path: entryPath,
				Kind: repobrowser.EntryFile,
				Size: int64(len(content)),
				Mode: "100644",
			}
		} else if _, exists := seen[head]; !exists {
			seen[head] = repobrowser.TreeEntry{
				Name: head,
				Path: entryPath,
				Kind: repobrowser.EntryDir,
				Mode: "040000",
			}
		}
	}
	if !matched && dir != "" {
		return nil, repobrowser.ErrPathNotFound
	}
	out := make([]repobrowser.TreeEntry, 0, len(seen))
	for _, e := range seen {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Kind == repobrowser.EntryDir) != (out[j].Kind == repobrowser.EntryDir) {
			return out[i].Kind == repobrowser.EntryDir
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// ReadBlob implements repobrowser.Browser.
func (b *Browser) ReadBlob(_ context.Context, name, _, p string, maxBytes int64) (repobrowser.Blob, error) {
	r, ok := b.repos[name]
	if !ok {
		return repobrowser.Blob{}, repobrowser.ErrRepoNotFound
	}
	p = strings.Trim(path.Clean("/"+p), "/")
	content, ok := r.Files[p]
	if !ok {
		// is it a directory?
		for fp := range r.Files {
			if _, isChild := childOf(p, fp); isChild {
				return repobrowser.Blob{}, repobrowser.ErrNotAFile
			}
		}
		return repobrowser.Blob{}, repobrowser.ErrPathNotFound
	}
	out := repobrowser.Blob{
		Path:     p,
		Size:     int64(len(content)),
		IsBinary: isBinary(content),
		Content:  content,
	}
	if maxBytes > 0 && int64(len(content)) > maxBytes {
		out.Content = content[:maxBytes]
		out.Truncated = true
	}
	return out, nil
}

// ListCommits implements repobrowser.Browser.
func (b *Browser) ListCommits(_ context.Context, name, _ string, page, size int) ([]repobrowser.CommitInfo, error) {
	r, ok := b.repos[name]
	if !ok {
		return nil, repobrowser.ErrRepoNotFound
	}
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 25
	}
	start := (page - 1) * size
	if start >= len(r.Commits) {
		return []repobrowser.CommitInfo{}, nil
	}
	end := start + size
	if end > len(r.Commits) {
		end = len(r.Commits)
	}
	return append([]repobrowser.CommitInfo(nil), r.Commits[start:end]...), nil
}

// childOf returns the path of file relative to dir if file is inside dir.
// dir == "" matches every file.
func childOf(dir, file string) (string, bool) {
	if dir == "" {
		return file, true
	}
	if file == dir {
		return "", true
	}
	if strings.HasPrefix(file, dir+"/") {
		return strings.TrimPrefix(file, dir+"/"), true
	}
	return "", false
}

func isBinary(b []byte) bool {
	if len(b) > 8000 {
		b = b[:8000]
	}
	return bytes.IndexByte(b, 0) >= 0
}

var _ repobrowser.Browser = (*Browser)(nil)
