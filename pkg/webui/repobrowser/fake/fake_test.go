package fake_test

import (
	"testing"

	"github.com/charmbracelet/soft-serve/pkg/webui/repobrowser"
	"github.com/charmbracelet/soft-serve/pkg/webui/repobrowser/fake"
	"github.com/charmbracelet/soft-serve/pkg/webui/repobrowser/repobrowsertest"
)

func TestFake_Contract(t *testing.T) {
	repobrowsertest.RunContract(t, func(_ *testing.T, fx repobrowsertest.Fixture) repobrowser.Browser {
		repos := make([]*fake.Repo, 0, len(fx.Repos))
		for _, r := range fx.Repos {
			repos = append(repos, &fake.Repo{
				Info: repobrowser.RepoInfo{
					Name:          r.Name,
					ProjectName:   r.ProjectName,
					Description:   r.Description,
					DefaultBranch: r.DefaultBranch,
					UpdatedAt:     r.UpdatedAt,
				},
				Files:   r.Files,
				Commits: r.Commits,
				Refs:    r.Refs,
			})
		}
		return fake.New(repos)
	})
}
