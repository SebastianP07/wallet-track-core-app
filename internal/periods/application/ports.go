// Package application holds the periods and templates use cases. It reaches infrastructure only
// through the ports below.
package application

import (
	"context"

	"github.com/SebastianP07/wallet-track-core-app/internal/periods/domain"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
)

// TemplateFilter narrows a template list; a nil Active means every template.
type TemplateFilter struct {
	Active *bool
}

type Repository interface {
	ListPeriods(ctx context.Context) ([]domain.Period, error)
	GetPeriod(ctx context.Context, month kernel.YearMonth) (domain.Period, error)
	// CreatePeriod reports false, with no period, when the month already exists. Against a concurrent
	// create it waits for that transaction to end.
	CreatePeriod(ctx context.Context, month kernel.YearMonth) (domain.Period, bool, error)
	// LockPeriod holds the month's row until the transaction ends.
	LockPeriod(ctx context.Context, month kernel.YearMonth) (domain.Period, error)
	MarkPeriodClosed(ctx context.Context, id string) (domain.Period, error)
	MarkPendingItemsOverdue(ctx context.Context, periodID string, kinds []domain.ItemKind) error
	// AddItem does nothing when the month already has an item of that template.
	AddItem(ctx context.Context, item domain.NewItem) error

	ListTemplates(ctx context.Context, filter TemplateFilter) ([]domain.Template, error)
	// LockActiveTemplates holds the rows until the transaction ends, so that a template being cloned
	// cannot be deleted before its item is stored.
	LockActiveTemplates(ctx context.Context) ([]domain.Template, error)
	GetTemplate(ctx context.Context, id string) (domain.Template, error)
	LockTemplate(ctx context.Context, id string) (domain.Template, error)
	CreateTemplate(ctx context.Context, template domain.NewTemplate) (domain.Template, error)
	UpdateTemplate(ctx context.Context, template domain.Template) (domain.Template, error)
	DeleteTemplate(ctx context.Context, id string) (found bool, err error)
}

type TxManager interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context, repo Repository) error) error
}
