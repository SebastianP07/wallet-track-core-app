// Package application holds the tags use cases. It reaches infrastructure only through the port below.
package application

import (
	"context"

	"github.com/SebastianP07/wallet-track-core-app/internal/tags/domain"
)

type Repository interface {
	List(ctx context.Context) ([]domain.Tag, error)
	// Create and Update return domain.ErrNameTaken when another tag has the name.
	Create(ctx context.Context, tag domain.NewTag) (domain.Tag, error)
	Update(ctx context.Context, id string, changes domain.Changes) (domain.Tag, error)
	Delete(ctx context.Context, id string) (found bool, err error)
}

type ListTags struct {
	repo Repository
}

func NewListTags(repo Repository) *ListTags {
	return &ListTags{repo: repo}
}

func (u *ListTags) Execute(ctx context.Context) ([]domain.Tag, error) {
	return u.repo.List(ctx)
}

type CreateTag struct {
	repo Repository
}

func NewCreateTag(repo Repository) *CreateTag {
	return &CreateTag{repo: repo}
}

func (u *CreateTag) Execute(ctx context.Context, tag domain.NewTag) (domain.Tag, error) {
	return u.repo.Create(ctx, tag)
}

type UpdateTag struct {
	repo Repository
}

func NewUpdateTag(repo Repository) *UpdateTag {
	return &UpdateTag{repo: repo}
}

func (u *UpdateTag) Execute(ctx context.Context, id string, changes domain.Changes) (domain.Tag, error) {
	return u.repo.Update(ctx, id, changes)
}

type DeleteTag struct {
	repo Repository
}

func NewDeleteTag(repo Repository) *DeleteTag {
	return &DeleteTag{repo: repo}
}

// Execute keeps the items and templates that had the tag; the foreign keys only clear their link.
func (u *DeleteTag) Execute(ctx context.Context, id string) error {
	found, err := u.repo.Delete(ctx, id)
	if err != nil {
		return err
	}
	if !found {
		return domain.ErrNotFound
	}
	return nil
}
