// Package postgres implements the accounts repository and transactions on PostgreSQL.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SebastianP07/wallet-track-core-app/internal/accounts/adapters/postgres/sqlcgen"
	"github.com/SebastianP07/wallet-track-core-app/internal/accounts/application"
	"github.com/SebastianP07/wallet-track-core-app/internal/accounts/domain"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
)

var (
	_ application.Repository = (*Repository)(nil)
	_ application.TxManager  = (*TxManager)(nil)
)

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

func (r *Repository) List(ctx context.Context) ([]domain.Account, error) {
	rows, err := r.queries.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	accounts := make([]domain.Account, 0, len(rows))
	for _, row := range rows {
		accounts = append(accounts, toDomain(row))
	}
	return accounts, nil
}

func (r *Repository) Get(ctx context.Context, id string) (domain.Account, error) {
	uuid, err := parseID(id)
	if err != nil {
		return domain.Account{}, err
	}
	row, err := r.queries.GetAccount(ctx, uuid)
	return toDomainOrNotFound(row, err)
}

func (r *Repository) Create(ctx context.Context, account domain.NewAccount) (domain.Account, error) {
	row, err := r.queries.CreateAccount(ctx, sqlcgen.CreateAccountParams{
		Name:    account.Name,
		Kind:    string(account.Kind),
		Balance: int64(account.Balance),
	})
	if err != nil {
		return domain.Account{}, err
	}
	return toDomain(row), nil
}

func (r *Repository) Update(ctx context.Context, account domain.Account) (domain.Account, error) {
	uuid, err := parseID(account.ID)
	if err != nil {
		return domain.Account{}, err
	}
	row, err := r.queries.UpdateAccount(ctx, sqlcgen.UpdateAccountParams{
		ID:      uuid,
		Name:    account.Name,
		Kind:    string(account.Kind),
		Balance: int64(account.Balance),
	})
	return toDomainOrNotFound(row, err)
}

func (r *Repository) Delete(ctx context.Context, id string) error {
	uuid, err := parseID(id)
	if err != nil {
		return err
	}
	return r.queries.DeleteAccount(ctx, uuid)
}

func (r *Repository) LockWrites(ctx context.Context) error {
	return r.queries.LockAccountWrites(ctx)
}

func parseID(id string) (pgtype.UUID, error) {
	var uuid pgtype.UUID
	if err := uuid.Scan(id); err != nil {
		return pgtype.UUID{}, fmt.Errorf("parse account id: %w", err)
	}
	return uuid, nil
}

func toDomainOrNotFound(row sqlcgen.Account, err error) (domain.Account, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Account{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Account{}, err
	}
	return toDomain(row), nil
}

func toDomain(row sqlcgen.Account) domain.Account {
	return domain.Account{
		ID:        row.ID.String(),
		Name:      row.Name,
		Kind:      domain.Kind(row.Kind),
		Balance:   kernel.Money(row.Balance),
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}
}
