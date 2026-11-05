// Package postgres implements the expense items repository and transactions on PostgreSQL.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SebastianP07/wallet-track-core-app/internal/expenses/adapters/postgres/sqlcgen"
	"github.com/SebastianP07/wallet-track-core-app/internal/expenses/application"
	"github.com/SebastianP07/wallet-track-core-app/internal/expenses/domain"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
)

var (
	_ application.Repository = (*Repository)(nil)
	_ application.TxManager  = (*TxManager)(nil)
)

// What a refused item write means, for the rules only the database can check at write time.
var itemConstraints = map[string]domain.Violation{
	"period_items_tag_id_fkey":  {Field: "tagId", Message: "must be an existing tag"},
	"period_items_card_id_fkey": {Field: "cardId", Message: "must be an existing card"},
}

type TxManager struct {
	pool *pgxpool.Pool
}

func NewTxManager(pool *pgxpool.Pool) *TxManager {
	return &TxManager{pool: pool}
}

func (m *TxManager) WithinTx(ctx context.Context, fn func(ctx context.Context, repo application.Repository) error) error {
	return pgx.BeginFunc(ctx, m.pool, func(tx pgx.Tx) error {
		return fn(ctx, &Repository{queries: sqlcgen.New(tx)})
	})
}

type Repository struct {
	queries *sqlcgen.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{queries: sqlcgen.New(pool)}
}

func (r *Repository) GetPeriod(ctx context.Context, month kernel.YearMonth) (domain.Period, error) {
	row, err := r.queries.GetPeriod(ctx, sqlcgen.GetPeriodParams{Year: yearColumn(month), Month: monthColumn(month)})
	return toPeriod(row.ID, row.Year, row.Month, row.Status, err)
}

func (r *Repository) LockPeriod(ctx context.Context, month kernel.YearMonth) (domain.Period, error) {
	row, err := r.queries.LockPeriod(ctx, sqlcgen.LockPeriodParams{Year: yearColumn(month), Month: monthColumn(month)})
	return toPeriod(row.ID, row.Year, row.Month, row.Status, err)
}

func (r *Repository) LockPeriodOfItem(ctx context.Context, itemID string) (domain.Period, error) {
	uuid, err := parseID(itemID)
	if err != nil {
		return domain.Period{}, err
	}
	row, err := r.queries.LockPeriodOfItem(ctx, uuid)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Period{}, domain.ErrItemNotFound
	}
	return toPeriod(row.ID, row.Year, row.Month, row.Status, err)
}

func (r *Repository) ListItems(ctx context.Context, periodID string) ([]domain.Item, error) {
	uuid, err := parseID(periodID)
	if err != nil {
		return nil, err
	}
	rows, err := r.queries.ListItems(ctx, uuid)
	if err != nil {
		return nil, err
	}
	items := make([]domain.Item, 0, len(rows))
	for _, row := range rows {
		items = append(items, toItem(sqlcgen.GetItemRow(row)))
	}
	return items, nil
}

func (r *Repository) GetItem(ctx context.Context, id string) (domain.Item, error) {
	uuid, err := parseID(id)
	if err != nil {
		return domain.Item{}, err
	}
	return r.getItem(ctx, uuid)
}

func (r *Repository) getItem(ctx context.Context, id pgtype.UUID) (domain.Item, error) {
	row, err := r.queries.GetItem(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Item{}, domain.ErrItemNotFound
	}
	if err != nil {
		return domain.Item{}, err
	}
	return toItem(row), nil
}

func (r *Repository) CreateItem(ctx context.Context, periodID string, item domain.NewItem) (domain.Item, error) {
	period, err := parseID(periodID)
	if err != nil {
		return domain.Item{}, err
	}
	cardID, err := parseOptionalID(item.CardID)
	if err != nil {
		return domain.Item{}, err
	}
	tagID, err := parseOptionalID(item.TagID)
	if err != nil {
		return domain.Item{}, err
	}
	id, err := r.queries.CreateItem(ctx, sqlcgen.CreateItemParams{
		PeriodID: period,
		Kind:     string(item.Kind),
		Name:     item.Name,
		Amount:   int64(item.Amount),
		DueDate:  toDate(item.DueDate),
		CardID:   cardID,
		TagID:    tagID,
	})
	if err != nil {
		return domain.Item{}, explainItemWrite(err)
	}
	return r.getItem(ctx, id)
}

func (r *Repository) UpdateItem(ctx context.Context, item domain.Item) (domain.Item, error) {
	uuid, err := parseID(item.ID)
	if err != nil {
		return domain.Item{}, err
	}
	cardID, err := parseOptionalID(item.CardID)
	if err != nil {
		return domain.Item{}, err
	}
	tagID, err := parseOptionalID(item.TagID)
	if err != nil {
		return domain.Item{}, err
	}
	updated, err := r.queries.UpdateItem(ctx, sqlcgen.UpdateItemParams{
		ID:      uuid,
		Name:    item.Name,
		Amount:  int64(item.Amount),
		DueDate: toDate(item.DueDate),
		CardID:  cardID,
		TagID:   tagID,
	})
	if err != nil {
		return domain.Item{}, explainItemWrite(err)
	}
	if updated == 0 {
		return domain.Item{}, domain.ErrItemNotFound
	}
	return r.getItem(ctx, uuid)
}

func (r *Repository) SetItemStatus(ctx context.Context, item domain.Item) (domain.Item, error) {
	uuid, err := parseID(item.ID)
	if err != nil {
		return domain.Item{}, err
	}
	doneAt := pgtype.Timestamptz{}
	if item.DoneAt != nil {
		doneAt = pgtype.Timestamptz{Time: *item.DoneAt, Valid: true}
	}
	updated, err := r.queries.SetItemStatus(ctx, sqlcgen.SetItemStatusParams{ID: uuid, Status: string(item.Status), DoneAt: doneAt})
	if err != nil {
		return domain.Item{}, err
	}
	if updated == 0 {
		return domain.Item{}, domain.ErrItemNotFound
	}
	return r.getItem(ctx, uuid)
}

func (r *Repository) DeleteItem(ctx context.Context, id string) (bool, error) {
	uuid, err := parseID(id)
	if err != nil {
		return false, err
	}
	deleted, err := r.queries.DeleteItem(ctx, uuid)
	if err != nil {
		return false, err
	}
	return deleted > 0, nil
}

func explainItemWrite(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if violation, known := itemConstraints[pgErr.ConstraintName]; known {
			return &domain.ValidationError{Violations: []domain.Violation{violation}}
		}
	}
	return err
}

func parseID(id string) (pgtype.UUID, error) {
	var uuid pgtype.UUID
	if err := uuid.Scan(id); err != nil {
		return pgtype.UUID{}, fmt.Errorf("parse id: %w", err)
	}
	return uuid, nil
}

func parseOptionalID(id *string) (pgtype.UUID, error) {
	if id == nil {
		return pgtype.UUID{}, nil
	}
	return parseID(*id)
}

func fromOptionalID(id pgtype.UUID) *string {
	if !id.Valid {
		return nil
	}
	text := id.String()
	return &text
}

func toDate(date *time.Time) pgtype.Date {
	if date == nil {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: *date, Valid: true}
}

func yearColumn(month kernel.YearMonth) int32 {
	return int32(month.Year) //nolint:gosec // kernel.YearMonth keeps the year to four digits.
}

func monthColumn(month kernel.YearMonth) int32 {
	return int32(month.Month) //nolint:gosec // kernel.YearMonth keeps the month to 1-12.
}

func toPeriod(id pgtype.UUID, year, month int32, status string, err error) (domain.Period, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Period{}, domain.ErrPeriodNotFound
	}
	if err != nil {
		return domain.Period{}, err
	}
	return domain.Period{
		ID:     id.String(),
		Month:  kernel.YearMonth{Year: int(year), Month: time.Month(month)},
		Status: domain.PeriodStatus(status),
	}, nil
}

func toItem(row sqlcgen.GetItemRow) domain.Item {
	item := domain.Item{
		ID:         row.ID.String(),
		Month:      kernel.YearMonth{Year: int(row.Year), Month: time.Month(row.Month)},
		TemplateID: fromOptionalID(row.TemplateID),
		PersonID:   fromOptionalID(row.PersonID),
		Kind:       domain.ItemKind(row.Kind),
		Name:       row.Name,
		Amount:     kernel.Money(row.Amount),
		CardID:     fromOptionalID(row.CardID),
		TagID:      fromOptionalID(row.TagID),
		Status:     domain.ItemStatus(row.Status),
		CreatedAt:  row.CreatedAt.Time,
		UpdatedAt:  row.UpdatedAt.Time,
	}
	if row.DueDate.Valid {
		due := row.DueDate.Time
		item.DueDate = &due
	}
	if row.DoneAt.Valid {
		done := row.DoneAt.Time
		item.DoneAt = &done
	}
	return item
}
