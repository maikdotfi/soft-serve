package migrate

import (
	"context"

	"github.com/charmbracelet/soft-serve/pkg/db"
)

const (
	ciName    = "ci"
	ciVersion = 5
)

var ci = Migration{
	Name:    ciName,
	Version: ciVersion,
	Migrate: func(ctx context.Context, tx *db.Tx) error {
		return migrateUp(ctx, tx, ciVersion, ciName)
	},
	Rollback: func(ctx context.Context, tx *db.Tx) error {
		return migrateDown(ctx, tx, ciVersion, ciName)
	},
}
