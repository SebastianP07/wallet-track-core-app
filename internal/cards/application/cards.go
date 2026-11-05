// Package application holds the credit cards use cases. It reaches infrastructure only through
// the port below.
package application

import (
	"context"
	"fmt"

	"github.com/SebastianP07/wallet-track-core-app/internal/cards/domain"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
)

type Repository interface {
	List(ctx context.Context) ([]domain.Card, error)
	Get(ctx context.Context, id string) (domain.Card, error)
	Create(ctx context.Context, card domain.NewCard) (domain.Card, error)
	Update(ctx context.Context, id string, changes domain.Changes) (domain.Card, error)
	Delete(ctx context.Context, id string) (found bool, err error)
	// SumCharges returns domain.ErrPeriodNotFound for a month that has not been opened.
	SumCharges(ctx context.Context, month kernel.YearMonth) ([]domain.CardTotal, error)
}

type ListCards struct {
	repo Repository
}

func NewListCards(repo Repository) *ListCards {
	return &ListCards{repo: repo}
}

func (u *ListCards) Execute(ctx context.Context) ([]domain.Card, error) {
	return u.repo.List(ctx)
}

type GetCard struct {
	repo Repository
}

func NewGetCard(repo Repository) *GetCard {
	return &GetCard{repo: repo}
}

func (u *GetCard) Execute(ctx context.Context, id string) (domain.Card, error) {
	return u.repo.Get(ctx, id)
}

type CreateCard struct {
	repo Repository
}

func NewCreateCard(repo Repository) *CreateCard {
	return &CreateCard{repo: repo}
}

func (u *CreateCard) Execute(ctx context.Context, card domain.NewCard) (domain.Card, error) {
	return u.repo.Create(ctx, card)
}

type UpdateCard struct {
	repo Repository
}

func NewUpdateCard(repo Repository) *UpdateCard {
	return &UpdateCard{repo: repo}
}

func (u *UpdateCard) Execute(ctx context.Context, id string, changes domain.Changes) (domain.Card, error) {
	return u.repo.Update(ctx, id, changes)
}

type DeleteCard struct {
	repo Repository
}

func NewDeleteCard(repo Repository) *DeleteCard {
	return &DeleteCard{repo: repo}
}

// Execute leaves the rest to the database: a trigger deactivates the card's active card charge
// templates and the foreign keys clear the card on templates, items and platforms, all in this delete.
func (u *DeleteCard) Execute(ctx context.Context, id string) error {
	found, err := u.repo.Delete(ctx, id)
	if err != nil {
		return err
	}
	if !found {
		return domain.ErrNotFound
	}
	return nil
}

type GetCardTotals struct {
	repo Repository
}

func NewGetCardTotals(repo Repository) *GetCardTotals {
	return &GetCardTotals{repo: repo}
}

func (u *GetCardTotals) Execute(ctx context.Context, month kernel.YearMonth) (domain.MonthTotals, error) {
	items, err := u.repo.SumCharges(ctx, month)
	if err != nil {
		return domain.MonthTotals{}, err
	}
	totals, err := domain.NewMonthTotals(items)
	if err != nil {
		return domain.MonthTotals{}, fmt.Errorf("total card charges: %w", err)
	}
	return totals, nil
}
