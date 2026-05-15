package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"charm.land/log/v2"
	"github.com/charmbracelet/soft-serve/pkg/backend"
	"github.com/charmbracelet/soft-serve/pkg/proto"
	"github.com/charmbracelet/soft-serve/pkg/workitem"
	"github.com/gorilla/mux"
)

func WorkItemController(_ context.Context, r *mux.Router) {
	api := r.PathPrefix("/api/v1/repos/{repo}/work-items").Subrouter()
	api.HandleFunc("", workItemList).Methods(http.MethodGet)
	api.HandleFunc("/", workItemList).Methods(http.MethodGet)
	api.HandleFunc("", workItemCreate).Methods(http.MethodPost)
	api.HandleFunc("/", workItemCreate).Methods(http.MethodPost)
	api.HandleFunc("/{id:[0-9]+}", workItemMove).Methods(http.MethodPatch)
	api.HandleFunc("/{id:[0-9]+}/messages", workItemMessages).Methods(http.MethodGet)
	api.HandleFunc("/{id:[0-9]+}/messages", workItemAddMessage).Methods(http.MethodPost)
}

type workItemDTO struct {
	ID          int64  `json:"id"`
	RepoName    string `json:"repo_name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Lane        string `json:"lane"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type workItemMessageDTO struct {
	ID         int64  `json:"id"`
	RepoName   string `json:"repo_name"`
	WorkItemID int64  `json:"work_item_id"`
	Kind       string `json:"kind"`
	Title      string `json:"title"`
	Body       string `json:"body"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

func workItemList(w http.ResponseWriter, r *http.Request) {
	svc, repoName, ctx, ok := workItemServiceAndRepo(w, r)
	if !ok {
		return
	}
	items, err := svc.ListByRepo(ctx, repoName)
	if err != nil {
		workItemInternal(w, r, err)
		return
	}
	dtos := make([]workItemDTO, 0, len(items))
	for _, item := range items {
		dtos = append(dtos, toWorkItemDTO(item))
	}
	workItemWriteJSON(w, http.StatusOK, dtos)
}

func workItemCreate(w http.ResponseWriter, r *http.Request) {
	svc, repoName, ctx, ok := workItemServiceAndRepo(w, r)
	if !ok {
		return
	}
	var body struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	item, err := svc.Create(ctx, repoName, body.Title, body.Description)
	if err != nil {
		workItemTranslateError(w, r, err)
		return
	}
	workItemWriteJSON(w, http.StatusCreated, toWorkItemDTO(item))
}

func workItemMove(w http.ResponseWriter, r *http.Request) {
	svc, repoName, ctx, ok := workItemServiceAndRepo(w, r)
	if !ok {
		return
	}
	id, ok := workItemIDFromPath(w, r)
	if !ok {
		return
	}
	var body struct {
		Lane string `json:"lane"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	item, err := svc.Move(ctx, repoName, id, workitem.Lane(body.Lane))
	if err != nil {
		workItemTranslateError(w, r, err)
		return
	}
	workItemWriteJSON(w, http.StatusOK, toWorkItemDTO(item))
}

func workItemMessages(w http.ResponseWriter, r *http.Request) {
	svc, repoName, ctx, ok := workItemServiceAndRepo(w, r)
	if !ok {
		return
	}
	id, ok := workItemIDFromPath(w, r)
	if !ok {
		return
	}
	thread, err := svc.Thread(ctx, repoName, id)
	if err != nil {
		workItemTranslateError(w, r, err)
		return
	}
	dtos := make([]workItemMessageDTO, 0, len(thread.Messages))
	for _, message := range thread.Messages {
		dtos = append(dtos, toWorkItemMessageDTO(message))
	}
	workItemWriteJSON(w, http.StatusOK, dtos)
}

func workItemAddMessage(w http.ResponseWriter, r *http.Request) {
	svc, repoName, ctx, ok := workItemServiceAndRepo(w, r)
	if !ok {
		return
	}
	id, ok := workItemIDFromPath(w, r)
	if !ok {
		return
	}
	var body struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	message, err := svc.AddMessage(ctx, repoName, id, body.Body)
	if err != nil {
		workItemTranslateError(w, r, err)
		return
	}
	workItemWriteJSON(w, http.StatusCreated, toWorkItemMessageDTO(message))
}

func workItemServiceAndRepo(w http.ResponseWriter, r *http.Request) (*workitem.Service, string, context.Context, bool) {
	be := backend.FromContext(r.Context())
	if be == nil || be.WorkItemService() == nil {
		http.Error(w, "work item subsystem not configured", http.StatusServiceUnavailable)
		return nil, "", nil, false
	}

	repoName := mux.Vars(r)["repo"]
	repo, err := be.Repository(r.Context(), repoName)
	if err != nil {
		if errors.Is(err, proto.ErrRepoNotFound) {
			http.Error(w, "repository not found", http.StatusNotFound)
			return nil, "", nil, false
		}
		workItemInternal(w, r, err)
		return nil, "", nil, false
	}

	ctx := proto.WithRepositoryContext(r.Context(), repo)
	return be.WorkItemService(), repoName, ctx, true
}

func workItemIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		http.Error(w, "invalid work item id", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

func workItemTranslateError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, workitem.ErrInvalidTitle):
		http.Error(w, "invalid title", http.StatusBadRequest)
	case errors.Is(err, workitem.ErrInvalidMessage):
		http.Error(w, "invalid message", http.StatusBadRequest)
	case errors.Is(err, workitem.ErrInvalidLane):
		http.Error(w, "invalid lane", http.StatusBadRequest)
	case errors.Is(err, workitem.ErrWorkItemNotFound):
		http.Error(w, "work item not found", http.StatusNotFound)
	default:
		workItemInternal(w, r, err)
	}
}

func workItemInternal(w http.ResponseWriter, r *http.Request, err error) {
	log.FromContext(r.Context()).Error("work item http handler error",
		"path", r.URL.Path, "method", r.Method, "err", err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}

func workItemWriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		_, _ = io.WriteString(w, "")
	}
}

func toWorkItemDTO(item workitem.WorkItem) workItemDTO {
	return workItemDTO{
		ID:          item.ID,
		RepoName:    item.RepoName,
		Title:       item.Title,
		Description: item.Description,
		Lane:        string(item.Lane),
		CreatedAt:   formatWorkItemTime(item.CreatedAt),
		UpdatedAt:   formatWorkItemTime(item.UpdatedAt),
	}
}

func toWorkItemMessageDTO(message workitem.WorkItemMessage) workItemMessageDTO {
	return workItemMessageDTO{
		ID:         message.ID,
		RepoName:   message.RepoName,
		WorkItemID: message.WorkItemID,
		Kind:       string(message.Kind),
		Title:      message.Title,
		Body:       message.Body,
		CreatedAt:  formatWorkItemTime(message.CreatedAt),
		UpdatedAt:  formatWorkItemTime(message.UpdatedAt),
	}
}

func formatWorkItemTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02T15:04:05Z")
}
