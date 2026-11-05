// Package postgres implements the credit cards repository on PostgreSQL.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SebastianP07/wallet-track-core-app/internal/cards/adapters/postgres/sqlcgen"
	"github.com/SebastianP07/wallet-track-core-app/internal/cards/application"
	"github.com/SebastianP07/wallet-track-core-app/internal/cards/domain"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
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
		cards = append(cards, toDomain(row))
	}
	return cards, nil
}

func (r *Repository) Get(ctx context.Context, id string) (domain.Card, error) {
	uuid, err := parseID(id)
	if err != nil {
		return domain.Card{}, err
	}
	row, err := r.queries.GetCard(ctx, uuid)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Card{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Card{}, err
	}
	return toDomain(row), nil
}

func (r *Repository) Create(ctx context.Context, card domain.NewCard) (domain.Card, error) {
	row, err := r.queries.CreateCard(ctx, sqlcgen.CreateCardParams{
		Name:         card.Name,
		Issuer:       card.Issuer,
		StatementDay: dayColumn(card.StatementDay),
		DueDay:       dayColumn(card.DueDay),
	})
	if err != nil {
		return domain.Card{}, err
	}
	return toDomain(row), nil
}

func (r *Repository) Update(ctx context.Context, id string, changes domain.Changes) (domain.Card, error) {
	uuid, err := parseID(id)
	if err != nil {
		return domain.Card{}, err
	}
	row, err := r.queries.UpdateCard(ctx, sqlcgen.UpdateCardParams{
		ID:           uuid,
		Name:         toText(changes.Name),
		Issuer:       toText(changes.Issuer),
		StatementDay: toDay(changes.StatementDay),
		DueDay:       toDay(changes.DueDay),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Card{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Card{}, err
	}
	return toDomain(row), nil
}

func (r *Repository) Delete(ctx context.Context, id string) (bool, error) {
	uuid, err := parseID(id)
	if err != nil {
		return false, err
	}
	deleted, err := r.queries.DeleteCard(ctx, uuid)
	if err != nil {
		return false, err
	}
	return deleted > 0, nil
}

func (r *Repository) SumCharges(ctx context.Context, month kernel.YearMonth) ([]domain.CardTotal, error) {
	period, err := r.queries.GetPeriodID(ctx, sqlcgen.GetPeriodIDParams{
		Year:  int32(month.Year),  //nolint:gosec // kernel.YearMonth keeps the year to four digits.
		Month: int32(month.Month), //nolint:gosec // kernel.YearMonth keeps the month to 1-12.
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrPeriodNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := r.queries.SumCardCharges(ctx, period)
	if err != nil {
		return nil, err
	}
	totals := make([]domain.CardTotal, 0, len(rows))
	for _, row := range rows {
		total := domain.CardTotal{Total: kernel.Money(row.Total)}
		if row.CardID.Valid {
			id := row.CardID.String()
			total.CardID = &id
		}
		if row.CardName.Valid {
			total.CardName = &row.CardName.String
		}
		totals = append(totals, total)
	}
	return totals, nil
}

func parseID(id string) (pgtype.UUID, error) {
	var uuid pgtype.UUID
	if err := uuid.Scan(id); err != nil {
		return pgtype.UUID{}, fmt.Errorf("parse card id: %w", err)
	}
	return uuid, nil
}

func dayColumn(day int) int16 {
	return int16(day) //nolint:gosec // The domain keeps a day to 1-31.
}

func toDay(day *int) pgtype.Int2 {
	if day == nil {
		return pgtype.Int2{}
	}
	return pgtype.Int2{Int16: dayColumn(*day), Valid: true}
}

func toText(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *value, Valid: true}
}

func toDomain(row sqlcgen.CreditCard) domain.Card {
	return domain.Card{
		ID:           row.ID.String(),
		Name:         row.Name,
		Issuer:       row.Issuer,
		StatementDay: int(row.StatementDay),
		DueDay:       int(row.DueDay),
		CreatedAt:    row.CreatedAt.Time,
		UpdatedAt:    row.UpdatedAt.Time,
	}
}
