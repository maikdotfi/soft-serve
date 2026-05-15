package migrate

import (
	"context"

	"github.com/charmbracelet/soft-serve/pkg/db"
)

const (
	workItemMessagesName    = "work_item_messages"
	workItemMessagesVersion = 7
)

var workItemMessages = Migration{
	Name:    workItemMessagesName,
	Version: workItemMessagesVersion,
	Migrate: func(ctx context.Context, tx *db.Tx) error {
		return migrateUp(ctx, tx, workItemMessagesVersion, workItemMessagesName)
	},
	Rollback: func(ctx context.Context, tx *db.Tx) error {
		return migrateDown(ctx, tx, workItemMessagesVersion, workItemMessagesName)
	},
}
