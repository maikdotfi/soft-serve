package migrate

import (
	"context"
	"fmt"

	"charm.land/log/v2"
	"github.com/charmbracelet/soft-serve/pkg/db"
)

// Fork-owned migrations live on their own version sequence, recorded in
// their own table, so they can never collide with migrations upstream adds
// to the "migrations" table. SQL files are named
// fork_NNNN_<name>_<driver>.<up|down>.sql.
//
// Keep this in order of execution, oldest to newest.
var forkMigrations = []Migration{
	forkMigration(1, backupName),
	forkMigration(2, ciName),
}

const (
	forkMigrationsTable = "fork_migrations"

	backupName = "backup"
	ciName     = "ci"
)

// legacyForkMigrations lists the upstream versions that older fork builds
// used for fork migrations. Rows matching both version and name are moved to
// the fork table so upstream's sequence is left free.
var legacyForkMigrations = []struct {
	name          string
	legacyVersion int64
	forkVersion   int64
}{
	{name: backupName, legacyVersion: 4, forkVersion: 1},
	{name: ciName, legacyVersion: 5, forkVersion: 2},
}

func forkMigration(version int64, name string) Migration {
	return Migration{
		Version: version,
		Name:    name,
		Migrate: func(ctx context.Context, tx *db.Tx) error {
			return execForkMigration(ctx, tx, version, name, false)
		},
		Rollback: func(ctx context.Context, tx *db.Tx) error {
			return execForkMigration(ctx, tx, version, name, true)
		},
	}
}

func execForkMigration(ctx context.Context, tx *db.Tx, version int64, name string, down bool) error {
	direction := "up"
	if down {
		direction = "down"
	}

	driverName := tx.DriverName()
	if driverName == "sqlite3" {
		driverName = "sqlite"
	}

	fn := fmt.Sprintf("fork_%04d_%s_%s.%s.sql", version, toSnakeCase(name), driverName, direction)
	sqlstr, err := sqls.ReadFile(fn)
	if err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, string(sqlstr)); err != nil {
		return fmt.Errorf("fork migration %s: %w", fn, err)
	}

	return nil
}

func createForkMigrationsTable(ctx context.Context, tx *db.Tx) error {
	var schema string
	switch tx.DriverName() {
	case "sqlite3", "sqlite":
		schema = `CREATE TABLE IF NOT EXISTS fork_migrations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			version INTEGER NOT NULL UNIQUE
		);`
	case "postgres":
		schema = `CREATE TABLE IF NOT EXISTS fork_migrations (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			version INTEGER NOT NULL UNIQUE
		);`
	case "mysql":
		schema = `CREATE TABLE IF NOT EXISTS fork_migrations (
			id INT NOT NULL AUTO_INCREMENT,
			name TEXT NOT NULL,
			version INT NOT NULL,
			UNIQUE (version),
			PRIMARY KEY (id)
		);`
	default:
		return fmt.Errorf("fork migrations: unknown driver %q", tx.DriverName())
	}

	_, err := tx.ExecContext(ctx, schema)
	return err
}

// adoptLegacyForkMigrations moves fork rows that older builds recorded in
// the upstream migrations table into the fork table. It must run before
// upstream migrations so their "latest version" no longer counts fork rows.
func adoptLegacyForkMigrations(ctx context.Context, tx *db.Tx) error {
	if err := createForkMigrationsTable(ctx, tx); err != nil {
		return err
	}

	for _, l := range legacyForkMigrations {
		res, err := tx.ExecContext(ctx, tx.Rebind("DELETE FROM migrations WHERE version = ? AND name = ?"), l.legacyVersion, l.name)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			continue
		}

		if _, err := tx.ExecContext(ctx, tx.Rebind("INSERT INTO fork_migrations (name, version) VALUES (?, ?)"), l.name, l.forkVersion); err != nil {
			return err
		}
	}

	return nil
}

// migrateFork runs pending fork migrations after upstream ones.
func migrateFork(ctx context.Context, tx *db.Tx) error {
	logger := log.FromContext(ctx).WithPrefix("migrate")

	var latest int64
	if err := tx.Get(&latest, "SELECT COALESCE(MAX(version), 0) FROM fork_migrations"); err != nil {
		return err
	}

	for _, m := range forkMigrations {
		if m.Version <= latest {
			continue
		}

		logger.Infof("running fork migration %d. %s", m.Version, m.Name)
		if err := m.Migrate(ctx, tx); err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, tx.Rebind("INSERT INTO fork_migrations (name, version) VALUES (?, ?)"), m.Name, m.Version); err != nil {
			return err
		}
	}

	return nil
}
