package fake

import (
	"context"
	"sort"

	"github.com/charmbracelet/soft-serve/pkg/webui/workitembrowser"
)

type Reader struct {
	items    map[string][]workitembrowser.WorkItem
	messages map[string]map[int64][]workitembrowser.WorkItemMessage
}

func New(items map[string][]workitembrowser.WorkItem) *Reader {
	return NewWithMessages(items, nil)
}

func NewWithMessages(items map[string][]workitembrowser.WorkItem, messages map[string]map[int64][]workitembrowser.WorkItemMessage) *Reader {
	cloned := make(map[string][]workitembrowser.WorkItem, len(items))
	for repo, repoItems := range items {
		cloned[repo] = append([]workitembrowser.WorkItem(nil), repoItems...)
	}
	clonedMessages := make(map[string]map[int64][]workitembrowser.WorkItemMessage, len(messages))
	for repo, byItem := range messages {
		clonedMessages[repo] = make(map[int64][]workitembrowser.WorkItemMessage, len(byItem))
		for id, itemMessages := range byItem {
			clonedMessages[repo][id] = append([]workitembrowser.WorkItemMessage(nil), itemMessages...)
		}
	}
	return &Reader{items: cloned, messages: clonedMessages}
}

func (r *Reader) ListByRepo(_ context.Context, repoName string) ([]workitembrowser.WorkItem, error) {
	return append([]workitembrowser.WorkItem(nil), r.items[repoName]...), nil
}

func (r *Reader) Thread(_ context.Context, repoName string, id int64) (workitembrowser.WorkItemThread, error) {
	for _, item := range r.items[repoName] {
		if item.ID != id {
			continue
		}
		messages := []workitembrowser.WorkItemMessage{
			{
				WorkItemID: item.ID,
				Kind:       workitembrowser.MessageKindCard,
				Title:      item.Title,
				Body:       item.Description,
				CreatedAt:  item.CreatedAt,
				UpdatedAt:  item.UpdatedAt,
			},
		}
		messages = append(messages, r.messages[repoName][id]...)
		sort.Slice(messages[1:], func(i, j int) bool {
			return messages[i+1].ID < messages[j+1].ID
		})
		return workitembrowser.WorkItemThread{Item: item, Messages: messages}, nil
	}
	return workitembrowser.WorkItemThread{}, workitembrowser.ErrWorkItemNotFound
}

var _ workitembrowser.Reader = (*Reader)(nil)
