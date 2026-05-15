package serviceadapter

import (
	"context"

	"github.com/charmbracelet/soft-serve/pkg/webui/workitembrowser"
	"github.com/charmbracelet/soft-serve/pkg/workitem"
)

type Adapter struct {
	svc *workitem.Service
}

func New(svc *workitem.Service) *Adapter {
	return &Adapter{svc: svc}
}

func (a *Adapter) ListByRepo(ctx context.Context, repoName string) ([]workitembrowser.WorkItem, error) {
	items, err := a.svc.ListByRepo(ctx, repoName)
	if err != nil {
		return nil, err
	}
	out := make([]workitembrowser.WorkItem, 0, len(items))
	for _, item := range items {
		out = append(out, workitembrowser.WorkItem{
			ID:          item.ID,
			Title:       item.Title,
			Description: item.Description,
			Lane:        workitembrowser.Lane(item.Lane),
			CreatedAt:   item.CreatedAt,
			UpdatedAt:   item.UpdatedAt,
		})
	}
	return out, nil
}

var _ workitembrowser.Reader = (*Adapter)(nil)
