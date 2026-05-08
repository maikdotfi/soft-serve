// Package httpdispatch is a ci.RunnerDispatcher that POSTs dispatch
// and cancel webhooks to a registered runner over HTTP.
//
// Wire format
//
// Dispatch (POST <registration.DispatchURL>):
//
//	{
//	  "kind": "dispatch",
//	  "run_id": <int64>,
//	  "repo": "<repo name>",
//	  "workflow": "<workflow name>",
//	  "script": "<shell script body>",
//	  "container": "<image>" | null,
//	  "event": "<event type>",
//	  "callback_url": "<base URL the runner should report back to>"
//	}
//
// Cancel (POST <registration.DispatchURL>/cancel):
//
//	{
//	  "kind": "cancel",
//	  "run_id": <int64>
//	}
//
// Both requests carry "Authorization: Bearer <secret_token>" so the
// runner can verify the dispatcher identity.
//
// A runner is considered to have ACKed iff the response status is in
// the 2xx range. Any other outcome (including a network error) is
// returned as an error and treated by the service as a dispatch
// failure.
package httpdispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/charmbracelet/soft-serve/pkg/ci"
)

// Dispatcher implements ci.RunnerDispatcher by POSTing to the runner's
// dispatch_url. CallbackURL is the public base URL the runner should
// report back to; it is included in every dispatch payload so the
// runner does not need to be configured separately.
type Dispatcher struct {
	client      *http.Client
	callbackURL string
}

var _ ci.RunnerDispatcher = (*Dispatcher)(nil)

// New constructs a Dispatcher. If client is nil, a default client
// with a 30s timeout is used.
func New(client *http.Client, callbackURL string) *Dispatcher {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &Dispatcher{client: client, callbackURL: callbackURL}
}

// DispatchRun POSTs a dispatch payload to the runner.
func (d *Dispatcher) DispatchRun(ctx context.Context, registration ci.RunnerRegistration, run ci.Run) error {
	payload := dispatchPayload{
		Kind:        "dispatch",
		RunID:       run.ID,
		Repo:        run.RepoName,
		Workflow:    run.WorkflowName,
		Script:      run.Script,
		Container:   run.Container,
		Event:       string(run.TriggeredByEvent),
		CallbackURL: d.callbackURL,
	}
	return d.post(ctx, registration.DispatchURL, registration.SecretToken, payload)
}

// CancelRun POSTs a cancel payload to the runner. The cancel endpoint
// is the registration's dispatch URL with "/cancel" appended.
func (d *Dispatcher) CancelRun(ctx context.Context, registration ci.RunnerRegistration, run ci.Run) error {
	cancelURL := strings.TrimRight(registration.DispatchURL, "/") + "/cancel"
	payload := cancelPayload{Kind: "cancel", RunID: run.ID}
	return d.post(ctx, cancelURL, registration.SecretToken, payload)
}

func (d *Dispatcher) post(ctx context.Context, url, token string, body any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("post %s: %w", url, err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Read a bounded amount of the body so the error mentions
		// what the runner actually said without holding the
		// connection open indefinitely.
		excerpt, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("runner returned %d: %s", resp.StatusCode, strings.TrimSpace(string(excerpt)))
	}
	return nil
}

type dispatchPayload struct {
	Kind        string  `json:"kind"`
	RunID       int64   `json:"run_id"`
	Repo        string  `json:"repo"`
	Workflow    string  `json:"workflow"`
	Script      string  `json:"script"`
	Container   *string `json:"container"`
	Event       string  `json:"event"`
	CallbackURL string  `json:"callback_url"`
}

type cancelPayload struct {
	Kind  string `json:"kind"`
	RunID int64  `json:"run_id"`
}
