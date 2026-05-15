package memstore

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/charmbracelet/soft-serve/pkg/workitem"
)

type Store struct {
	mu     sync.Mutex
	items  map[int64]workitem.WorkItem
	nextID int64
}

var _ workitem.Store = (*Store)(nil)

func New() *Store {
	return &Store{items: map[int64]workitem.WorkItem{}}
}

func (s *Store) Create(_ context.Context, item workitem.WorkItem) (*workitem.WorkItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextID++
	item.ID = s.nextID
	s.items[item.ID] = item
	out := item
	return &out, nil
}

func (s *Store) Get(_ context.Context, repoName string, id int64) (*workitem.WorkItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.items[id]
	if !ok || item.RepoName != repoName {
		return nil, workitem.ErrWorkItemNotFound
	}
	out := item
	return &out, nil
}

func (s *Store) ListByRepo(_ context.Context, repoName string) ([]workitem.WorkItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]workitem.WorkItem, 0)
	for _, item := range s.items {
		if item.RepoName == repoName {
			out = append(out, item)
		}
	}
	sortWorkItems(out)
	return out, nil
}

func (s *Store) UpdateLane(_ context.Context, repoName string, id int64, lane workitem.Lane, updatedAt time.Time) (*workitem.WorkItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.items[id]
	if !ok || item.RepoName != repoName {
		return nil, workitem.ErrWorkItemNotFound
	}
	item.Lane = lane
	item.UpdatedAt = updatedAt
	s.items[item.ID] = item
	out := item
	return &out, nil
}

func sortWorkItems(items []workitem.WorkItem) {
	sort.Slice(items, func(i, j int) bool {
		if laneRank(items[i].Lane) != laneRank(items[j].Lane) {
			return laneRank(items[i].Lane) < laneRank(items[j].Lane)
		}
		return items[i].ID < items[j].ID
	})
}

func laneRank(lane workitem.Lane) int {
	switch lane {
	case workitem.LaneBacklog:
		return 0
	case workitem.LaneWIP:
		return 1
	case workitem.LaneDone:
		return 2
	default:
		return 3
	}
}
