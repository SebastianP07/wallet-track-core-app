package application

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/SebastianP07/wallet-track-core-app/internal/expenses/domain"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
)

// ItemView is an item with the urgency only the backend computes (D1, D11).
type ItemView struct {
	domain.Item
	Urgency domain.Urgency
}

func view(item domain.Item, clock Clock) ItemView {
	today := kernel.DateInBogota(clock.Now())
	return ItemView{Item: item, Urgency: domain.UrgencyOf(item.Status, item.DueDate, today)}
}

// ItemList is the items that pass the filter, with totals for the whole month (D5).
type ItemList struct {
	Items  []ItemView
	Totals domain.Totals
}

type ListItems struct {
	repo  Repository
	clock Clock
}

func NewListItems(repo Repository, clock Clock) *ListItems {
	return &ListItems{repo: repo, clock: clock}
}

func (u *ListItems) Execute(ctx context.Context, month kernel.YearMonth, filter domain.ItemFilter) (ItemList, error) {
	period, err := u.repo.GetPeriod(ctx, month)
	if err != nil {
		return ItemList{}, err
	}
	items, err := u.repo.ListItems(ctx, period.ID)
	if err != nil {
		return ItemList{}, fmt.Errorf("list items: %w", err)
	}
	totals, err := domain.ComputeTotals(items)
	if err != nil {
		return ItemList{}, fmt.Errorf("total items: %w", err)
	}
	views := make([]ItemView, 0, len(items))
	for _, item := range items {
		if filter.Matches(item) {
			views = append(views, view(item, u.clock))
		}
	}
	return ItemList{Items: views, Totals: totals}, nil
}

type GetItem struct {
	repo  Repository
	clock Clock
}

func NewGetItem(repo Repository, clock Clock) *GetItem {
	return &GetItem{repo: repo, clock: clock}
}

func (u *GetItem) Execute(ctx context.Context, id string) (ItemView, error) {
	item, err := u.repo.GetItem(ctx, id)
	if err != nil {
		return ItemView{}, err
	}
	return view(item, u.clock), nil
}

type CreateItem struct {
	tx    TxManager
	clock Clock
}

func NewCreateItem(tx TxManager, clock Clock) *CreateItem {
	return &CreateItem{tx: tx, clock: clock}
}

func (u *CreateItem) Execute(ctx context.Context, month kernel.YearMonth, item domain.NewItem) (ItemView, error) {
	var created domain.Item
	err := u.tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		period, err := repo.LockPeriod(ctx, month)
		if err != nil {
			return err
		}
		if period.Status == domain.PeriodClosed {
			return domain.ErrMonthClosed
		}
		existing, err := repo.ListItems(ctx, period.ID)
		if err != nil {
			return fmt.Errorf("list items: %w", err)
		}
		// Only the kind, status and amount of the new row count toward a total.
		next := domain.Item{Kind: item.Kind, Status: domain.StatusPending, Amount: item.Amount}
		if err := checkTotals(append(existing, next)); err != nil {
			return err
		}
		created, err = repo.CreateItem(ctx, period.ID, item)
		return err
	})
	if err != nil {
		return ItemView{}, err
	}
	return view(created, u.clock), nil
}

type UpdateItem struct {
	tx    TxManager
	clock Clock
}

func NewUpdateItem(tx TxManager, clock Clock) *UpdateItem {
	return &UpdateItem{tx: tx, clock: clock}
}

// Execute touches only this month's item, never its template. The contract allows it on a closed
// month too.
func (u *UpdateItem) Execute(ctx context.Context, id string, changes domain.ItemChanges) (ItemView, error) {
	var updated domain.Item
	err := u.tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		period, err := repo.LockPeriodOfItem(ctx, id)
		if err != nil {
			return err
		}
		items, err := repo.ListItems(ctx, period.ID)
		if err != nil {
			return fmt.Errorf("list items: %w", err)
		}
		index := slices.IndexFunc(items, func(item domain.Item) bool { return item.ID == id })
		if index < 0 {
			return domain.ErrItemNotFound
		}
		if err := items[index].RequireExpense(); err != nil {
			return err
		}
		changed, err := changes.ApplyTo(items[index])
		if err != nil {
			return err
		}
		items[index] = changed
		if err := checkTotals(items); err != nil {
			return err
		}
		updated, err = repo.UpdateItem(ctx, changed)
		return err
	})
	if err != nil {
		return ItemView{}, err
	}
	return view(updated, u.clock), nil
}

type DeleteItem struct {
	tx TxManager
}

func NewDeleteItem(tx TxManager) *DeleteItem {
	return &DeleteItem{tx: tx}
}

func (u *DeleteItem) Execute(ctx context.Context, id string) error {
	return u.tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		if _, err := repo.LockPeriodOfItem(ctx, id); err != nil {
			return err
		}
		item, err := repo.GetItem(ctx, id)
		if err != nil {
			return err
		}
		if err := item.RequireExpense(); err != nil {
			return err
		}
		found, err := repo.DeleteItem(ctx, id)
		if err != nil {
			return err
		}
		if !found {
			return domain.ErrItemNotFound
		}
		return nil
	})
}

type MarkItemDone struct {
	tx    TxManager
	clock Clock
}

func NewMarkItemDone(tx TxManager, clock Clock) *MarkItemDone {
	return &MarkItemDone{tx: tx, clock: clock}
}

func (u *MarkItemDone) Execute(ctx context.Context, id string) (ItemView, error) {
	now := u.clock.Now()
	return changeStatus(ctx, u.tx, u.clock, id, func(item domain.Item, _ domain.Period) domain.Item {
		return item.Done(now)
	})
}

type UnmarkItemDone struct {
	tx    TxManager
	clock Clock
}

func NewUnmarkItemDone(tx TxManager, clock Clock) *UnmarkItemDone {
	return &UnmarkItemDone{tx: tx, clock: clock}
}

func (u *UnmarkItemDone) Execute(ctx context.Context, id string) (ItemView, error) {
	return changeStatus(ctx, u.tx, u.clock, id, domain.Item.Undone)
}

// changeStatus writes only a real change, so a repeated toggle leaves updated_at alone.
func changeStatus(
	ctx context.Context,
	tx TxManager,
	clock Clock,
	id string,
	change func(domain.Item, domain.Period) domain.Item,
) (ItemView, error) {
	var result domain.Item
	err := tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		period, err := repo.LockPeriodOfItem(ctx, id)
		if err != nil {
			return err
		}
		item, err := repo.GetItem(ctx, id)
		if err != nil {
			return err
		}
		if err := item.RequireExpense(); err != nil {
			return err
		}
		result = change(item, period)
		if result.Status == item.Status {
			return nil
		}
		result, err = repo.SetItemStatus(ctx, result)
		return err
	})
	if err != nil {
		return ItemView{}, err
	}
	return view(result, clock), nil
}

func checkTotals(items []domain.Item) error {
	if _, err := domain.ComputeTotals(items); err != nil {
		if errors.Is(err, domain.ErrTotalOutOfRange) {
			return domain.TotalViolation()
		}
		return err
	}
	return nil
}
