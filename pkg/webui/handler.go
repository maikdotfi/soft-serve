package webui

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/soft-serve/pkg/webui/backupbrowser"
	"github.com/charmbracelet/soft-serve/pkg/webui/repobrowser"
	"github.com/charmbracelet/soft-serve/pkg/webui/workitembrowser"
	"github.com/gorilla/mux"
)

// Handler is the repository browser.
type Handler struct {
	browser      repobrowser.Browser
	backups      backupbrowser.Reader
	workItems    workitembrowser.Reader
	basePath     string
	maxBlobBytes int64
	tmpls        map[string]*pageTemplate
	staticFS     fs.FS
}

// pageData is the common envelope every page receives. Page-specific data
// is added with the embedded fields below.
type pageData struct {
	Title        string
	BasePath     string
	RepoCount    int
	Now          string
	HasBackups   bool
	HasWorkItems bool

	// Repo-scoped fields populated for repo/tree/blob/log pages.
	Repo       repobrowser.RepoInfo
	Ref        string
	Path       string
	ParentPath string
	Dir        string

	// Page-specific payload.
	Repos           []repobrowser.RepoInfo
	Tree            []repobrowser.TreeEntry
	Blob            repobrowser.Blob
	Commits         []repobrowser.CommitInfo
	Backup          backupbrowser.Overview
	WorkItemLanes   []workItemLaneData
	WorkItemAPIPath string

	// error.html only.
	Code   string
	Detail string
}

type workItemLaneData struct {
	Lane  workitembrowser.Lane
	Items []workitembrowser.WorkItem
}

func (h *Handler) routes() http.Handler {
	r := mux.NewRouter()

	r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.FS(h.staticFS))))

	r.HandleFunc("/", h.handleIndex).Methods(http.MethodGet)
	if h.backups != nil {
		r.HandleFunc("/backups", h.handleBackups).Methods(http.MethodGet)
	}
	r.HandleFunc("/r/{name}", h.handleRepo).Methods(http.MethodGet)
	if h.workItems != nil {
		r.HandleFunc("/r/{name}/tasks", h.handleRepoTasks).Methods(http.MethodGet)
	}
	r.HandleFunc("/r/{name}/tree/{ref}", h.handleTree).Methods(http.MethodGet)
	r.HandleFunc("/r/{name}/tree/{ref}/{path:.*}", h.handleTree).Methods(http.MethodGet)
	r.HandleFunc("/r/{name}/blob/{ref}/{path:.*}", h.handleBlob).Methods(http.MethodGet)
	r.HandleFunc("/r/{name}/log/{ref}", h.handleLog).Methods(http.MethodGet)

	r.NotFoundHandler = http.HandlerFunc(h.handleNotFound)

	return r
}

// ---------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------

func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	repos, err := h.browser.ListRepos(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	data := h.envelope("index", len(repos))
	data.Repos = repos
	h.render(w, r, "repos", data)
}

func (h *Handler) handleBackups(w http.ResponseWriter, r *http.Request) {
	if h.backups == nil {
		h.notFound(w, r, "page not found", r.URL.Path)
		return
	}
	overview, err := h.backups.Overview(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	data := h.envelope("backups", h.repoCount(r.Context()))
	data.Backup = overview
	h.render(w, r, "backups", data)
}

func (h *Handler) handleRepo(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	info, ok := h.lookupRepo(w, r, name)
	if !ok {
		return
	}
	ref := refOrDefault(info, "")

	tree, err := h.browser.ListTree(r.Context(), name, ref, "")
	if err != nil && !errors.Is(err, repobrowser.ErrPathNotFound) {
		h.serverError(w, r, err)
		return
	}
	commits, err := h.browser.ListCommits(r.Context(), name, ref, 1, 10)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	repoCount := h.repoCount(r.Context())

	data := h.envelope(name, repoCount)
	data.Repo = info
	data.Ref = ref
	data.Tree = tree
	data.Commits = commits
	h.render(w, r, "repo", data)
}

func (h *Handler) handleRepoTasks(w http.ResponseWriter, r *http.Request) {
	if h.workItems == nil {
		h.notFound(w, r, "page not found", r.URL.Path)
		return
	}
	name := mux.Vars(r)["name"]
	info, ok := h.lookupRepo(w, r, name)
	if !ok {
		return
	}
	items, err := h.workItems.ListByRepo(r.Context(), name)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	data := h.envelope(name+" tasks", h.repoCount(r.Context()))
	data.Repo = info
	data.Ref = refOrDefault(info, "")
	data.WorkItemLanes = groupWorkItemsByLane(items)
	data.WorkItemAPIPath = "/api/v1/repos/" + name + "/work-items"
	h.render(w, r, "tasks", data)
}

func (h *Handler) handleTree(w http.ResponseWriter, r *http.Request) {
	v := mux.Vars(r)
	name, ref, p := v["name"], v["ref"], cleanPath(v["path"])
	info, ok := h.lookupRepo(w, r, name)
	if !ok {
		return
	}
	ref = refOrDefault(info, ref)

	tree, err := h.browser.ListTree(r.Context(), name, ref, p)
	if err != nil {
		if errors.Is(err, repobrowser.ErrPathNotFound) {
			h.notFound(w, r, "path not found", p)
			return
		}
		h.serverError(w, r, err)
		return
	}

	data := h.envelope(name+"/"+p, h.repoCount(r.Context()))
	data.Repo = info
	data.Ref = ref
	data.Path = p
	data.ParentPath = parentDir(p)
	data.Tree = tree
	h.render(w, r, "tree", data)
}

func groupWorkItemsByLane(items []workitembrowser.WorkItem) []workItemLaneData {
	lanes := []workItemLaneData{
		{Lane: workitembrowser.LaneBacklog},
		{Lane: workitembrowser.LaneWIP},
		{Lane: workitembrowser.LaneDone},
	}
	index := map[workitembrowser.Lane]int{
		workitembrowser.LaneBacklog: 0,
		workitembrowser.LaneWIP:     1,
		workitembrowser.LaneDone:    2,
	}
	for _, item := range items {
		i, ok := index[item.Lane]
		if !ok {
			continue
		}
		lanes[i].Items = append(lanes[i].Items, item)
	}
	return lanes
}

func (h *Handler) handleBlob(w http.ResponseWriter, r *http.Request) {
	v := mux.Vars(r)
	name, ref, p := v["name"], v["ref"], cleanPath(v["path"])
	info, ok := h.lookupRepo(w, r, name)
	if !ok {
		return
	}
	ref = refOrDefault(info, ref)

	blob, err := h.browser.ReadBlob(r.Context(), name, ref, p, h.maxBlobBytes)
	if err != nil {
		switch {
		case errors.Is(err, repobrowser.ErrPathNotFound):
			h.notFound(w, r, "file not found", p)
		case errors.Is(err, repobrowser.ErrNotAFile):
			http.Redirect(w, r, h.basePath+"/r/"+name+"/tree/"+ref+"/"+p, http.StatusSeeOther)
		default:
			h.serverError(w, r, err)
		}
		return
	}

	dir := parentDir(p)
	tree, _ := h.browser.ListTree(r.Context(), name, ref, dir)

	data := h.envelope(name+"/"+p, h.repoCount(r.Context()))
	data.Repo = info
	data.Ref = ref
	data.Path = p
	data.Dir = dir
	data.ParentPath = parentDir(dir)
	data.Blob = blob
	data.Tree = tree
	h.render(w, r, "blob", data)
}

func (h *Handler) handleLog(w http.ResponseWriter, r *http.Request) {
	v := mux.Vars(r)
	name, ref := v["name"], v["ref"]
	info, ok := h.lookupRepo(w, r, name)
	if !ok {
		return
	}
	ref = refOrDefault(info, ref)

	page := 1
	if s := r.URL.Query().Get("page"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			page = n
		}
	}

	commits, err := h.browser.ListCommits(r.Context(), name, ref, page, 50)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	data := h.envelope(name+"@"+ref, h.repoCount(r.Context()))
	data.Repo = info
	data.Ref = ref
	data.Commits = commits
	h.render(w, r, "log", data)
}

func (h *Handler) handleNotFound(w http.ResponseWriter, r *http.Request) {
	h.notFound(w, r, "page not found", r.URL.Path)
}

// ---------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------

func (h *Handler) lookupRepo(w http.ResponseWriter, r *http.Request, name string) (repobrowser.RepoInfo, bool) {
	info, err := h.browser.GetRepo(r.Context(), name)
	if err != nil {
		if errors.Is(err, repobrowser.ErrRepoNotFound) {
			h.notFound(w, r, "repository not found", name)
			return repobrowser.RepoInfo{}, false
		}
		h.serverError(w, r, err)
		return repobrowser.RepoInfo{}, false
	}
	return info, true
}

func (h *Handler) repoCount(ctx context.Context) int {
	all, err := h.browser.ListRepos(ctx)
	if err != nil {
		return 0
	}
	return len(all)
}

func (h *Handler) envelope(title string, repoCount int) pageData {
	return pageData{
		Title:        title,
		BasePath:     h.basePath,
		RepoCount:    repoCount,
		Now:          time.Now().UTC().Format("2006-01-02 15:04 UTC"),
		HasBackups:   h.backups != nil,
		HasWorkItems: h.workItems != nil,
	}
}

func (h *Handler) render(w http.ResponseWriter, _ *http.Request, name string, data pageData) {
	t, ok := h.tmpls[name]
	if !ok {
		http.Error(w, "template not found: "+name, http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.tmpl.ExecuteTemplate(&buf, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(buf.Bytes())
}

func (h *Handler) serverError(w http.ResponseWriter, r *http.Request, err error) {
	data := h.envelope("error", 0)
	data.Code = "500"
	data.Title = "internal error"
	data.Detail = err.Error()
	w.WriteHeader(http.StatusInternalServerError)
	h.renderRaw(w, r, "error", data)
}

func (h *Handler) notFound(w http.ResponseWriter, r *http.Request, title, detail string) {
	data := h.envelope("404", 0)
	data.Code = "404"
	data.Title = title
	data.Detail = detail
	w.WriteHeader(http.StatusNotFound)
	h.renderRaw(w, r, "error", data)
}

func (h *Handler) renderRaw(w http.ResponseWriter, _ *http.Request, name string, data pageData) {
	t, ok := h.tmpls[name]
	if !ok {
		_, _ = w.Write([]byte(data.Title + ": " + data.Detail))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = t.tmpl.ExecuteTemplate(w, "layout", data)
}

// ---------------------------------------------------------------------
// Path helpers
// ---------------------------------------------------------------------

func cleanPath(p string) string {
	p = strings.Trim(p, "/")
	if p == "" {
		return ""
	}
	c := path.Clean("/" + p)
	return strings.TrimPrefix(c, "/")
}

func parentDir(p string) string {
	if p == "" {
		return ""
	}
	d := path.Dir(p)
	if d == "." || d == "/" {
		return ""
	}
	return d
}

func refOrDefault(info repobrowser.RepoInfo, ref string) string {
	if ref != "" {
		return ref
	}
	if info.DefaultBranch != "" {
		return info.DefaultBranch
	}
	return "HEAD"
}
