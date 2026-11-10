// Package postgres implements the periods repository and transactions on PostgreSQL.
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

	"github.com/SebastianP07/wallet-track-core-app/internal/periods/adapters/postgres/sqlcgen"
	"github.com/SebastianP07/wallet-track-core-app/internal/periods/application"
	"github.com/SebastianP07/wallet-track-core-app/internal/periods/domain"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
)

var (
	_ application.Repository = (*Repository)(nil)
	_ application.TxManager  = (*TxManager)(nil)
)

// What a refused template write means, for the rules only the database can check at write time
// (a card or tag deleted meanwhile) and as the last word on the rest.
var templateConstraints = map[string]domain.Violation{
	"recurring_templates_card_id_fkey":                {Field: "cardId", Message: "must be an existing card"},
	"recurring_templates_tag_id_fkey":                 {Field: "tagId", Message: "must be an existing tag"},
	"recurring_templates_fixed_has_due_day":           {Field: "dueDay", Message: "is required on a fixed template"},
	"recurring_templates_active_card_charge_has_card": {Field: "cardId", Message: "an active card charge template must have a card"},
}

// MembersInTx builds the platforms port on the transaction, so a month and its items commit together.
type MembersInTx func(tx pgx.Tx) application.MemberIncome

// ChargesInTx builds the other platforms port on the transaction, for the same reason (D14).
type ChargesInTx func(tx pgx.Tx) application.PlatformCharges

type TxManager struct {
	pool    *pgxpool.Pool
	members MembersInTx
	charges ChargesInTx
}

func NewTxManager(pool *pgxpool.Pool, members MembersInTx, charges ChargesInTx) *TxManager {
	return &TxManager{pool: pool, members: members, charges: charges}
}

func (m *TxManager) WithinTx(ctx context.Context, fn func(ctx context.Context, repo application.Repository) error) error {
	return pgx.BeginFunc(ctx, m.pool, func(tx pgx.Tx) error {
		return fn(ctx, &Repository{queries: sqlcgen.New(tx), members: m.members(tx), charges: m.charges(tx)})
	})
}

type Repository struct {
	queries *sqlcgen.Queries
	members application.MemberIncome
	charges application.PlatformCharges
}

func (r *Repository) MemberIncome() application.MemberIncome {
	return r.members
}

func (r *Repository) PlatformCharges() application.PlatformCharges {
	return r.charges
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{queries: sqlcgen.New(pool)}
}

// NewRepositoryInTx is this module's repository on another module's transaction, for the ports it serves.
func NewRepositoryInTx(tx pgx.Tx) *Repository {
	return &Repository{queries: sqlcgen.New(tx)}
}

func (r *Repository) ListPeriods(ctx context.Context) ([]domain.Period, error) {
	rows, err := r.queries.ListPeriods(ctx)
	if err != nil {
		return nil, err
	}
	periods := make([]domain.Period, 0, len(rows))
	for _, row := range rows {
		periods = append(periods, toPeriod(row))
	}
	return periods, nil
}

func (r *Repository) GetPeriod(ctx context.Context, month kernel.YearMonth) (domain.Period, error) {
	row, err := r.queries.GetPeriod(ctx, sqlcgen.GetPeriodParams(monthColumns(month)))
	return toPeriodOrNotFound(row, err)
}

func (r *Repository) CreatePeriod(ctx context.Context, month kernel.YearMonth) (domain.Period, bool, error) {
	row, err := r.queries.CreatePeriod(ctx, sqlcgen.CreatePeriodParams(monthColumns(month)))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Period{}, false, nil
	}
	if err != nil {
		return domain.Period{}, false, err
	}
	return toPeriod(row), true, nil
}

func (r *Repository) LockPeriod(ctx context.Context, month kernel.YearMonth) (domain.Period, error) {
	row, err := r.queries.LockPeriod(ctx, sqlcgen.LockPeriodParams(monthColumns(month)))
	return toPeriodOrNotFound(row, err)
}

func (r *Repository) MarkPeriodClosed(ctx context.Context, id string) (domain.Period, error) {
	uuid, err := parseID(id)
	if err != nil {
		return domain.Period{}, err
	}
	row, err := r.queries.MarkPeriodClosed(ctx, uuid)
	return toPeriodOrNotFound(row, err)
}

func (r *Repository) CarryPendingExpenses(ctx context.Context, periodID string, to kernel.YearMonth) error {
	uuid, err := parseID(periodID)
	if err != nil {
		return err
	}
	year, month := monthInts(to)
	return r.queries.CarryPendingExpenses(ctx, sqlcgen.CarryPendingExpensesParams{PeriodID: uuid, OwedYear: year, OwedMonth: month})
}

func (r *Repository) CarryOwedExpenses(ctx context.Context, from, to kernel.YearMonth) error {
	fromYear, fromMonth := monthInts(from)
	toYear, toMonth := monthInts(to)
	return r.queries.CarryOwedExpenses(ctx, sqlcgen.CarryOwedExpensesParams{
		FromYear: fromYear, FromMonth: fromMonth, ToYear: toYear, ToMonth: toMonth,
	})
}

func (r *Repository) MarkPendingIncomeOverdue(ctx context.Context, periodID string) error {
	uuid, err := parseID(periodID)
	if err != nil {
		return err
	}
	return r.queries.MarkPendingIncomeOverdue(ctx, uuid)
}

func monthInts(month kernel.YearMonth) (pgtype.Int4, pgtype.Int4) {
	columns := monthColumns(month)
	return pgtype.Int4{Int32: columns.Year, Valid: true}, pgtype.Int4{Int32: columns.Month, Valid: true}
}

func (r *Repository) AddItem(ctx context.Context, item domain.NewItem) error {
	periodID, err := parseID(item.PeriodID)
	if err != nil {
		return err
	}
	templateID, err := parseID(item.TemplateID)
	if err != nil {
		return err
	}
	cardID, err := parseOptionalID(item.CardID)
	if err != nil {
		return err
	}
	tagID, err := parseOptionalID(item.TagID)
	if err != nil {
		return err
	}
	return r.queries.AddItem(ctx, sqlcgen.AddItemParams{
		PeriodID:   periodID,
		TemplateID: templateID,
		Kind:       string(item.Kind),
		Name:       item.Name,
		Amount:     int64(item.Amount),
		DueDate:    toDate(item.DueDate),
		CardID:     cardID,
		TagID:      tagID,
	})
}

func (r *Repository) AddMemberItem(ctx context.Context, item domain.NewMemberItem) error {
	periodID, err := parseID(item.PeriodID)
	if err != nil {
		return err
	}
	personID, err := parseID(item.PersonID)
	if err != nil {
		return err
	}
	return r.queries.AddMemberItem(ctx, sqlcgen.AddMemberItemParams{
		PeriodID: periodID,
		PersonID: personID,
		Name:     item.Name,
		Amount:   int64(item.Amount),
	})
}

func (r *Repository) AddPlatformItem(ctx context.Context, item domain.NewPlatformItem) error {
	periodID, err := parseID(item.PeriodID)
	if err != nil {
		return err
	}
	platformID, err := parseID(item.PlatformID)
	if err != nil {
		return err
	}
	cardID, err := parseID(item.CardID)
	if err != nil {
		return err
	}
	return r.queries.AddPlatformItem(ctx, sqlcgen.AddPlatformItemParams{
		PeriodID:   periodID,
		PlatformID: platformID,
		Name:       item.Name,
		Amount:     int64(item.Amount),
		CardID:     cardID,
	})
}

func (r *Repository) ListTemplates(ctx context.Context, filter application.TemplateFilter) ([]domain.Template, error) {
	var active pgtype.Bool
	if filter.Active != nil {
		active = pgtype.Bool{Bool: *filter.Active, Valid: true}
	}
	rows, err := r.queries.ListTemplates(ctx, active)
	if err != nil {
		return nil, err
	}
	return toTemplates(rows), nil
}

func (r *Repository) LockActiveTemplates(ctx context.Context) ([]domain.Template, error) {
	rows, err := r.queries.LockActiveTemplates(ctx)
	if err != nil {
		return nil, err
	}
	return toTemplates(rows), nil
}

func (r *Repository) GetTemplate(ctx context.Context, id string) (domain.Template, error) {
	uuid, err := parseID(id)
	if err != nil {
		return domain.Template{}, err
	}
	row, err := r.queries.GetTemplate(ctx, uuid)
	return toTemplateOrNotFound(row, err)
}

func (r *Repository) LockTemplate(ctx context.Context, id string) (domain.Template, error) {
	uuid, err := parseID(id)
	if err != nil {
		return domain.Template{}, err
	}
	row, err := r.queries.LockTemplate(ctx, uuid)
	return toTemplateOrNotFound(row, err)
}

func (r *Repository) CreateTemplate(ctx context.Context, template domain.NewTemplate) (domain.Template, error) {
	cardID, err := parseOptionalID(template.CardID)
	if err != nil {
		return domain.Template{}, err
	}
	tagID, err := parseOptionalID(template.TagID)
	if err != nil {
		return domain.Template{}, err
	}
	row, err := r.queries.CreateTemplate(ctx, sqlcgen.CreateTemplateParams{
		Kind:   string(template.Kind),
		Name:   template.Name,
		Amount: int64(template.Amount),
		DueDay: toDueDay(template.DueDay),
		CardID: cardID,
		TagID:  tagID,
		Active: template.Active,
	})
	if err != nil {
		return domain.Template{}, explainTemplateWrite(err)
	}
	return toTemplate(row), nil
}

func (r *Repository) UpdateTemplate(ctx context.Context, template domain.Template) (domain.Template, error) {
	uuid, err := parseID(template.ID)
	if err != nil {
		return domain.Template{}, err
	}
	cardID, err := parseOptionalID(template.CardID)
	if err != nil {
		return domain.Template{}, err
	}
	tagID, err := parseOptionalID(template.TagID)
	if err != nil {
		return domain.Template{}, err
	}
	row, err := r.queries.UpdateTemplate(ctx, sqlcgen.UpdateTemplateParams{
		ID:     uuid,
		Name:   template.Name,
		Amount: int64(template.Amount),
		DueDay: toDueDay(template.DueDay),
		CardID: cardID,
		TagID:  tagID,
		Active: template.Active,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return domain.Template{}, explainTemplateWrite(err)
	}
	return toTemplateOrNotFound(row, err)
}

func (r *Repository) DeleteTemplate(ctx context.Context, id string) (bool, error) {
	uuid, err := parseID(id)
	if err != nil {
		return false, err
	}
	deleted, err := r.queries.DeleteTemplate(ctx, uuid)
	if err != nil {
		return false, err
	}
	return deleted > 0, nil
}

func (r *Repository) ReorderTemplates(ctx context.Context, ids []string) error {
	uuids := make([]pgtype.UUID, 0, len(ids))
	for _, id := range ids {
		uuid, err := parseID(id)
		if err != nil {
			return err
		}
		uuids = append(uuids, uuid)
	}
	// Locked in id order, as a month's open takes them, so the two cannot deadlock.
	if err := r.queries.LockTemplatesByID(ctx, uuids); err != nil {
		return err
	}
	return r.queries.PlaceTemplates(ctx, uuids)
}

func explainTemplateWrite(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if violation, known := templateConstraints[pgErr.ConstraintName]; known {
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

func monthColumns(month kernel.YearMonth) struct {
	Year  int32
	Month int32
} {
	return struct {
		Year  int32
		Month int32
	}{
		Year:  int32(month.Year),  //nolint:gosec // kernel.YearMonth keeps the year to four digits.
		Month: int32(month.Month), //nolint:gosec // kernel.YearMonth keeps the month to 1-12.
	}
}

func toDueDay(day *int) pgtype.Int2 {
	if day == nil {
		return pgtype.Int2{}
	}
	return pgtype.Int2{Int16: int16(*day), Valid: true} //nolint:gosec // The domain keeps the due day to 1-31.
}

func toPeriodOrNotFound(row sqlcgen.Period, err error) (domain.Period, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Period{}, domain.ErrPeriodNotFound
	}
	if err != nil {
		return domain.Period{}, err
	}
	return toPeriod(row), nil
}

func toPeriod(row sqlcgen.Period) domain.Period {
	return domain.Period{
		ID:        row.ID.String(),
		Month:     kernel.YearMonth{Year: int(row.Year), Month: time.Month(row.Month)},
		Status:    domain.PeriodStatus(row.Status),
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}
}

func toTemplateOrNotFound(row sqlcgen.RecurringTemplate, err error) (domain.Template, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Template{}, domain.ErrTemplateNotFound
	}
	if err != nil {
		return domain.Template{}, err
	}
	return toTemplate(row), nil
}

func toTemplates(rows []sqlcgen.RecurringTemplate) []domain.Template {
	templates := make([]domain.Template, 0, len(rows))
	for _, row := range rows {
		templates = append(templates, toTemplate(row))
	}
	return templates
}

func toTemplate(row sqlcgen.RecurringTemplate) domain.Template {
	template := domain.Template{
		ID:        row.ID.String(),
		Kind:      domain.TemplateKind(row.Kind),
		Name:      row.Name,
		Amount:    kernel.Money(row.Amount),
		CardID:    fromOptionalID(row.CardID),
		TagID:     fromOptionalID(row.TagID),
		Active:    row.Active,
		Position:  int(row.Position),
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}
	if row.DueDay.Valid {
		day := int(row.DueDay.Int16)
		template.DueDay = &day
	}
	return template
}
