// Package postgres implements the platforms repository and transactions on PostgreSQL.
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

	"github.com/SebastianP07/wallet-track-core-app/internal/platforms/adapters/postgres/sqlcgen"
	"github.com/SebastianP07/wallet-track-core-app/internal/platforms/application"
	"github.com/SebastianP07/wallet-track-core-app/internal/platforms/domain"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
)

const (
	ownerConstraint          = "people_single_owner_idx"
	paymentPersonConstraint  = "platform_payments_person_id_fkey"
	platformCardConstraint   = "platforms_card_id_fkey"
	memberPlatformConstraint = "platform_members_platform_id_fkey"
	memberPersonConstraint   = "platform_members_person_id_fkey"
)

var (
	_ application.Repository = (*Repository)(nil)
	_ application.TxManager  = (*TxManager)(nil)
)

// ItemsInTx builds the expenses port on the transaction, so a payment and its item commit together.
type ItemsInTx func(tx pgx.Tx) application.IncomeItems

type TxManager struct {
	pool  *pgxpool.Pool
	items ItemsInTx
}

func NewTxManager(pool *pgxpool.Pool, items ItemsInTx) *TxManager {
	return &TxManager{pool: pool, items: items}
}

func (m *TxManager) WithinTx(ctx context.Context, fn func(ctx context.Context, repo application.Repository) error) error {
	return pgx.BeginFunc(ctx, m.pool, func(tx pgx.Tx) error {
		return fn(ctx, &Repository{queries: sqlcgen.New(tx), items: m.items(tx)})
	})
}

type Repository struct {
	queries *sqlcgen.Queries
	items   application.IncomeItems
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{queries: sqlcgen.New(pool)}
}

// NewRepositoryInTx is this module's repository on another module's transaction, for the ports it serves.
func NewRepositoryInTx(tx pgx.Tx) *Repository {
	return &Repository{queries: sqlcgen.New(tx)}
}

func (r *Repository) LockPlatforms(ctx context.Context) error {
	return r.queries.LockPlatforms(ctx)
}

func (r *Repository) IncomeItems() application.IncomeItems {
	return r.items
}

func (r *Repository) ListPeople(ctx context.Context) ([]domain.Person, error) {
	rows, err := r.queries.ListPeople(ctx)
	if err != nil {
		return nil, err
	}
	people := make([]domain.Person, 0, len(rows))
	for _, row := range rows {
		people = append(people, toPerson(row))
	}
	return people, nil
}

func (r *Repository) GetPerson(ctx context.Context, id string) (domain.Person, error) {
	uuid, err := parseID(id)
	if err != nil {
		return domain.Person{}, err
	}
	row, err := r.queries.GetPerson(ctx, uuid)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Person{}, domain.ErrPersonNotFound
	}
	if err != nil {
		return domain.Person{}, err
	}
	return toPerson(row), nil
}

func (r *Repository) CreatePerson(ctx context.Context, person domain.NewPerson) (domain.Person, error) {
	row, err := r.queries.CreatePerson(ctx, sqlcgen.CreatePersonParams{
		Name:          person.Name,
		IsOwner:       person.IsOwner,
		BillingMonths: monthsColumn(person.BillingMonths),
	})
	if err != nil {
		return domain.Person{}, explainWrite(err)
	}
	return toPerson(row), nil
}

func (r *Repository) UpdatePerson(ctx context.Context, person domain.Person) (domain.Person, error) {
	uuid, err := parseID(person.ID)
	if err != nil {
		return domain.Person{}, err
	}
	row, err := r.queries.UpdatePerson(ctx, sqlcgen.UpdatePersonParams{
		ID:            uuid,
		Name:          person.Name,
		IsOwner:       person.IsOwner,
		BillingMonths: monthsColumn(person.BillingMonths),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Person{}, domain.ErrPersonNotFound
	}
	if err != nil {
		return domain.Person{}, explainWrite(err)
	}
	return toPerson(row), nil
}

func (r *Repository) DeletePerson(ctx context.Context, id string) (bool, error) {
	uuid, err := parseID(id)
	if err != nil {
		return false, err
	}
	deleted, err := r.queries.DeletePerson(ctx, uuid)
	if err != nil {
		return false, explainWrite(err)
	}
	return deleted > 0, nil
}

func (r *Repository) ListPlatforms(ctx context.Context) ([]domain.Platform, error) {
	rows, err := r.queries.ListPlatforms(ctx)
	if err != nil {
		return nil, err
	}
	members, err := r.membersByPlatform(ctx)
	if err != nil {
		return nil, err
	}
	platforms := make([]domain.Platform, 0, len(rows))
	for _, row := range rows {
		platform := toPlatform(row)
		platform.Members = members[platform.ID]
		platforms = append(platforms, platform)
	}
	return platforms, nil
}

func (r *Repository) GetPlatform(ctx context.Context, id string) (domain.Platform, error) {
	uuid, err := parseID(id)
	if err != nil {
		return domain.Platform{}, err
	}
	row, err := r.queries.GetPlatform(ctx, uuid)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Platform{}, domain.ErrPlatformNotFound
	}
	if err != nil {
		return domain.Platform{}, err
	}
	return r.withMembers(ctx, toPlatform(row))
}

func (r *Repository) CreatePlatform(ctx context.Context, platform domain.NewPlatform) (domain.Platform, error) {
	cardID, err := parseOptionalID(platform.CardID)
	if err != nil {
		return domain.Platform{}, err
	}
	row, err := r.queries.CreatePlatform(ctx, sqlcgen.CreatePlatformParams{
		Name:        platform.Name,
		MonthlyCost: int64(platform.MonthlyCost),
		CardID:      cardID,
	})
	if err != nil {
		return domain.Platform{}, explainWrite(err)
	}
	return toPlatform(row), nil
}

func (r *Repository) UpdatePlatform(ctx context.Context, platform domain.Platform) (domain.Platform, error) {
	uuid, err := parseID(platform.ID)
	if err != nil {
		return domain.Platform{}, err
	}
	cardID, err := parseOptionalID(platform.CardID)
	if err != nil {
		return domain.Platform{}, err
	}
	row, err := r.queries.UpdatePlatform(ctx, sqlcgen.UpdatePlatformParams{
		ID:          uuid,
		Name:        platform.Name,
		MonthlyCost: int64(platform.MonthlyCost),
		CardID:      cardID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Platform{}, domain.ErrPlatformNotFound
	}
	if err != nil {
		return domain.Platform{}, explainWrite(err)
	}
	return r.withMembers(ctx, toPlatform(row))
}

func (r *Repository) DeletePlatform(ctx context.Context, id string) (bool, error) {
	uuid, err := parseID(id)
	if err != nil {
		return false, err
	}
	deleted, err := r.queries.DeletePlatform(ctx, uuid)
	if err != nil {
		return false, err
	}
	return deleted > 0, nil
}

func (r *Repository) AddMember(ctx context.Context, platformID, personID string) error {
	platform, err := parseID(platformID)
	if err != nil {
		return err
	}
	person, err := parseID(personID)
	if err != nil {
		return err
	}
	return explainWrite(r.queries.AddMember(ctx, sqlcgen.AddMemberParams{PlatformID: platform, PersonID: person}))
}

func (r *Repository) RemoveMember(ctx context.Context, platformID, personID string) error {
	platform, err := parseID(platformID)
	if err != nil {
		return err
	}
	person, err := parseID(personID)
	if err != nil {
		return err
	}
	return r.queries.RemoveMember(ctx, sqlcgen.RemoveMemberParams{PlatformID: platform, PersonID: person})
}

func (r *Repository) ListPayments(ctx context.Context) ([]domain.Payment, error) {
	rows, err := r.queries.ListPayments(ctx)
	if err != nil {
		return nil, err
	}
	payments := make([]domain.Payment, 0, len(rows))
	for _, row := range rows {
		payments = append(payments, toPayment(row))
	}
	return payments, nil
}

func (r *Repository) ListPersonPayments(ctx context.Context, personID string) ([]domain.Payment, error) {
	uuid, err := parseID(personID)
	if err != nil {
		return nil, err
	}
	rows, err := r.queries.ListPersonPayments(ctx, uuid)
	if err != nil {
		return nil, err
	}
	payments := make([]domain.Payment, 0, len(rows))
	for _, row := range rows {
		payments = append(payments, toPayment(row))
	}
	return payments, nil
}

func (r *Repository) GetPayment(ctx context.Context, id string) (domain.Payment, error) {
	uuid, err := parseID(id)
	if err != nil {
		return domain.Payment{}, err
	}
	row, err := r.queries.GetPayment(ctx, uuid)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Payment{}, domain.ErrPaymentNotFound
	}
	if err != nil {
		return domain.Payment{}, err
	}
	return toPayment(row), nil
}

func (r *Repository) CreatePayment(ctx context.Context, payment domain.NewPayment) (domain.Payment, error) {
	person, err := parseID(payment.PersonID)
	if err != nil {
		return domain.Payment{}, err
	}
	item, err := parseOptionalID(payment.PeriodItemID)
	if err != nil {
		return domain.Payment{}, err
	}
	row, err := r.queries.CreatePayment(ctx, sqlcgen.CreatePaymentParams{
		PersonID:      person,
		PaidAt:        pgtype.Date{Time: payment.PaidAt, Valid: true},
		MonthsCovered: monthsColumn(payment.MonthsCovered),
		Amount:        int64(payment.Amount),
		PeriodItemID:  item,
	})
	if err != nil {
		return domain.Payment{}, err
	}
	return toPayment(row), nil
}

func (r *Repository) DeletePayment(ctx context.Context, id string) (bool, error) {
	uuid, err := parseID(id)
	if err != nil {
		return false, err
	}
	deleted, err := r.queries.DeletePayment(ctx, uuid)
	if err != nil {
		return false, err
	}
	return deleted > 0, nil
}

func (r *Repository) DeletePaymentOfItem(ctx context.Context, itemID string) error {
	uuid, err := parseID(itemID)
	if err != nil {
		return err
	}
	_, err = r.queries.DeletePaymentOfItem(ctx, uuid)
	return err
}

func (r *Repository) withMembers(ctx context.Context, platform domain.Platform) (domain.Platform, error) {
	members, err := r.membersByPlatform(ctx)
	if err != nil {
		return domain.Platform{}, err
	}
	platform.Members = members[platform.ID]
	return platform, nil
}

func (r *Repository) membersByPlatform(ctx context.Context) (map[string][]domain.Member, error) {
	rows, err := r.queries.ListMembers(ctx)
	if err != nil {
		return nil, err
	}
	members := make(map[string][]domain.Member)
	for _, row := range rows {
		platformID := row.PlatformID.String()
		members[platformID] = append(members[platformID], domain.Member{PersonID: row.PersonID.String(), Name: row.Name})
	}
	return members, nil
}

// explainWrite turns what only the database can check at write time into what it means.
func explainWrite(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.ConstraintName {
	case ownerConstraint:
		return domain.ErrOwnerExists
	case paymentPersonConstraint:
		return domain.ErrPersonHasPayments
	case platformCardConstraint:
		return &domain.ValidationError{Violations: []domain.Violation{{Field: "cardId", Message: "must be an existing card"}}}
	case memberPlatformConstraint:
		return domain.ErrPlatformNotFound
	case memberPersonConstraint:
		return domain.ErrPersonNotFound
	default:
		return err
	}
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

func monthsColumn(months int) int32 {
	return int32(months) //nolint:gosec // The domain keeps months to 1-120.
}

func toPerson(row sqlcgen.Person) domain.Person {
	return domain.Person{
		ID:            row.ID.String(),
		Name:          row.Name,
		IsOwner:       row.IsOwner,
		BillingMonths: int(row.BillingMonths),
		CreatedAt:     row.CreatedAt.Time,
		UpdatedAt:     row.UpdatedAt.Time,
	}
}

func toPlatform(row sqlcgen.Platform) domain.Platform {
	return domain.Platform{
		ID:          row.ID.String(),
		Name:        row.Name,
		MonthlyCost: kernel.Money(row.MonthlyCost),
		CardID:      fromOptionalID(row.CardID),
		CreatedAt:   row.CreatedAt.Time,
		UpdatedAt:   row.UpdatedAt.Time,
	}
}

func toPayment(row sqlcgen.PlatformPayment) domain.Payment {
	return domain.Payment{
		ID:            row.ID.String(),
		PersonID:      row.PersonID.String(),
		PaidAt:        calendarDate(row.PaidAt),
		MonthsCovered: int(row.MonthsCovered),
		Amount:        kernel.Money(row.Amount),
		CoveredUntil:  calendarDate(row.CoveredUntil),
		PeriodItemID:  fromOptionalID(row.PeriodItemID),
		CreatedAt:     row.CreatedAt.Time,
		UpdatedAt:     row.UpdatedAt.Time,
	}
}

// calendarDate carries a date at midnight UTC, as every date in the domain is.
func calendarDate(date pgtype.Date) time.Time {
	year, month, day := date.Time.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}
