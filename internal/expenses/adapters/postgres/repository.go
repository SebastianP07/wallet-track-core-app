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

// PaymentsInTx builds the platforms port on the transaction, so an item and its payment commit together.
type PaymentsInTx func(tx pgx.Tx) application.MemberPayments

// TemplatesInTx builds the periods port on the transaction, so an item and its template commit together.
type TemplatesInTx func(tx pgx.Tx) application.Templates

type TxManager struct {
	pool      *pgxpool.Pool
	payments  PaymentsInTx
	templates TemplatesInTx
}

func NewTxManager(pool *pgxpool.Pool, payments PaymentsInTx, templates TemplatesInTx) *TxManager {
	return &TxManager{pool: pool, payments: payments, templates: templates}
}

func (m *TxManager) WithinTx(ctx context.Context, fn func(ctx context.Context, repo application.Repository) error) error {
	return pgx.BeginFunc(ctx, m.pool, func(tx pgx.Tx) error {
		return fn(ctx, &Repository{queries: sqlcgen.New(tx), payments: m.payments(tx), templates: m.templates(tx)})
	})
}

type Repository struct {
	queries   *sqlcgen.Queries
	payments  application.MemberPayments
	templates application.Templates
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{queries: sqlcgen.New(pool)}
}

// NewRepositoryInTx is this module's repository on another module's transaction, for the ports it serves.
func NewRepositoryInTx(tx pgx.Tx) *Repository {
	return &Repository{queries: sqlcgen.New(tx)}
}

func (r *Repository) MemberPayments() application.MemberPayments {
	return r.payments
}

func (r *Repository) Templates() application.Templates {
	return r.templates
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

func (r *Repository) ListOwedItems(ctx context.Context, month kernel.YearMonth) ([]domain.Item, error) {
	rows, err := r.queries.ListOwedItems(ctx, sqlcgen.ListOwedItemsParams{
		OwedYear:  pgtype.Int4{Int32: yearColumn(month), Valid: true},
		OwedMonth: pgtype.Int4{Int32: monthColumn(month), Valid: true},
	})
	if err != nil {
		return nil, err
	}
	items := make([]domain.Item, 0, len(rows))
	for _, row := range rows {
		items = append(items, toItem(sqlcgen.GetItemRow(row)))
	}
	return items, nil
}

func (r *Repository) ListOverdueIncome(ctx context.Context) ([]domain.Item, error) {
	rows, err := r.queries.ListOverdueIncome(ctx)
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

func (r *Repository) SetItemTemplate(ctx context.Context, itemID, templateID string) (domain.Item, error) {
	item, err := parseID(itemID)
	if err != nil {
		return domain.Item{}, err
	}
	template, err := parseID(templateID)
	if err != nil {
		return domain.Item{}, err
	}
	updated, err := r.queries.SetItemTemplate(ctx, sqlcgen.SetItemTemplateParams{ID: item, TemplateID: template})
	if err != nil {
		return domain.Item{}, err
	}
	if updated == 0 {
		return domain.Item{}, domain.ErrItemNotFound
	}
	return r.getItem(ctx, item)
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
	params := sqlcgen.SetItemStatusParams{ID: uuid, Status: string(item.Status), DoneAt: doneAt}
	if item.OwedIn != nil {
		params.OwedYear = pgtype.Int4{Int32: yearColumn(*item.OwedIn), Valid: true}
		params.OwedMonth = pgtype.Int4{Int32: monthColumn(*item.OwedIn), Valid: true}
	}
	updated, err := r.queries.SetItemStatus(ctx, params)
	if err != nil {
		return domain.Item{}, err
	}
	if updated == 0 {
		return domain.Item{}, domain.ErrItemNotFound
	}
	return r.getItem(ctx, uuid)
}

func (r *Repository) GetMemberItem(ctx context.Context, personID string, month kernel.YearMonth) (domain.Item, error) {
	person, err := parseID(personID)
	if err != nil {
		return domain.Item{}, err
	}
	row, err := r.queries.GetMemberItem(ctx, sqlcgen.GetMemberItemParams{
		Year:     yearColumn(month),
		Month:    monthColumn(month),
		PersonID: person,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Item{}, domain.ErrItemNotFound
	}
	if err != nil {
		return domain.Item{}, err
	}
	return toItem(sqlcgen.GetItemRow(row)), nil
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

func (r *Repository) ListKindOrder(ctx context.Context, periodID string, kind domain.ItemKind) ([]domain.OrderedItem, error) {
	period, err := parseID(periodID)
	if err != nil {
		return nil, err
	}
	rows, err := r.queries.ListKindOrder(ctx, sqlcgen.ListKindOrderParams{PeriodID: period, Kind: string(kind)})
	if err != nil {
		return nil, err
	}
	items := make([]domain.OrderedItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, domain.OrderedItem{ID: row.ID.String(), TemplateID: fromOptionalID(row.TemplateID)})
	}
	return items, nil
}

func (r *Repository) SetItemOrder(ctx context.Context, ids []string) error {
	uuids := make([]pgtype.UUID, 0, len(ids))
	for _, id := range ids {
		uuid, err := parseID(id)
		if err != nil {
			return err
		}
		uuids = append(uuids, uuid)
	}
	return r.queries.SetItemPositions(ctx, uuids)
}

func (r *Repository) LockOpenPeriods(ctx context.Context) error {
	_, err := r.queries.LockOpenPeriods(ctx)
	return err
}

func (r *Repository) UpdatePlatformCharges(ctx context.Context, charge domain.PlatformCharge) error {
	platform, card, err := chargeIDs(charge)
	if err != nil {
		return err
	}
	return r.queries.UpdatePlatformCharges(ctx, sqlcgen.UpdatePlatformChargesParams{
		PlatformID: platform,
		Name:       charge.Name,
		Amount:     int64(charge.Amount),
		CardID:     card,
	})
}

func (r *Repository) AddPlatformCharges(ctx context.Context, charge domain.PlatformCharge) error {
	platform, card, err := chargeIDs(charge)
	if err != nil {
		return err
	}
	err = r.queries.AddPlatformCharges(ctx, sqlcgen.AddPlatformChargesParams{
		PlatformID: platform,
		Name:       charge.Name,
		Amount:     int64(charge.Amount),
		CardID:     card,
	})
	return explainItemWrite(err)
}

func (r *Repository) RemovePlatformCharges(ctx context.Context, platformID string) error {
	platform, err := parseID(platformID)
	if err != nil {
		return err
	}
	return r.queries.RemovePlatformCharges(ctx, platform)
}

func (r *Repository) OpenCardChargesOutOfRange(ctx context.Context) (bool, error) {
	return r.queries.OpenCardChargesOutOfRange(ctx, int64(kernel.MaxMoney))
}

func chargeIDs(charge domain.PlatformCharge) (pgtype.UUID, pgtype.UUID, error) {
	platform, err := parseID(charge.PlatformID)
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, err
	}
	card, err := parseID(charge.CardID)
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, err
	}
	return platform, card, nil
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
		PlatformID: fromOptionalID(row.PlatformID),
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
	if row.OwedYear.Valid && row.OwedMonth.Valid {
		item.OwedIn = &kernel.YearMonth{Year: int(row.OwedYear.Int32), Month: time.Month(row.OwedMonth.Int32)}
	}
	return item
}
