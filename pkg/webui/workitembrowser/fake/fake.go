package fake

import (
	"context"

	"github.com/charmbracelet/soft-serve/pkg/webui/workitembrowser"
)

type Reader struct {
	items map[string][]workitembrowser.WorkItem
}

func New(items map[string][]workitembrowser.WorkItem) *Reader {
	cloned := make(map[string][]workitembrowser.WorkItem, len(items))
	for repo, repoItems := range items {
		cloned[repo] = append([]workitembrowser.WorkItem(nil), repoItems...)
	}
	return &Reader{items: cloned}
}

func (r *Reader) ListByRepo(_ context.Context, repoName string) ([]workitembrowser.WorkItem, error) {
	return append([]workitembrowser.WorkItem(nil), r.items[repoName]...), nil
}

var _ workitembrowser.Reader = (*Reader)(nil)
