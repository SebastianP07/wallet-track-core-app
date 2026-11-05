// Package postgres implements the credit cards repository on PostgreSQL.
package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SebastianP07/wallet-track-core-app/internal/cards/adapters/postgres/sqlcgen"
	"github.com/SebastianP07/wallet-track-core-app/internal/cards/application"
	"github.com/SebastianP07/wallet-track-core-app/internal/cards/domain"
)

var _ application.Repository = (*Repository)(nil)

type Repository struct {
	queries *sqlcgen.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{queries: sqlcgen.New(pool)}
}

func (r *Repository) List(ctx context.Context) ([]domain.Card, error) {
	rows, err := r.queries.ListCards(ctx)
	if err != nil {
		return nil, err
	}
	cards := make([]domain.Card, 0, len(rows))
	for _, row := range rows {
		cards = append(cards, domain.Card{
			ID:           row.ID.String(),
			Name:         row.Name,
			Issuer:       row.Issuer,
			StatementDay: int(row.StatementDay),
			DueDay:       int(row.DueDay),
			CreatedAt:    row.CreatedAt.Time,
			UpdatedAt:    row.UpdatedAt.Time,
		})
	}
	return cards, nil
}
