package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/charmbracelet/soft-serve/pkg/db"
	"github.com/charmbracelet/soft-serve/pkg/workitem"
)

type Store struct {
	db *db.DB
}

var _ workitem.Store = (*Store)(nil)

func New(database *db.DB) *Store {
	return &Store{db: database}
}

func (s *Store) Create(ctx context.Context, item workitem.WorkItem) (*workitem.WorkItem, error) {
	var id int64
	err := s.db.TransactionContext(ctx, func(tx *db.Tx) error {
		repoID, err := repoIDByName(ctx, tx, item.RepoName)
		if err != nil {
			return err
		}
		query := tx.Rebind(`INSERT INTO work_items
			(repo_id, title, description, lane, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)
			RETURNING id;`)
		if err := tx.GetContext(ctx, &id, query,
			repoID, item.Title, item.Description, string(item.Lane), item.CreatedAt, item.UpdatedAt,
		); err != nil {
			insert := tx.Rebind(`INSERT INTO work_items
				(repo_id, title, description, lane, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?);`)
			result, ierr := tx.ExecContext(ctx, insert,
				repoID, item.Title, item.Description, string(item.Lane), item.CreatedAt, item.UpdatedAt,
			)
			if ierr != nil {
				return db.WrapError(ierr)
			}
			id, _ = result.LastInsertId()
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, item.RepoName, id)
}

func (s *Store) Get(ctx context.Context, repoName string, id int64) (*workitem.WorkItem, error) {
	var row itemRow
	query := s.db.Rebind(itemSelectSQL + ` WHERE r.name = ? AND wi.id = ?;`)
	if err := s.db.GetContext(ctx, &row, query, repoName, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, workitem.ErrWorkItemNotFound
		}
		return nil, db.WrapError(err)
	}
	item := row.toWorkItem()
	return &item, nil
}

func (s *Store) ListByRepo(ctx context.Context, repoName string) ([]workitem.WorkItem, error) {
	var rows []itemRow
	query := s.db.Rebind(itemSelectSQL + ` WHERE r.name = ?
		ORDER BY CASE wi.lane
			WHEN 'backlog' THEN 0
			WHEN 'wip' THEN 1
			WHEN 'done' THEN 2
			ELSE 3
		END, wi.id ASC;`)
	if err := s.db.SelectContext(ctx, &rows, query, repoName); err != nil {
		return nil, db.WrapError(err)
	}
	items := make([]workitem.WorkItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, row.toWorkItem())
	}
	return items, nil
}

func (s *Store) UpdateLane(ctx context.Context, repoName string, id int64, lane workitem.Lane, updatedAt time.Time) (*workitem.WorkItem, error) {
	query := s.db.Rebind(`UPDATE work_items
		SET lane = ?, updated_at = ?
		WHERE id = ? AND repo_id = (SELECT id FROM repos WHERE name = ?);`)
	result, err := s.db.ExecContext(ctx, query, string(lane), updatedAt, id, repoName)
	if err != nil {
		return nil, db.WrapError(err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return nil, workitem.ErrWorkItemNotFound
	}
	return s.Get(ctx, repoName, id)
}

func (s *Store) AddMessage(ctx context.Context, message workitem.WorkItemMessage) (*workitem.WorkItemMessage, error) {
	var id int64
	err := s.db.TransactionContext(ctx, func(tx *db.Tx) error {
		workItemID, err := workItemIDByRepo(ctx, tx, message.RepoName, message.WorkItemID)
		if err != nil {
			return err
		}
		query := tx.Rebind(`INSERT INTO work_item_messages
			(work_item_id, kind, body, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?)
			RETURNING id;`)
		if err := tx.GetContext(ctx, &id, query,
			workItemID, string(message.Kind), message.Body, message.CreatedAt, message.UpdatedAt,
		); err != nil {
			insert := tx.Rebind(`INSERT INTO work_item_messages
				(work_item_id, kind, body, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?);`)
			result, ierr := tx.ExecContext(ctx, insert,
				workItemID, string(message.Kind), message.Body, message.CreatedAt, message.UpdatedAt,
			)
			if ierr != nil {
				return db.WrapError(ierr)
			}
			id, _ = result.LastInsertId()
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	messages, err := s.ListMessages(ctx, message.RepoName, message.WorkItemID)
	if err != nil {
		return nil, err
	}
	for _, m := range messages {
		if m.ID == id {
			out := m
			return &out, nil
		}
	}
	return nil, workitem.ErrWorkItemNotFound
}

func (s *Store) ListMessages(ctx context.Context, repoName string, workItemID int64) ([]workitem.WorkItemMessage, error) {
	if _, err := workItemIDByRepo(ctx, s.db, repoName, workItemID); err != nil {
		return nil, err
	}

	var rows []messageRow
	query := s.db.Rebind(messageSelectSQL + ` WHERE r.name = ? AND wi.id = ?
		ORDER BY wim.id ASC;`)
	if err := s.db.SelectContext(ctx, &rows, query, repoName, workItemID); err != nil {
		return nil, db.WrapError(err)
	}
	messages := make([]workitem.WorkItemMessage, 0, len(rows))
	for _, row := range rows {
		messages = append(messages, row.toWorkItemMessage())
	}
	return messages, nil
}

const itemSelectSQL = `SELECT
	wi.id,
	r.name AS repo_name,
	wi.title,
	wi.description,
	wi.lane,
	wi.created_at,
	wi.updated_at
	FROM work_items wi
	JOIN repos r ON r.id = wi.repo_id`

type itemRow struct {
	ID          int64     `db:"id"`
	RepoName    string    `db:"repo_name"`
	Title       string    `db:"title"`
	Description string    `db:"description"`
	Lane        string    `db:"lane"`
	CreatedAt   time.Time `db:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"`
}

func (r itemRow) toWorkItem() workitem.WorkItem {
	return workitem.WorkItem{
		ID:          r.ID,
		RepoName:    r.RepoName,
		Title:       r.Title,
		Description: r.Description,
		Lane:        workitem.Lane(r.Lane),
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

const messageSelectSQL = `SELECT
		wim.id,
		r.name AS repo_name,
		wi.id AS work_item_id,
		wim.kind,
		wim.body,
		wim.created_at,
		wim.updated_at
		FROM work_item_messages wim
		JOIN work_items wi ON wi.id = wim.work_item_id
		JOIN repos r ON r.id = wi.repo_id`

type messageRow struct {
	ID         int64     `db:"id"`
	RepoName   string    `db:"repo_name"`
	WorkItemID int64     `db:"work_item_id"`
	Kind       string    `db:"kind"`
	Body       string    `db:"body"`
	CreatedAt  time.Time `db:"created_at"`
	UpdatedAt  time.Time `db:"updated_at"`
}

func (r messageRow) toWorkItemMessage() workitem.WorkItemMessage {
	return workitem.WorkItemMessage{
		ID:         r.ID,
		RepoName:   r.RepoName,
		WorkItemID: r.WorkItemID,
		Kind:       workitem.MessageKind(r.Kind),
		Body:       r.Body,
		CreatedAt:  r.CreatedAt,
		UpdatedAt:  r.UpdatedAt,
	}
}

func repoIDByName(ctx context.Context, h db.Handler, name string) (int64, error) {
	var id int64
	query := h.Rebind(`SELECT id FROM repos WHERE name = ?;`)
	if err := h.GetContext(ctx, &id, query, name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("repo %q: %w", name, workitem.ErrWorkItemNotFound)
		}
		return 0, db.WrapError(err)
	}
	return id, nil
}

func workItemIDByRepo(ctx context.Context, h db.Handler, repoName string, id int64) (int64, error) {
	var found int64
	query := h.Rebind(`SELECT wi.id
		FROM work_items wi
		JOIN repos r ON r.id = wi.repo_id
		WHERE r.name = ? AND wi.id = ?;`)
	if err := h.GetContext(ctx, &found, query, repoName, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, workitem.ErrWorkItemNotFound
		}
		return 0, db.WrapError(err)
	}
	return found, nil
}
