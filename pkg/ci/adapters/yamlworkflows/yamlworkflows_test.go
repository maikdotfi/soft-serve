package yamlworkflows_test

import (
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/charmbracelet/soft-serve/pkg/ci"
	"github.com/charmbracelet/soft-serve/pkg/ci/adapters/yamlworkflows"
)

func TestParseFiles_NameDerivedFromFileNameAndAllFieldsRoundTrip(t *testing.T) {
	files := map[string][]byte{
		"unit.yml": []byte(`
script: |
  go test ./...
runs_on: linux-amd64
container: ghcr.io/charmbracelet/soft-serve-ci:latest
triggers:
  - push
`),
		"release.yaml": []byte(`
script: go build
runs_on: linux-arm64
triggers:
  - repository
  - branch_tag_create
`),
	}
	defs, err := yamlworkflows.ParseFiles(files)
	if err != nil {
		t.Fatalf("parse files: %v", err)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })
	if len(defs) != 2 {
		t.Fatalf("def count = %d, want 2", len(defs))
	}
	if defs[1].Name != "unit" || !strings.Contains(defs[1].Script, "go test") {
		t.Fatalf("unit def = %#v", defs[1])
	}
	if defs[1].Container == nil || *defs[1].Container != "ghcr.io/charmbracelet/soft-serve-ci:latest" {
		t.Fatalf("unit container = %v", defs[1].Container)
	}
	if !defs[1].Triggers[ci.EventTypePush] {
		t.Fatalf("unit triggers = %v", defs[1].Triggers)
	}
	if defs[0].Name != "release" || defs[0].Container != nil {
		t.Fatalf("release def = %#v", defs[0])
	}
	if !defs[0].Triggers[ci.EventTypeRepository] || !defs[0].Triggers[ci.EventTypeBranchTagCreate] {
		t.Fatalf("release triggers = %v", defs[0].Triggers)
	}
}

func TestParseFiles_RejectsUnknownTriggerWithErrWorkflowParse(t *testing.T) {
	_, err := yamlworkflows.ParseFiles(map[string][]byte{
		"bad.yml": []byte(`
script: ok
runs_on: linux-amd64
triggers:
  - pull_request
`),
	})
	if !errors.Is(err, ci.ErrWorkflowParse) {
		t.Fatalf("err = %v, want ErrWorkflowParse", err)
	}
}

func TestParseFiles_RejectsMalformedYAML(t *testing.T) {
	_, err := yamlworkflows.ParseFiles(map[string][]byte{
		"bad.yml": []byte("script: [unbalanced"),
	})
	if !errors.Is(err, ci.ErrWorkflowParse) {
		t.Fatalf("err = %v, want ErrWorkflowParse", err)
	}
}

func TestParseFiles_RequiresScriptRunsOnAndTriggers(t *testing.T) {
	tests := []struct {
		name, body string
	}{
		{"missing script", "runs_on: linux\ntriggers: [push]\n"},
		{"missing runs_on", "script: ok\ntriggers: [push]\n"},
		{"missing triggers", "script: ok\nruns_on: linux\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := yamlworkflows.ParseFiles(map[string][]byte{"x.yml": []byte(tt.body)})
			if !errors.Is(err, ci.ErrWorkflowParse) {
				t.Fatalf("err = %v, want ErrWorkflowParse", err)
			}
		})
	}
}
