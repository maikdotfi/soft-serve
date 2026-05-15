package migrate

import (
	"context"

	"github.com/charmbracelet/soft-serve/pkg/db"
)

const (
	workItemsName    = "work_items"
	workItemsVersion = 6
)

var workItems = Migration{
	Name:    workItemsName,
	Version: workItemsVersion,
	Migrate: func(ctx context.Context, tx *db.Tx) error {
		return migrateUp(ctx, tx, workItemsVersion, workItemsName)
	},
	Rollback: func(ctx context.Context, tx *db.Tx) error {
		return migrateDown(ctx, tx, workItemsVersion, workItemsName)
	},
}
