package migrate

import (
	"context"

	"github.com/charmbracelet/soft-serve/pkg/db"
)

const (
	backupName    = "backup"
	backupVersion = 4
)

var backup = Migration{
	Name:    backupName,
	Version: backupVersion,
	Migrate: func(ctx context.Context, tx *db.Tx) error {
		return migrateUp(ctx, tx, backupVersion, backupName)
	},
	Rollback: func(ctx context.Context, tx *db.Tx) error {
		return migrateDown(ctx, tx, backupVersion, backupName)
	},
}