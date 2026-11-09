package application

import (
	"context"
	"errors"

	"github.com/SebastianP07/wallet-track-core-app/internal/periods/domain"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
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

// ItemTemplates is what the expenses module asks of this one when an item is added to repeat monthly or
// removed to stop repeating. Built on a repository from another module's transaction, it runs in it.
type ItemTemplates struct {
	repo Repository
}

func NewItemTemplates(repo Repository) *ItemTemplates {
	return &ItemTemplates{repo: repo}
}

// CreateFor stores an active template with the item's values; the month it was added to already has its item.
func (t *ItemTemplates) CreateFor(ctx context.Context, kind, name string, amount kernel.Money, dueDay *int, tagID *string) (string, error) {
	template, err := domain.CreateNewTemplate(domain.NewTemplateInput{
		Kind:   kind,
		Name:   name,
		Amount: int64(amount),
		DueDay: dueDay,
		TagID:  tagID,
	})
	if err != nil {
		return "", err
	}
	created, err := t.repo.CreateTemplate(ctx, template)
	if err != nil {
		return "", err
	}
	return created.ID, nil
}

// Reorder puts the templates in the order of ids, among the positions they hold now, so the next month opened gets
// their items in that order (D13).
func (t *ItemTemplates) Reorder(ctx context.Context, ids []string) error {
	return t.repo.ReorderTemplates(ctx, ids)
}

// Stop keeps the template, inactive, so the items it created keep their link and it can be resumed from the
// templates section. One already inactive or deleted is left as it is.
func (t *ItemTemplates) Stop(ctx context.Context, id string) error {
	template, err := t.repo.LockTemplate(ctx, id)
	if errors.Is(err, domain.ErrTemplateNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !template.Active {
		return nil
	}
	template.Active = false
	_, err = t.repo.UpdateTemplate(ctx, template)
	return err
}
