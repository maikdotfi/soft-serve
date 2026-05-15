package workitem

import "time"

type Lane string

const (
	LaneBacklog Lane = "backlog"
	LaneWIP     Lane = "wip"
	LaneDone    Lane = "done"
)

var validLanes = map[Lane]bool{
	LaneBacklog: true,
	LaneWIP:     true,
	LaneDone:    true,
}

func (l Lane) Valid() bool {
	return validLanes[l]
}

type WorkItem struct {
	ID          int64
	RepoName    string
	Title       string
	Description string
	Lane        Lane
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type MessageKind string

const (
	MessageKindCard    MessageKind = "card"
	MessageKindComment MessageKind = "comment"
)

type WorkItemMessage struct {
	ID         int64
	RepoName   string
	WorkItemID int64
	Kind       MessageKind
	Title      string
	Body       string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type WorkItemThread struct {
	Item     WorkItem
	Messages []WorkItemMessage
}
