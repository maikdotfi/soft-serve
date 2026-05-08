// Package yamlworkflows is a ci.WorkflowSource that reads
// .soft-serve/workflows/*.yml from a repository's HEAD tree and
// parses each file into a ci.WorkflowDefinition.
//
// The workflow YAML schema:
//
//	# .soft-serve/workflows/unit.yml
//	script: |
//	  go test ./...
//	runs_on: linux-amd64
//	container: ghcr.io/charmbracelet/soft-serve-ci:latest  # optional
//	triggers:
//	  - push
//	  - branch_tag_create
//
// The workflow's name is derived from the file name (extension
// stripped). Any parse error is wrapped in ci.ErrWorkflowParse so
// callers can reject the push at the git boundary.
package yamlworkflows

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/charmbracelet/soft-serve/git"
	"github.com/charmbracelet/soft-serve/pkg/backend"
	"github.com/charmbracelet/soft-serve/pkg/ci"
	"gopkg.in/yaml.v3"
)

// MagicFolder is the path inside the repository where workflow files
// are discovered.
const MagicFolder = ".soft-serve/workflows"

// Source implements ci.WorkflowSource against a *backend.Backend.
type Source struct {
	be *backend.Backend
}

var _ ci.WorkflowSource = (*Source)(nil)

// New constructs a Source.
func New(be *backend.Backend) *Source {
	return &Source{be: be}
}

// ParseMagicFolder enumerates and parses every YAML file under
// MagicFolder at the repository's HEAD. A repository with no head
// commit, or no magic folder, yields zero definitions and no error.
// A file that fails to parse causes the whole call to return
// ci.ErrWorkflowParse without producing partial output.
func (s *Source) ParseMagicFolder(ctx context.Context, repoName string) ([]ci.WorkflowDefinition, error) {
	gitRepo, err := s.openGitRepo(ctx, repoName)
	if err != nil {
		return nil, err
	}

	head, err := gitRepo.HEAD()
	if err != nil {
		// Empty repository — no workflows to sync, but also no parse
		// failure. Return an empty slice.
		return nil, nil
	}
	return s.parseAtTree(gitRepo, head.ID)
}

// ParseMagicFolderAtCommit enumerates and parses every YAML file
// under MagicFolder at the given commit SHA. Used at pre-receive
// time when the ref is not yet updated and the gate must validate
// the *incoming* tree. A missing magic folder is treated the same
// as in ParseMagicFolder: no parse failure, zero definitions.
func (s *Source) ParseMagicFolderAtCommit(ctx context.Context, repoName, commitSHA string) ([]ci.WorkflowDefinition, error) {
	gitRepo, err := s.openGitRepo(ctx, repoName)
	if err != nil {
		return nil, err
	}
	return s.parseAtTree(gitRepo, commitSHA)
}

func (s *Source) openGitRepo(ctx context.Context, repoName string) (*git.Repository, error) {
	repo, err := s.be.Repository(ctx, repoName)
	if err != nil {
		return nil, fmt.Errorf("open repository %q: %w", repoName, err)
	}
	g, err := repo.Open()
	if err != nil {
		return nil, fmt.Errorf("open git repo: %w", err)
	}
	return g, nil
}

// parseAtTree walks the magic folder under the given commit's tree
// in the already-opened repository. Returns nil, nil if the magic
// folder is absent (so an empty repo or a repo without workflows is
// indistinguishable from "no parse failure, no definitions").
func (s *Source) parseAtTree(gitRepo *git.Repository, commitSHA string) ([]ci.WorkflowDefinition, error) {
	tree, err := gitRepo.LsTree(commitSHA)
	if err != nil {
		return nil, fmt.Errorf("ls-tree %s: %w", commitSHA, err)
	}

	subtree, err := tree.SubTree(MagicFolder)
	if err != nil {
		// Magic folder missing — also a no-op.
		return nil, nil
	}

	entries, err := subtree.Entries()
	if err != nil {
		return nil, fmt.Errorf("read magic folder: %w", err)
	}

	files := make(map[string][]byte, len(entries))
	for _, entry := range entries {
		if entry.Type() != "blob" {
			continue
		}
		if !isYAMLName(entry.Name()) {
			continue
		}
		bytes, err := entry.Contents()
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", entry.Name(), err)
		}
		files[entry.Name()] = bytes
	}

	return ParseFiles(files)
}

// ParseFiles parses the YAML contents of every file in the input map
// keyed by file name (just the leaf name, not a full path) into
// WorkflowDefinitions. Public so adapters that read files differently
// (tests, future backends) can reuse the parsing logic.
func ParseFiles(files map[string][]byte) ([]ci.WorkflowDefinition, error) {
	defs := make([]ci.WorkflowDefinition, 0, len(files))
	for fileName, content := range files {
		name := strings.TrimSuffix(fileName, path.Ext(fileName))
		def, err := parseDefinition(name, content)
		if err != nil {
			return nil, fmt.Errorf("workflow %q: %w", fileName, err)
		}
		defs = append(defs, def)
	}
	return defs, nil
}

func parseDefinition(name string, content []byte) (ci.WorkflowDefinition, error) {
	var raw struct {
		Script    string   `yaml:"script"`
		RunsOn    string   `yaml:"runs_on"`
		Container *string  `yaml:"container,omitempty"`
		Triggers  []string `yaml:"triggers"`
	}
	if err := yaml.Unmarshal(content, &raw); err != nil {
		return ci.WorkflowDefinition{}, fmt.Errorf("%w: %v", ci.ErrWorkflowParse, err)
	}
	if raw.Script == "" {
		return ci.WorkflowDefinition{}, fmt.Errorf("%w: script is required", ci.ErrWorkflowParse)
	}
	if raw.RunsOn == "" {
		return ci.WorkflowDefinition{}, fmt.Errorf("%w: runs_on is required", ci.ErrWorkflowParse)
	}
	if len(raw.Triggers) == 0 {
		return ci.WorkflowDefinition{}, fmt.Errorf("%w: at least one trigger is required", ci.ErrWorkflowParse)
	}

	triggers := make(map[ci.EventType]bool, len(raw.Triggers))
	for _, value := range raw.Triggers {
		event := ci.EventType(value)
		if !ci.ValidEventTypes[event] {
			return ci.WorkflowDefinition{}, fmt.Errorf("%w: unknown trigger %q", ci.ErrWorkflowParse, value)
		}
		triggers[event] = true
	}

	return ci.WorkflowDefinition{
		Name:      name,
		Script:    raw.Script,
		RunsOn:    raw.RunsOn,
		Container: raw.Container,
		Triggers:  triggers,
	}, nil
}

func isYAMLName(name string) bool {
	ext := strings.ToLower(path.Ext(name))
	return ext == ".yml" || ext == ".yaml"
}

