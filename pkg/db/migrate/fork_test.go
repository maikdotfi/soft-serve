package migrate

import (
	"context"
	"slices"
	"testing"

	"github.com/charmbracelet/soft-serve/pkg/config"
	"github.com/charmbracelet/soft-serve/pkg/db"
	"github.com/charmbracelet/soft-serve/pkg/db/internal/test"
)

func openMigratedDB(t *testing.T) (context.Context, *db.DB) {
	t.Helper()
	ctx := config.WithContext(context.TODO(), config.DefaultConfig())
	dbx, err := test.OpenSqlite(ctx, t)
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, dbx); err != nil {
		t.Fatalf("Migrate() => %v, want nil error", err)
	}
	return ctx, dbx
}

func appliedMigrations(t *testing.T, dbx *db.DB, table string) []Migrations {
	t.Helper()
	var ms []Migrations
	if err := dbx.Select(&ms, "SELECT * FROM "+table+" ORDER BY version"); err != nil {
		t.Fatalf("select %s: %v", table, err)
	}
	return ms
}

func names(ms []Migrations) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.Name)
	}
	return out
}

// simulateLegacyForkDB rewrites the bookkeeping of a freshly migrated
// database into the shape produced by fork builds that registered backup
// and ci as upstream migrations 4 and 5.
func simulateLegacyForkDB(t *testing.T, dbx *db.DB) {
	t.Helper()
	for _, q := range []string{
		"DROP TABLE " + forkMigrationsTable,
		"INSERT INTO migrations (name, version) VALUES ('backup', 4), ('ci', 5)",
	} {
		if _, err := dbx.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

// withUpstreamMigration temporarily appends a migration to the upstream list,
// standing in for one upstream ships in the future.
func withUpstreamMigration(t *testing.T, m Migration) {
	t.Helper()
	orig := migrations
	migrations = append(slices.Clone(orig), m)
	t.Cleanup(func() { migrations = orig })
}

func TestMigrate_TracksForkMigrationsSeparatelyFromUpstream(t *testing.T) {
	_, dbx := openMigratedDB(t)

	upstream := appliedMigrations(t, dbx, "migrations")
	if got, want := len(upstream), len(migrations); got != want {
		t.Errorf("upstream migrations applied = %v, want %d entries", names(upstream), want)
	}
	for _, m := range upstream {
		if m.Name == backupName || m.Name == ciName {
			t.Errorf("fork migration %q recorded in upstream migrations table", m.Name)
		}
	}

	fork := appliedMigrations(t, dbx, forkMigrationsTable)
	if got, want := names(fork), []string{backupName, ciName}; !slices.Equal(got, want) {
		t.Errorf("fork migrations applied = %v, want %v", got, want)
	}
}

func TestMigrate_IsIdempotent(t *testing.T) {
	ctx, dbx := openMigratedDB(t)
	if err := Migrate(ctx, dbx); err != nil {
		t.Fatalf("second Migrate() => %v, want nil error", err)
	}
	if got := len(appliedMigrations(t, dbx, forkMigrationsTable)); got != len(forkMigrations) {
		t.Errorf("fork migrations applied = %d, want %d", got, len(forkMigrations))
	}
}

func TestMigrate_AdoptsLegacyForkRowsFromUpstreamTable(t *testing.T) {
	ctx, dbx := openMigratedDB(t)
	simulateLegacyForkDB(t, dbx)

	if err := Migrate(ctx, dbx); err != nil {
		t.Fatalf("Migrate() => %v, want nil error", err)
	}

	for _, m := range appliedMigrations(t, dbx, "migrations") {
		if m.Name == backupName || m.Name == ciName {
			t.Errorf("legacy fork row %q@%d still in upstream migrations table", m.Name, m.Version)
		}
	}
	fork := appliedMigrations(t, dbx, forkMigrationsTable)
	if got, want := names(fork), []string{backupName, ciName}; !slices.Equal(got, want) {
		t.Errorf("fork migrations after adoption = %v, want %v", got, want)
	}
}

func TestMigrate_RunsNewUpstreamMigrationOnLegacyForkDB(t *testing.T) {
	ctx, dbx := openMigratedDB(t)
	simulateLegacyForkDB(t, dbx)

	ran := false
	withUpstreamMigration(t, Migration{
		Name:    "future_upstream",
		Version: 4,
		Migrate: func(context.Context, *db.Tx) error {
			ran = true
			return nil
		},
	})

	if err := Migrate(ctx, dbx); err != nil {
		t.Fatalf("Migrate() => %v, want nil error", err)
	}
	if !ran {
		t.Error("upstream migration 4 was skipped because legacy fork rows occupied versions 4-5")
	}
}

func TestMigrate_DoesNotAdoptUpstreamMigrationWithSameVersion(t *testing.T) {
	ctx, dbx := openMigratedDB(t)
	withUpstreamMigration(t, Migration{
		Name:    "future_upstream",
		Version: 4,
		Migrate: func(context.Context, *db.Tx) error { return nil },
	})
	if err := Migrate(ctx, dbx); err != nil {
		t.Fatalf("Migrate() => %v, want nil error", err)
	}

	upstream := appliedMigrations(t, dbx, "migrations")
	if !slices.Contains(names(upstream), "future_upstream") {
		t.Errorf("upstream migrations = %v, want future_upstream kept", names(upstream))
	}
}
