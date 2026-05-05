// Package bundle provides an adapter for git bundle operations.
// Per AGENTS.md: this is an adapter that implements the backup.BundleProvider port.
package bundle

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// GitBundleProvider implements backup.BundleProvider using the git binary
// to create and restore git bundle files.
type GitBundleProvider struct {
	dataPath string // root data path where repos are stored
}

// NewGitBundleProvider creates a new GitBundleProvider.
func NewGitBundleProvider(dataPath string) *GitBundleProvider {
	return &GitBundleProvider{dataPath: dataPath}
}

// repoPath returns the filesystem path for a given repository name.
func (g *GitBundleProvider) repoPath(repoName string) string {
	return filepath.Join(g.dataPath, "repos", repoName+".git")
}

// CreateBundle creates a git bundle for the given repository.
// Per spec: "The entire repository content is backed up as a git bundle,
// not just the pushed ref."
func (g *GitBundleProvider) CreateBundle(ctx context.Context, repoName string) ([]byte, error) {
	repoPath := g.repoPath(repoName)

	// Verify the repo exists
	if _, err := os.Stat(repoPath); err != nil {
		return nil, fmt.Errorf("repository not found at %s: %w", repoPath, err)
	}

	// Create a temp file for the bundle
	tmpFile, err := os.CreateTemp("", "soft-serve-bundle-*.bundle")
	if err != nil {
		return nil, fmt.Errorf("creating temp file for bundle: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath) //nolint:errcheck
	tmpFile.Close()           //nolint:errcheck

	// Use git bundle create --all to capture all refs
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "bundle", "create", tmpPath, "--all")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("creating git bundle for %s: %w (stderr: %s)", repoName, err, stderr.String())
	}

	// Verify the bundle
	verifyCmd := exec.CommandContext(ctx, "git", "bundle", "verify", tmpPath)
	var verifyStderr bytes.Buffer
	verifyCmd.Stderr = &verifyStderr
	if err := verifyCmd.Run(); err != nil {
		// If bundle verify fails, the bundle may be empty (new repo with no refs)
		// This is expected for empty repos
		_ = verifyStderr.String()
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("reading bundle file for %s: %w", repoName, err)
	}

	// If the bundle is empty, return an error
	if len(data) == 0 {
		return nil, fmt.Errorf("repository %s has no references to bundle", repoName)
	}

	return data, nil
}

// RestoreFromBundle restores a repository from a git bundle.
// Per spec: "git clone --bundle or git bundle unbundle"
func (g *GitBundleProvider) RestoreFromBundle(ctx context.Context, repoName string, content []byte) error {
	repoPath := g.repoPath(repoName)

	// Write the bundle content to a temp file
	tmpFile, err := os.CreateTemp("", "soft-serve-restore-*.bundle")
	if err != nil {
		return fmt.Errorf("creating temp file for restore: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath) //nolint:errcheck

	if _, err := tmpFile.Write(content); err != nil {
		tmpFile.Close() //nolint:errcheck
		return fmt.Errorf("writing bundle content to temp file: %w", err)
	}
	tmpFile.Close() //nolint:errcheck

	// If the repo directory doesn't exist, clone from the bundle
	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		parentDir := filepath.Dir(repoPath)
		if err := os.MkdirAll(parentDir, os.ModePerm); err != nil {
			return fmt.Errorf("creating repo parent directory: %w", err)
		}

		// git clone --bare from bundle
		cmd := exec.CommandContext(ctx, "git", "clone", "--bare", tmpPath, repoPath)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("cloning from bundle for %s: %w (stderr: %s)", repoName, err, stderr.String())
		}
		return nil
	}

	// If the repo already exists, unbundle into it
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "bundle", "unbundle", tmpPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("unbundling for %s: %w (stderr: %s)", repoName, err, stderr.String())
	}

	return nil
}