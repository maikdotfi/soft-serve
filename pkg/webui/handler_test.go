package webui_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/soft-serve/pkg/webui"
	"github.com/charmbracelet/soft-serve/pkg/webui/repobrowser"
	"github.com/charmbracelet/soft-serve/pkg/webui/repobrowser/fake"
)

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	updated := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	browser := fake.New([]*fake.Repo{
		{
			Info: repobrowser.RepoInfo{
				Name:          "alpha",
				ProjectName:   "Alpha Project",
				Description:   "first test repo",
				DefaultBranch: "main",
				UpdatedAt:     updated,
			},
			Files: map[string][]byte{
				"README.md":   []byte("# Alpha\n\nhello world\n"),
				"src/main.go": []byte("package main\n\nfunc main() {}\n"),
				"src/util.go": []byte("package main\n"),
			},
			Commits: []repobrowser.CommitInfo{
				{Hash: "c0ffee1c0ffee1c0ffee1", Author: "Ada", AuthorEmail: "ada@example.com", When: updated, Subject: "initial commit"},
			},
			Refs: []repobrowser.RefInfo{
				{Name: "main", Kind: repobrowser.RefBranch, Hash: "c0ffee1"},
				{Name: "v0.1.0", Kind: repobrowser.RefTag, Hash: "c0ffee1"},
			},
		},
		{
			Info: repobrowser.RepoInfo{
				Name:          "beta",
				ProjectName:   "Beta",
				Description:   "second test repo",
				DefaultBranch: "main",
				UpdatedAt:     updated.Add(-24 * time.Hour),
			},
			Files: map[string][]byte{"hello.txt": []byte("beta\n")},
		},
	})
	h, err := webui.NewHandler(browser)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	return h
}

func get(t *testing.T, h http.Handler, path string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec, rec.Body.String()
}

func TestIndex_ListsRepos(t *testing.T) {
	h := newTestHandler(t)
	rec, body := get(t, h, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(body, "alpha") {
		t.Errorf("body missing 'alpha':\n%s", body)
	}
	if !strings.Contains(body, "beta") {
		t.Errorf("body missing 'beta'")
	}
	if !strings.Contains(body, "first test repo") {
		t.Errorf("body missing alpha description")
	}
}

func TestRepoOverview_ShowsTreeAndCommits(t *testing.T) {
	h := newTestHandler(t)
	rec, body := get(t, h, "/r/alpha")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	for _, want := range []string{"alpha", "README.md", "src", "initial commit"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func TestRepoOverview_UnknownRepo_404(t *testing.T) {
	h := newTestHandler(t)
	rec, _ := get(t, h, "/r/nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestTreeView_RootAndSubdir(t *testing.T) {
	h := newTestHandler(t)

	rec, body := get(t, h, "/r/alpha/tree/main/")
	if rec.Code != http.StatusOK {
		t.Fatalf("root tree status = %d", rec.Code)
	}
	if !strings.Contains(body, "README.md") || !strings.Contains(body, "src") {
		t.Errorf("root tree missing entries:\n%s", body)
	}

	rec, body = get(t, h, "/r/alpha/tree/main/src")
	if rec.Code != http.StatusOK {
		t.Fatalf("src tree status = %d", rec.Code)
	}
	if !strings.Contains(body, "main.go") || !strings.Contains(body, "util.go") {
		t.Errorf("src tree missing entries:\n%s", body)
	}
}

func TestBlobView_ShowsContent(t *testing.T) {
	h := newTestHandler(t)
	rec, body := get(t, h, "/r/alpha/blob/main/README.md")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(body, "hello world") {
		t.Errorf("body missing 'hello world':\n%s", body)
	}
}

func TestBlobView_UnknownPath_404(t *testing.T) {
	h := newTestHandler(t)
	rec, _ := get(t, h, "/r/alpha/blob/main/nope.txt")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestLogView_ShowsCommits(t *testing.T) {
	h := newTestHandler(t)
	rec, body := get(t, h, "/r/alpha/log/main")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	for _, want := range []string{"initial commit", "Ada", "c0ffee1"} {
		if !strings.Contains(body, want) {
			t.Errorf("log missing %q", want)
		}
	}
}

func TestStaticAssets_ServesCSS(t *testing.T) {
	h := newTestHandler(t)
	rec, body := get(t, h, "/static/style.css")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(body, "--phosphor") {
		t.Errorf("style.css missing --phosphor design token")
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "css") {
		t.Errorf("Content-Type = %q, want css", ct)
	}
}
