package application

import (
	"context"
	"fmt"
	"slices"

	"github.com/SebastianP07/wallet-track-core-app/internal/accounts/domain"
)

// AccountList is every account with the totals the frontend must never compute (D1).
type AccountList struct {
	Items  []domain.Account
	Totals domain.Totals
}

type ListAccounts struct {
	repo Repository
}

func NewListAccounts(repo Repository) *ListAccounts {
	return &ListAccounts{repo: repo}
}

func (u *ListAccounts) Execute(ctx context.Context) (AccountList, error) {
	accounts, err := u.repo.List(ctx)
	if err != nil {
		return AccountList{}, fmt.Errorf("list accounts: %w", err)
	}
	totals, err := domain.ComputeTotals(accounts)
	if err != nil {
		return AccountList{}, fmt.Errorf("total accounts: %w", err)
	}
	return AccountList{Items: accounts, Totals: totals}, nil
}

type GetAccount struct {
	repo Repository
}

func NewGetAccount(repo Repository) *GetAccount {
	return &GetAccount{repo: repo}
}

func (u *GetAccount) Execute(ctx context.Context, id string) (domain.Account, error) {
	return u.repo.Get(ctx, id)
}

type CreateAccount struct {
	tx TxManager
}

func NewCreateAccount(tx TxManager) *CreateAccount {
	return &CreateAccount{tx: tx}
}

func (u *CreateAccount) Execute(ctx context.Context, account domain.NewAccount) (domain.Account, error) {
	var created domain.Account
	err := u.tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		existing, err := lockAndList(ctx, repo)
		if err != nil {
			return err
		}
		// Only the kind and balance of the new row count toward a total.
		if _, err := domain.ComputeTotals(append(existing, domain.Account{Kind: account.Kind, Balance: account.Balance})); err != nil {
			return err
		}
		created, err = repo.Create(ctx, account)
		return err
	})
	return created, err
}

type UpdateAccount struct {
	tx TxManager
}

func NewUpdateAccount(tx TxManager) *UpdateAccount {
	return &UpdateAccount{tx: tx}
}

func (u *UpdateAccount) Execute(ctx context.Context, id string, changes domain.Changes) (domain.Account, error) {
	var updated domain.Account
	err := u.tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		accounts, err := lockAndList(ctx, repo)
		if err != nil {
			return err
		}
		index, err := indexOf(accounts, id)
		if err != nil {
			return err
		}
		accounts[index] = changes.ApplyTo(accounts[index])
		if _, err := domain.ComputeTotals(accounts); err != nil {
			return err
		}
		updated, err = repo.Update(ctx, accounts[index])
		return err
	})
	return updated, err
}

type DeleteAccount struct {
	tx TxManager
}

func NewDeleteAccount(tx TxManager) *DeleteAccount {
	return &DeleteAccount{tx: tx}
}

// Execute checks the totals too: deleting a large negative balance can raise them past the bound.
func (u *DeleteAccount) Execute(ctx context.Context, id string) error {
	return u.tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		accounts, err := lockAndList(ctx, repo)
		if err != nil {
			return err
		}
		index, err := indexOf(accounts, id)
		if err != nil {
			return err
		}
		if _, err := domain.ComputeTotals(slices.Delete(accounts, index, index+1)); err != nil {
			return err
		}
		return repo.Delete(ctx, id)
	})
}

func lockAndList(ctx context.Context, repo Repository) ([]domain.Account, error) {
	if err := repo.LockWrites(ctx); err != nil {
		return nil, fmt.Errorf("lock account writes: %w", err)
	}
	accounts, err := repo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	return accounts, nil
}

func indexOf(accounts []domain.Account, id string) (int, error) {
	index := slices.IndexFunc(accounts, func(account domain.Account) bool { return account.ID == id })
	if index < 0 {
		return 0, domain.ErrNotFound
	}
	return index, nil
}
