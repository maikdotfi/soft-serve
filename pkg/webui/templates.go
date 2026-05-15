package webui

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/soft-serve/pkg/webui/backupbrowser"
	"github.com/dustin/go-humanize"
)

// pageTemplate is one fully-composed page (layout + a "page" template).
type pageTemplate struct {
	tmpl *template.Template
}

// loadTemplates parses every page template once at construction. Each page
// is composed with the shared layout and the shared "_repoheader",
// "_treelist" and "_commitlist" partials defined by repo.html. We achieve
// that by parsing the layout + partials together with the page-specific
// definition of "page".
func loadTemplates(fsys embed.FS) (map[string]*pageTemplate, error) {
	pages := []string{"repos", "repo", "tree", "blob", "log", "backups", "tasks", "error"}
	out := make(map[string]*pageTemplate, len(pages))

	// Files that contribute partials shared across pages.
	commonFiles := []string{"templates/layout.html", "templates/repo.html"}

	for _, name := range pages {
		t := template.New(name).Funcs(templateFuncs())

		// Add layout + repo.html (which carries _repoheader, _treelist,
		// _commitlist). For the page being rendered, parse its own file
		// last so its "page" definition wins.
		filesToParse := append([]string{}, commonFiles...)
		pageFile := "templates/" + name + ".html"
		if !contains(filesToParse, pageFile) {
			filesToParse = append(filesToParse, pageFile)
		} else {
			// Move pageFile to the end so its "page" overrides repo.html's.
			filesToParse = removeString(filesToParse, pageFile)
			filesToParse = append(filesToParse, pageFile)
		}

		t, err := parseFromFS(t, fsys, filesToParse...)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		out[name] = &pageTemplate{tmpl: t}
	}
	return out, nil
}

func parseFromFS(t *template.Template, fsys embed.FS, files ...string) (*template.Template, error) {
	for _, f := range files {
		b, err := fs.ReadFile(fsys, f)
		if err != nil {
			return nil, err
		}
		if _, err := t.Parse(string(b)); err != nil {
			return nil, fmt.Errorf("parse %s: %w", f, err)
		}
	}
	return t, nil
}

// templateFuncs are the FuncMap available to every template.
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"add":               func(a, b int) int { return a + b },
		"shortdate":         shortDate,
		"shorthash":         shortHash,
		"humansize":         humanSize,
		"datetime":          dateTime,
		"backupstatusclass": backupStatusClass,
		"linenumbered":      lineNumbered,
		"basename":          filepath.Base,
		"dirname": func(p string) string {
			d := filepath.Dir(p)
			if d == "." {
				return ""
			}
			return d
		},
	}
}

func shortDate(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	if time.Since(t) < 365*24*time.Hour {
		return humanize.Time(t)
	}
	return t.Format("2006-01-02")
}

func shortHash(h string) string {
	if len(h) <= 7 {
		return h
	}
	return h[:7]
}

func humanSize(n int64) string {
	if n <= 0 {
		return "—"
	}
	return humanize.IBytes(uint64(n))
}

func dateTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.UTC().Format("2006-01-02 15:04 UTC")
}

func backupStatusClass(status backupbrowser.Status) string {
	switch status {
	case backupbrowser.StatusStored:
		return "tag--branch"
	case backupbrowser.StatusFailed:
		return "tag--fail"
	default:
		return "tag--warn"
	}
}

// lineNumbered returns HTML-safe gutter-numbered code from a byte slice.
// Each rendered line is wrapped so CSS can style hover state per row.
func lineNumbered(b []byte) template.HTML {
	if len(b) == 0 {
		return template.HTML(`<span class="ln-row"><span class="ln">1</span></span>`)
	}
	src := string(b)
	src = strings.ReplaceAll(src, "\r\n", "\n")
	lines := strings.Split(src, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	var sb strings.Builder
	width := digitWidth(len(lines))
	for i, line := range lines {
		fmt.Fprintf(&sb, `<span class="ln-row"><span class="ln">%*d</span>%s</span>`+"\n",
			width, i+1, template.HTMLEscapeString(line))
	}
	return template.HTML(sb.String())
}

func digitWidth(n int) int {
	if n <= 0 {
		return 1
	}
	w := 0
	for n > 0 {
		w++
		n /= 10
	}
	return w
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func removeString(xs []string, s string) []string {
	out := xs[:0]
	for _, x := range xs {
		if x != s {
			out = append(out, x)
		}
	}
	return out
}
