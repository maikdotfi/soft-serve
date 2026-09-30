# Fork notes

This repository is a fork of [charmbracelet/soft-serve](https://github.com/charmbracelet/soft-serve).
It adds:

- **S3 backup and restore**: scheduled repo bundles and server snapshots, plus `soft restore` ([docs](docs/fork/BACKUP.md), [spec](docs/fork/backup.allium)).
- **CI**: workflow validation on push, runs, a runner dispatch API and `soft ci` ([docs](docs/fork/CI.md), [spec](docs/fork/ci.allium)).
- **Web UI**: a read-only HTML browser at `/ui`, including backup status. It is admin-only: HTTP Basic auth with an admin's username and one of their access tokens (`soft token create ui`) as the password.
- **Automatic TLS**: Let's Encrypt certificates for the HTTP server via TLS-ALPN-01 ([docs](docs/fork/ACME.md)).
- **Deploy tooling**: the `Makefile`, `deploy/`, and a local Garage daemon for S3 integration tests.

The fork is rebased onto upstream regularly. Everything below exists to keep that cheap.

## Where fork code lives

Fork logic lives in files and packages upstream doesn't have, so upstream never edits them:

| Area | Fork-owned paths |
| --- | --- |
| Domain + adapters | `pkg/acme/`, `pkg/backup/`, `pkg/ci/`, `pkg/webui/` |
| Composition root | `cmd/acme.go`, `cmd/services.go`, `cmd/soft/fork.go`, `cmd/soft/ci/`, `cmd/soft/restore/` |
| Glue inside upstream packages | `pkg/backend/fork.go`, `pkg/backend/ci.go`, `pkg/config/acme.go`, `pkg/config/backup.go`, `pkg/db/migrate/fork*.go`, `pkg/store/backup.go`, `pkg/store/database/backup.go`, `pkg/web/ci.go`, `pkg/web/webui.go`, `pkg/webhook/fired_event.go` |
| Tests | the above, plus `cmd/soft/serve/acme_test.go`, `testscript/fork_test.go`, `testscript/testdata/acme-config.txtar`, `testscript/testdata/backup-schedule.txtar`, `testscript/testdata/webui-auth.txtar` |
| Docs | `FORK.md`, `docs/fork/`, `AGENTS.md` |

## Touching upstream files

Keep every edit to an upstream-owned file to a **one-line hook marked `// fork`**, and put the logic in a fork-owned file next to it. For example:

```go
envs = append(envs, c.Backup.environ()...) // fork
```

`make upstream-status` lists every upstream file the fork modifies, with line counts. If a hook grows past a line or two, move its body into a fork file.

A few hooks can't be one line: a field in an interface or struct literal, or a map entry. Keep those as short as possible.

## Database migrations

Upstream migrations use a single integer sequence recorded in the `migrations` table. Fork migrations have their **own** sequence in the `fork_migrations` table:

- Add fork migrations to `forkMigrations` in `pkg/db/migrate/fork.go`, with SQL in `fork_NNNN_<name>_<driver>.<up|down>.sql`.
- **Never** add fork migrations to upstream's `migrations` list in `migrations.go`.
- Fork migrations run after upstream migrations, in the same transaction.
- Databases created by older fork builds recorded `backup` and `ci` as upstream versions 4 and 5. `Migrate` moves those rows to `fork_migrations` before upstream migrations run, so upstream's own future 0004 and 0005 still run.
- `soft admin rollback` only rolls back upstream migrations.

## Syncing with upstream

```sh
make upstream-status                       # divergence, new upstream migrations, fork footprint
git switch -c sync/$(date +%F) main
git rebase upstream/main
```

Resolving conflicts:

- **`go.mod` / `go.sum`**: take upstream's side (during a rebase that is `--ours`), re-add the fork's direct dependencies (currently only `github.com/minio/minio-go/v7`), then run `go mod tidy`:

  ```sh
  git checkout --ours go.mod go.sum
  go get github.com/minio/minio-go/v7@<version from main>
  go mod tidy && git add go.mod go.sum
  ```

- **Anything else**: keep upstream's code and re-apply the `// fork` hook on top of it.

`git config rerere.enabled true` makes git remember resolutions between syncs.

Verify, then publish:

```sh
go build ./... && go vet ./... && go test ./...
# Postgres (throwaway container):
docker run -d --rm --name ss-pg -e POSTGRES_PASSWORD=postgres -p 55432:5432 postgres:16
SOFT_SERVE_DB_DRIVER=postgres \
SOFT_SERVE_DB_DATA_SOURCE='postgres://postgres:postgres@localhost:55432/postgres?sslmode=disable' \
  go test ./...
docker stop ss-pg

git switch main && git merge --ff-only sync/<date>
git push --force-with-lease origin main
```

After a sync, check that upstream's new behaviour covers the fork's surfaces too. For example, upstream access-control changes don't automatically apply to `/ui` or `/api/v1/ci`.

GitHub Actions are disabled on this fork. `.github/` is kept identical to upstream so it never conflicts.
