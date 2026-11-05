// Package application holds the credit cards use cases. It reaches infrastructure only through
// the port below.
package application

import (
	"context"

	"github.com/SebastianP07/wallet-track-core-app/internal/cards/domain"
)

type Repository interface {
	List(ctx context.Context) ([]domain.Card, error)
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
