package application

import (
	"context"

	"github.com/SebastianP07/wallet-track-core-app/internal/periods/domain"
)

type ListTemplates struct {
	repo Repository
}

func NewListTemplates(repo Repository) *ListTemplates {
	return &ListTemplates{repo: repo}
}

func (u *ListTemplates) Execute(ctx context.Context, filter TemplateFilter) ([]domain.Template, error) {
	return u.repo.ListTemplates(ctx, filter)
}

type GetTemplate struct {
	repo Repository
}

func NewGetTemplate(repo Repository) *GetTemplate {
	return &GetTemplate{repo: repo}
}

func (u *GetTemplate) Execute(ctx context.Context, id string) (domain.Template, error) {
	return u.repo.GetTemplate(ctx, id)
}

type CreateTemplate struct {
	repo Repository
}

func NewCreateTemplate(repo Repository) *CreateTemplate {
	return &CreateTemplate{repo: repo}
}

func (u *CreateTemplate) Execute(ctx context.Context, template domain.NewTemplate) (domain.Template, error) {
	return u.repo.CreateTemplate(ctx, template)
}

type UpdateTemplate struct {
	tx TxManager
}

func NewUpdateTemplate(tx TxManager) *UpdateTemplate {
	return &UpdateTemplate{tx: tx}
}

// Execute touches only the template, so items already created keep what they were cloned with.
func (u *UpdateTemplate) Execute(ctx context.Context, id string, changes domain.TemplateChanges) (domain.Template, error) {
	var updated domain.Template
	err := u.tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		template, err := repo.LockTemplate(ctx, id)
		if err != nil {
			return err
		}
		changed, err := changes.ApplyTo(template)
		if err != nil {
			return err
		}
		updated, err = repo.UpdateTemplate(ctx, changed)
		return err
	})
	return updated, err
}

type DeleteTemplate struct {
	repo Repository
}

func NewDeleteTemplate(repo Repository) *DeleteTemplate {
	return &DeleteTemplate{repo: repo}
}

// Execute keeps the items the template created; the foreign key only clears their link.
func (u *DeleteTemplate) Execute(ctx context.Context, id string) error {
	found, err := u.repo.DeleteTemplate(ctx, id)
	if err != nil {
		return err
	}
	if !found {
		return domain.ErrTemplateNotFound
	}
	return nil
}
