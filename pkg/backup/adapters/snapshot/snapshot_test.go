package snapshot

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/soft-serve/pkg/db"
	"github.com/matryer/is"
)

func TestServerSnapshotProvider_RoundTripsSQLiteSnapshotWithDefaultDSN(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	tmp := t.TempDir()
	sourceDSN := sqliteSnapshotTestDSN(filepath.Join(tmp, "soft-serve.db"))
	restoredDSN := sqliteSnapshotTestDSN(filepath.Join(tmp, "restored.db"))

	sourceDB := openSnapshotTestDB(t, ctx, sourceDSN)
	_, err := sourceDB.ExecContext(ctx, "CREATE TABLE snapshot_round_trip (id INTEGER PRIMARY KEY, payload TEXT NOT NULL)")
	is.NoErr(err)
	_, err = sourceDB.ExecContext(ctx, "INSERT INTO snapshot_round_trip (id, payload) VALUES (?, ?)", 1, "restored")
	is.NoErr(err)

	sourceProvider := NewServerSnapshotProvider(tmp, sourceDB, sourceDSN, nil)
	content, err := sourceProvider.CreateSnapshotData(ctx)
	is.NoErr(err)

	restoredDB := openSnapshotTestDB(t, ctx, restoredDSN)
	restoredProvider := NewServerSnapshotProvider(tmp, restoredDB, restoredDSN, nil)
	is.NoErr(restoredProvider.RestoreSnapshotData(ctx, content))
	is.NoErr(restoredDB.Close())

	restoredDB = openSnapshotTestDB(t, ctx, restoredDSN)
	var payload string
	err = restoredDB.GetContext(ctx, &payload, "SELECT payload FROM snapshot_round_trip WHERE id = ?", 1)
	is.NoErr(err)
	is.Equal(payload, "restored")
}

func sqliteSnapshotTestDSN(path string) string {
	return path + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
}

func openSnapshotTestDB(t *testing.T, ctx context.Context, dsn string) *db.DB {
	t.Helper()

	dbx, err := db.Open(ctx, "sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = dbx.Close() })

	return dbx
}
