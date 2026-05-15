package serviceadapter

import (
	"context"
	"errors"

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
		out = append(out, toWorkItem(item))
	}
	return out, nil
}

func (a *Adapter) Thread(ctx context.Context, repoName string, id int64) (workitembrowser.WorkItemThread, error) {
	thread, err := a.svc.Thread(ctx, repoName, id)
	if err != nil {
		if errors.Is(err, workitem.ErrWorkItemNotFound) {
			return workitembrowser.WorkItemThread{}, workitembrowser.ErrWorkItemNotFound
		}
		return workitembrowser.WorkItemThread{}, err
	}

	out := workitembrowser.WorkItemThread{
		Item: toWorkItem(thread.Item),
	}
	out.Messages = make([]workitembrowser.WorkItemMessage, 0, len(thread.Messages))
	for _, message := range thread.Messages {
		out.Messages = append(out.Messages, workitembrowser.WorkItemMessage{
			ID:         message.ID,
			WorkItemID: message.WorkItemID,
			Kind:       workitembrowser.MessageKind(message.Kind),
			Title:      message.Title,
			Body:       message.Body,
			CreatedAt:  message.CreatedAt,
			UpdatedAt:  message.UpdatedAt,
		})
	}
	return out, nil
}

func toWorkItem(item workitem.WorkItem) workitembrowser.WorkItem {
	return workitembrowser.WorkItem{
		ID:          item.ID,
		Title:       item.Title,
		Description: item.Description,
		Lane:        workitembrowser.Lane(item.Lane),
		CreatedAt:   item.CreatedAt,
		UpdatedAt:   item.UpdatedAt,
	}
}

var _ workitembrowser.Reader = (*Adapter)(nil)
