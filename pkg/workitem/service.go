package workitem

import (
	"context"
	"strings"
)

type Service struct {
	store Store
	clock Clock
}

func NewService(store Store, clock Clock) *Service {
	return &Service{store: store, clock: clock}
}

func (s *Service) Create(ctx context.Context, repoName, title, description string) (WorkItem, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return WorkItem{}, ErrInvalidTitle
	}

	now := s.clock.Now()
	item, err := s.store.Create(ctx, WorkItem{
		RepoName:    repoName,
		Title:       title,
		Description: strings.TrimSpace(description),
		Lane:        LaneBacklog,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		return WorkItem{}, err
	}
	return *item, nil
}

func (s *Service) ListByRepo(ctx context.Context, repoName string) ([]WorkItem, error) {
	return s.store.ListByRepo(ctx, repoName)
}

func (s *Service) Move(ctx context.Context, repoName string, id int64, lane Lane) (WorkItem, error) {
	if !lane.Valid() {
		return WorkItem{}, ErrInvalidLane
	}
	item, err := s.store.UpdateLane(ctx, repoName, id, lane, s.clock.Now())
	if err != nil {
		return WorkItem{}, err
	}
	return *item, nil
}

func (s *Service) Thread(ctx context.Context, repoName string, id int64) (WorkItemThread, error) {
	item, err := s.store.Get(ctx, repoName, id)
	if err != nil {
		return WorkItemThread{}, err
	}
	messages, err := s.store.ListMessages(ctx, repoName, id)
	if err != nil {
		return WorkItemThread{}, err
	}

	opening := WorkItemMessage{
		RepoName:   item.RepoName,
		WorkItemID: item.ID,
		Kind:       MessageKindCard,
		Title:      item.Title,
		Body:       item.Description,
		CreatedAt:  item.CreatedAt,
		UpdatedAt:  item.UpdatedAt,
	}
	messages = append([]WorkItemMessage{opening}, messages...)
	return WorkItemThread{Item: *item, Messages: messages}, nil
}

func (s *Service) AddMessage(ctx context.Context, repoName string, workItemID int64, body string) (WorkItemMessage, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return WorkItemMessage{}, ErrInvalidMessage
	}

	now := s.clock.Now()
	message, err := s.store.AddMessage(ctx, WorkItemMessage{
		RepoName:   repoName,
		WorkItemID: workItemID,
		Kind:       MessageKindComment,
		Body:       body,
		CreatedAt:  now,
		UpdatedAt:  now,
	})
	if err != nil {
		return WorkItemMessage{}, err
	}
	return *message, nil
}
