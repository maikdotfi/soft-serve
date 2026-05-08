package httpdispatch_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/soft-serve/pkg/ci"
	"github.com/charmbracelet/soft-serve/pkg/ci/adapters/httpdispatch"
)

func TestDispatcher_DispatchAndCancelHitExpectedURLsWithBearer(t *testing.T) {
	var (
		mu        sync.Mutex
		captured  []capturedRequest
		respondOK = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) }
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		captured = append(captured, capturedRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Auth:   r.Header.Get("Authorization"),
			Body:   string(body),
		})
		mu.Unlock()
		respondOK(w, r)
	}))
	defer server.Close()

	dispatcher := httpdispatch.New(nil, "https://soft-serve.example/api/runs")
	registration := ci.RunnerRegistration{
		Name:        "linux-amd64",
		DispatchURL: server.URL + "/dispatch",
		SecretToken: "tok-123",
	}
	container := "ubuntu:24.04"
	run := ci.Run{
		ID:               42,
		RepoName:         "repo",
		WorkflowName:     "unit",
		Script:           "go test ./...",
		RunsOn:           "linux-amd64",
		Container:        &container,
		TriggeredByEvent: ci.EventTypePush,
	}

	if err := dispatcher.DispatchRun(context.Background(), registration, run); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if err := dispatcher.CancelRun(context.Background(), registration, run); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(captured) != 2 {
		t.Fatalf("captured = %d, want 2", len(captured))
	}
	dispatch := captured[0]
	if dispatch.Method != http.MethodPost || dispatch.Path != "/dispatch" {
		t.Fatalf("dispatch req = %+v", dispatch)
	}
	if dispatch.Auth != "Bearer tok-123" {
		t.Fatalf("dispatch auth = %q", dispatch.Auth)
	}
	var dispatchBody map[string]any
	if err := json.Unmarshal([]byte(dispatch.Body), &dispatchBody); err != nil {
		t.Fatalf("dispatch body parse: %v", err)
	}
	if dispatchBody["run_id"].(float64) != 42 || dispatchBody["script"] != "go test ./..." {
		t.Fatalf("dispatch payload = %#v", dispatchBody)
	}
	if dispatchBody["callback_url"] != "https://soft-serve.example/api/runs" {
		t.Fatalf("callback url = %v", dispatchBody["callback_url"])
	}

	cancel := captured[1]
	if cancel.Path != "/dispatch/cancel" {
		t.Fatalf("cancel path = %q, want /dispatch/cancel", cancel.Path)
	}
}

func TestDispatcher_NonSuccessStatusBecomesError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "runner is busy", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	dispatcher := httpdispatch.New(nil, "https://soft-serve.example/api/runs")
	err := dispatcher.DispatchRun(context.Background(), ci.RunnerRegistration{
		DispatchURL: server.URL,
		SecretToken: "tok",
	}, ci.Run{ID: 1})
	if err == nil {
		t.Fatal("expected error for 503 response")
	}
	if !strings.Contains(err.Error(), "503") || !strings.Contains(err.Error(), "runner is busy") {
		t.Fatalf("error = %v, want to mention 503 + body", err)
	}
}

type capturedRequest struct {
	Method string
	Path   string
	Auth   string
	Body   string
}
