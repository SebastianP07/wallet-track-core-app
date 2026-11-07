package application

import (
	"context"
	"fmt"

	"github.com/SebastianP07/wallet-track-core-app/internal/dashboard/domain"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
)

type TagsReader interface {
	List(ctx context.Context) ([]domain.Tag, error)
}

type ExportReaders struct {
	Periods  PeriodReader
	Accounts AccountsReader
	Items    ItemsReader
	Tags     TagsReader
}

// ExportMonth answers domain.ErrPeriodNotFound for a month not opened, as GetDashboard does.
type ExportMonth struct {
	readers ExportReaders
}

func NewExportMonth(readers ExportReaders) *ExportMonth {
	return &ExportMonth{readers: readers}
}

func (u *ExportMonth) Execute(ctx context.Context, month kernel.YearMonth) ([]domain.ExportRow, error) {
	if _, err := u.readers.Periods.Get(ctx, month); err != nil {
		return nil, err
	}
	items, err := u.readers.Items.List(ctx, month)
	if err != nil {
		return nil, fmt.Errorf("read items: %w", err)
	}
	accounts, err := u.readers.Accounts.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("read accounts: %w", err)
	}
	tags, err := u.readers.Tags.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("read tags: %w", err)
	}
	return domain.ExportRows(items.Items, accounts.Items, tags), nil
}
