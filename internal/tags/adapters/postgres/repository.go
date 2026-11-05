// Package postgres implements the tags repository on PostgreSQL.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SebastianP07/wallet-track-core-app/internal/tags/adapters/postgres/sqlcgen"
	"github.com/SebastianP07/wallet-track-core-app/internal/tags/application"
	"github.com/SebastianP07/wallet-track-core-app/internal/tags/domain"
)

const uniqueViolation = "23505"

var _ application.Repository = (*Repository)(nil)

type Repository struct {
	queries *sqlcgen.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{queries: sqlcgen.New(pool)}
}

func (r *Repository) List(ctx context.Context) ([]domain.Tag, error) {
	rows, err := r.queries.ListTags(ctx)
	if err != nil {
		return nil, err
	}
	tags := make([]domain.Tag, 0, len(rows))
	for _, row := range rows {
		tags = append(tags, toDomain(row))
	}
	return tags, nil
}

func (r *Repository) Create(ctx context.Context, tag domain.NewTag) (domain.Tag, error) {
	row, err := r.queries.CreateTag(ctx, sqlcgen.CreateTagParams{Name: tag.Name, Color: tag.Color})
	if err != nil {
		return domain.Tag{}, explainWrite(err)
	}
	return toDomain(row), nil
}

func (r *Repository) Update(ctx context.Context, id string, changes domain.Changes) (domain.Tag, error) {
	uuid, err := parseID(id)
	if err != nil {
		return domain.Tag{}, err
	}
	row, err := r.queries.UpdateTag(ctx, sqlcgen.UpdateTagParams{
		ID:    uuid,
		Name:  toText(changes.Name),
		Color: toText(changes.Color),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Tag{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Tag{}, explainWrite(err)
	}
	return toDomain(row), nil
}

func (r *Repository) Delete(ctx context.Context, id string) (bool, error) {
	uuid, err := parseID(id)
	if err != nil {
		return false, err
	}
	deleted, err := r.queries.DeleteTag(ctx, uuid)
	if err != nil {
		return false, err
	}
	return deleted > 0, nil
}

func explainWrite(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return domain.ErrNameTaken
	}
	return err
}

func parseID(id string) (pgtype.UUID, error) {
	var uuid pgtype.UUID
	if err := uuid.Scan(id); err != nil {
		return pgtype.UUID{}, fmt.Errorf("parse tag id: %w", err)
	}
	return uuid, nil
}

func toText(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *value, Valid: true}
}

func toDomain(row sqlcgen.Tag) domain.Tag {
	return domain.Tag{
		ID:        row.ID.String(),
		Name:      row.Name,
		Color:     row.Color,
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}
}
