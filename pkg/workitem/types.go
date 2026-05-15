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
