//go:build integration

package s3_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/soft-serve/pkg/backup/adapters/bundle"
	s3adapter "github.com/charmbracelet/soft-serve/pkg/backup/adapters/s3"
	"github.com/charmbracelet/soft-serve/pkg/backup/adapters/s3/garagetest"
)

// TestS3Adapter_RepoBackup_RoundTrip_Garage exercises the S3 adapter against a
// real S3-compatible target (Garage). Skipped unless the GARAGE_* env is set;
// see `make garage-up`.
func TestS3Adapter_RepoBackup_RoundTrip_Garage(t *testing.T) {
	cfg := garagetest.RequireBucket(t)

	adapter, err := s3adapter.NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const (
		repoName = "octocat/hello-world"
		backupID = int64(42)
	)
	want := []byte("garage round-trip payload")

	if err := adapter.UploadRepoBackup(ctx, repoName, backupID, want); err != nil {
		t.Fatalf("UploadRepoBackup: %v", err)
	}

	got, err := adapter.DownloadRepoBackup(ctx, repoName, backupID)
	if err != nil {
		t.Fatalf("DownloadRepoBackup: %v", err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("downloaded bytes differ: got %q, want %q", got, want)
	}

	if err := adapter.DeleteRepoBackup(ctx, repoName, backupID); err != nil {
		t.Fatalf("DeleteRepoBackup: %v", err)
	}

	if _, err := adapter.DownloadRepoBackup(ctx, repoName, backupID); err == nil {
		t.Fatal("expected DownloadRepoBackup to fail after delete, got nil")
	}
}

// TestGitBundleRoundTrip_Garage is the end-to-end integration test: creates a
// real bare git repo with commits, bundles it through GitBundleProvider,
// uploads the bundle to Garage via the S3 adapter, downloads it back, restores
// it through GitBundleProvider into a fresh repo, and verifies the restored
// repo has the same refs and commit history.
func TestGitBundleRoundTrip_Garage(t *testing.T) {
	s3Cfg := garagetest.RequireBucket(t)

	s3Adapter, err := s3adapter.NewAdapter(s3Cfg)
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}

	// ---- Set up a temporary data directory for repos ----
	dataDir := t.TempDir()

	// ---- Create a source bare repo with real commits ----
	const repoName = "testorg/myproject"
	repoPath := filepath.Join(dataDir, "repos", repoName+".git")
	if err := os.MkdirAll(filepath.Dir(repoPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := gitCmd(dataDir, "init", "--bare", repoPath); err != nil {
		t.Fatalf("git init --bare: %v", err)
	}

	// We need a working tree to make commits, then push them into the bare repo.
	workTree := t.TempDir()
	if err := gitCmd(workTree, "init"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if err := gitCmd(workTree, "remote", "add", "origin", repoPath); err != nil {
		t.Fatalf("git remote add: %v", err)
	}

	// Make initial commit on main.
	writeFile(t, filepath.Join(workTree, "README.md"), "# My Project\nHello from integration!\n")
	if err := gitCmd(workTree, "add", "README.md"); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if err := gitCmd(workTree, "-c", "user.email=test@test.com", "-c", "user.name=Test", "commit", "-m", "initial commit"); err != nil {
		t.Fatalf("git commit: %v", err)
	}
	if err := gitCmd(workTree, "push", "-u", "origin", "HEAD:refs/heads/main"); err != nil {
		t.Fatalf("git push main: %v", err)
	}

	// Add a second commit.
	writeFile(t, filepath.Join(workTree, "hello.txt"), "world\n")
	if err := gitCmd(workTree, "add", "hello.txt"); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if err := gitCmd(workTree, "-c", "user.email=test@test.com", "-c", "user.name=Test", "commit", "-m", "add hello.txt"); err != nil {
		t.Fatalf("git commit: %v", err)
	}
	if err := gitCmd(workTree, "push", "origin", "HEAD:refs/heads/main"); err != nil {
		t.Fatalf("git push: %v", err)
	}

	// Create a feature branch too.
	if err := gitCmd(workTree, "checkout", "-b", "feature/x"); err != nil {
		t.Fatalf("git checkout -b: %v", err)
	}
	writeFile(t, filepath.Join(workTree, "feature.txt"), "experimental\n")
	if err := gitCmd(workTree, "add", "feature.txt"); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if err := gitCmd(workTree, "-c", "user.email=test@test.com", "-c", "user.name=Test", "commit", "-m", "add feature"); err != nil {
		t.Fatalf("git commit: %v", err)
	}
	if err := gitCmd(workTree, "push", "-u", "origin", "feature/x"); err != nil {
		t.Fatalf("git push feature/x: %v", err)
	}

	// ---- Record the original repo's refs and commits ----
	originalRefs := gitRefs(t, repoPath)
	originalCommits := gitLog(t, repoPath, "main")

	// ---- Bundle the repo ----
	bundler := bundle.NewGitBundleProvider(dataDir)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const backupID = int64(1)

	bundleContent, err := bundler.CreateBundle(ctx, repoName)
	if err != nil {
		t.Fatalf("CreateBundle: %v", err)
	}
	if len(bundleContent) == 0 {
		t.Fatal("CreateBundle returned empty bundle")
	}
	t.Logf("bundle size: %d bytes", len(bundleContent))

	// ---- Upload the bundle to Garage ----
	if err := s3Adapter.UploadRepoBackup(ctx, repoName, backupID, bundleContent); err != nil {
		t.Fatalf("UploadRepoBackup: %v", err)
	}

	// ---- Download the bundle back ----
	downloaded, err := s3Adapter.DownloadRepoBackup(ctx, repoName, backupID)
	if err != nil {
		t.Fatalf("DownloadRepoBackup: %v", err)
	}
	if !bytes.Equal(bundleContent, downloaded) {
		t.Fatalf("downloaded bundle differs from uploaded bundle: got %d bytes, want %d bytes", len(downloaded), len(bundleContent))
	}

	// ---- Remove the original repo (simulates a disaster) ----
	if err := os.RemoveAll(repoPath); err != nil {
		t.Fatalf("removing original repo: %v", err)
	}

	// ---- Restore the repo from the downloaded bundle ----
	if err := bundler.RestoreFromBundle(ctx, repoName, downloaded); err != nil {
		t.Fatalf("RestoreFromBundle: %v", err)
	}

	// ---- Verify the restored repo ----
	if _, err := os.Stat(repoPath); err != nil {
		t.Fatalf("restored repo path does not exist: %v", err)
	}

	restoredRefs := gitRefs(t, repoPath)
	if !refSetsEqual(originalRefs, restoredRefs) {
		t.Fatalf("refs mismatch after restore:\n  original: %v\n  restored: %v", originalRefs, restoredRefs)
	}

	restoredCommits := gitLog(t, repoPath, "main")
	if originalCommits != restoredCommits {
		t.Fatalf("commit history mismatch after restore:\n  original:\n%s\n  restored:\n%s", originalCommits, restoredCommits)
	}

	// ---- Cleanup: remove the backup from S3 ----
	if err := s3Adapter.DeleteRepoBackup(ctx, repoName, backupID); err != nil {
		t.Fatalf("DeleteRepoBackup: %v", err)
	}
}

// gitCmd runs a git command in the given working directory.
func gitCmd(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return nil
}

// gitRefs returns the sorted list of refs (sha refname) in the bare repo.
func gitRefs(t *testing.T, bareRepo string) []string {
	t.Helper()
	cmd := exec.Command("git", "-C", bareRepo, "for-each-ref", "--format=%(objectname) %(refname)")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git for-each-ref: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	sort.Strings(lines)
	return lines
}

// gitLog returns the oneline log for the given branch in the bare repo.
func gitLog(t *testing.T, bareRepo string, branch string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", bareRepo, "log", "--oneline", branch)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git log %s: %v", branch, err)
	}
	return strings.TrimSpace(string(out))
}

// refSetsEqual checks whether two sorted ref slices are identical.
func refSetsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// writeFile writes data to path, creating any necessary parent directories.
func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
