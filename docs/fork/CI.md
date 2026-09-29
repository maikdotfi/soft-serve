# CI Workflows

Soft Serve now includes a lightweight CI system that runs workflows on
webhook events like `push`. You define shell scripts inside your repo,
they get dispatched to a runner you host, and the results are tracked
inside Soft Serve.

## How it works

Create a folder named `.soft-serve/workflows/` in any repository and drop
YAML files inside it. Each file becomes a workflow.

When a push lands on the server, Soft Serve parses those files. If any
workflow says it triggers on `push`, a **Run** is created. The run is
dispatched to a registered runner over HTTP, the runner executes the
script, and reports back logs and the exit code.

Runs live through these states:

```
pending → dispatched → running → succeeded
              │               │
              ├→ failed       ├→ failed
              └→ canceled     └→ canceled
```

## Writing a workflow

Each file under `.soft-serve/workflows/` is one workflow. The file name
(without the extension) becomes the workflow name.

Example `.soft-serve/workflows/unit.yml`:

```yaml
script: |
  go test ./...
runs_on: linux-amd64
triggers:
  - push
```

Fields:

| Field | Required | Description |
|-------|----------|-------------|
| `script` | yes | The shell script the runner will execute. |
| `runs_on` | yes | Name of the runner this workflow targets. Must match a registered runner. |
| `triggers` | yes | List of event types: `push`, `branch_tag_create`, `branch_tag_delete`, `collaborator`, `repository`, `repository_visibility_change`. |
| `container` | no | Optional OCI image (e.g. `golang:1.25`) the runner should use. |

Soft Serve validates workflows when you push. If a file is malformed or
references an unknown trigger, the push is rejected and you see the error
in your git client:

```
soft-serve: rejecting push: ci: workflow parse error: script is required
```

## Registering a runner

Before anything can execute, you need at least one runner. Runners are
registered server-wide by an admin.

```sh
soft ci runner register linux-amd64 https://runner.example.local/dispatch
```

This prints a secret token once — store it somewhere safe, because Soft
Serve never reveals it again. The runner presents this token on every
request back to the server.

```
Registered runner "linux-amd64".
Dispatch URL: https://runner.example.local/dispatch
Secret token: aabbccdd...

Store this token now; it is shown only at registration time.
```

List runners:

```sh
soft ci runner list
```

Remove a runner:

```sh
soft ci runner remove linux-amd64
```

Removing a runner does **not** affect runs that are already dispatched to
it, but new runs targeting that runner will immediately fail with
`unknown_runner`.

## Inspecting runs

List every run on the server:

```sh
soft ci run list
```

Show a single run:

```sh
soft ci run get 42
```

Cancel a run:

```sh
soft ci run cancel 42
```

View logs:

```sh
soft ci run logs 42
```

## Manual maintenance

If you ever need to re-sync a repo's workflows from its current HEAD
without pushing:

```sh
soft ci sync my-repo
```

Run the pickup-timeout enforcement pass once (normally runs every minute
in the background):

```sh
soft ci enforce-timeouts
```

Run the retention rotation once (also background normally):

```sh
soft ci rotate
```

## Run lifecycle details

### Pending → Dispatched

When a matching webhook event fires, a run is created in the `pending`
state. A background job picks it up and tries to POST a dispatch payload
to the runner's dispatch URL. If the runner responds with HTTP 2xx, the
run becomes `dispatched`.

If the runner is unknown, or the POST fails, the run immediately becomes
`failed` with a `failure_reason`:

- `unknown_runner` — no runner matches `runs_on`.
- `dispatch_ack_failed` — the runner did not respond or returned non-2xx.

### Dispatched → Running

Once the runner picks up the work it POSTs back to Soft Serve's
`/api/v1/ci/runs/{id}/started` endpoint. The run becomes `running` and
captures the current timestamp as `started_at`.

A runner has a fixed amount of time (1 hour by default) to report
`started` after the dispatch. If that deadline passes, the run is marked
`failed` with `failure_reason = pickup_timeout`.

### Running → Succeeded / Failed

When the script finishes the runner POSTs to
`/api/v1/ci/runs/{id}/completion` with an `exit_code`:

- `exit_code: 0` → run becomes `succeeded`.
- `exit_code != 0` → run becomes `failed` with `runner_reported_failure`.

While a run is running, the runner can stream log lines by POSTing to
`/api/v1/ci/runs/{id}/logs`.

### Cancellation

Canceling a pending run transitions it straight to `canceled`.
Canceling a dispatched or running run sends a cancel webhook to the
runner and waits for an HTTP 2xx ACK. If the runner does not ACK, the
run stays in its current state and you can retry the cancel later.

Any log lines or completion reports the runner sends after a successful
cancel are silently ignored.

### Retention

Terminal runs (`succeeded`, `failed`, `canceled`) and their logs are
kept for 7 days after `finished_at`. After that they are automatically
deleted by the background rotation job.

## Runner HTTP contract

The runner must accept two POST payloads at its dispatch URL.

### Dispatch (POST `<dispatch_url>`)

Headers:
```
Authorization: Bearer <secret_token>
Content-Type: application/json
```

Body:
```json
{
  "kind": "dispatch",
  "run_id": 42,
  "repo": "my-repo",
  "workflow": "unit",
  "script": "go test ./...",
  "container": "golang:1.25",
  "event": "push",
  "callback_url": "https://soft-serve.example.com"
}
```

A 2xx response means the runner acknowledged the dispatch. Anything else
(including network errors) is treated as a failure.

### Cancel (POST `<dispatch_url>/cancel`)

Body:
```json
{
  "kind": "cancel",
  "run_id": 42
}
```

A 2xx response means the runner ACKed the cancel. Soft Serve then
transitions the run to `canceled`.

### Reporter callbacks (POST to Soft Serve)

The runner reports back to Soft Serve using the callback URL it received
in the dispatch payload. All three reporter endpoints require the same
`Authorization: Bearer <secret_token>` header.

**Started:**
```bash
curl -X POST "https://soft-serve.example.com/api/v1/ci/runs/42/started" \
  -H "Authorization: Bearer <secret_token>"
```

**Completion:**
```bash
curl -X POST "https://soft-serve.example.com/api/v1/ci/runs/42/completion" \
  -H "Authorization: Bearer <secret_token>" \
  -H "Content-Type: application/json" \
  -d '{"exit_code": 0}'
```

**Log line:**
```bash
curl -X POST "https://soft-serve.example.com/api/v1/ci/runs/42/logs" \
  -H "Authorization: Bearer <secret_token>" \
  -H "Content-Type: application/json" \
  -d '{"line": "ok github.com/charmbracelet/soft-serve/pkg/ci"}'
```

## Query API

Authenticated clients can read runs and logs over HTTP without being a
runner:

```bash
# List all runs
curl "https://soft-serve.example.com/api/v1/ci/runs"

# Get one run
curl "https://soft-serve.example.com/api/v1/ci/runs/42"

# Get logs
curl "https://soft-serve.example.com/api/v1/ci/runs/42/logs"

# List workflows for a repo
curl "https://soft-serve.example.com/api/v1/ci/workflows?repo=my-repo"
```

Authentication is whatever auth your Soft Serve server requires (same
as the rest of the API).

## Architecture notes

- The CI subsystem is **enabled by default** when you run `soft serve`.
- It reuses the same database you already have (SQLite or Postgres),
  creating tables with the `ci_` prefix.
- Scripts are snapshotted into the run at creation time, so later pushes
  can't change what an in-flight run executes.
- The reference runner is not part of Soft Serve — you bring your own.
  It just needs to speak the simple JSON POST protocol described above.
