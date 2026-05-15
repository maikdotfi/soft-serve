package workitem

import "errors"

var (
	ErrInvalidLane      = errors.New("workitem: invalid lane")
	ErrInvalidMessage   = errors.New("workitem: invalid message")
	ErrInvalidTitle     = errors.New("workitem: invalid title")
	ErrWorkItemNotFound = errors.New("workitem: work item not found")
)
