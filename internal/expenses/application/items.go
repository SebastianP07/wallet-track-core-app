package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

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

// OverdueList is what closed months left unsettled, whichever month is being looked at (D7).
type OverdueList struct {
	Items  []ItemView
	Totals domain.OverdueTotals
}

type ListOverdueItems struct {
	repo  Repository
	clock Clock
}

func NewListOverdueItems(repo Repository, clock Clock) *ListOverdueItems {
	return &ListOverdueItems{repo: repo, clock: clock}
}

func (u *ListOverdueItems) Execute(ctx context.Context) (OverdueList, error) {
	items, err := u.repo.ListOverdueItems(ctx)
	if err != nil {
		return OverdueList{}, fmt.Errorf("list overdue items: %w", err)
	}
	views := make([]ItemView, 0, len(items))
	for _, item := range items {
		views = append(views, view(item, u.clock))
	}
	return OverdueList{Items: views, Totals: domain.ComputeOverdueTotals(items)}, nil
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

// Execute records the member's payment when the item is person-linked (D4, D8). A repeat records nothing.
func (u *MarkItemDone) Execute(ctx context.Context, id string) (ItemView, error) {
	now := u.clock.Now()
	return changeStatus(ctx, u.tx, u.clock, id, func(item domain.Item, _ domain.Period) domain.Item {
		return item.Done(now)
	}, func(ctx context.Context, payments MemberPayments, item domain.Item) error {
		return payments.RecordForItem(ctx, *item.PersonID, item.ID, item.Month)
	})
}

type UnmarkItemDone struct {
	tx    TxManager
	clock Clock
}

func NewUnmarkItemDone(tx TxManager, clock Clock) *UnmarkItemDone {
	return &UnmarkItemDone{tx: tx, clock: clock}
}

// Execute removes the payment that marking this item recorded, and no other (D4, D8).
func (u *UnmarkItemDone) Execute(ctx context.Context, id string) (ItemView, error) {
	return changeStatus(ctx, u.tx, u.clock, id, domain.Item.Undone,
		func(ctx context.Context, payments MemberPayments, item domain.Item) error {
			return payments.RemoveForItem(ctx, item.ID)
		})
}

// changeStatus writes only a real change, so a repeated toggle leaves updated_at alone and a person-linked
// item's payment is touched once. The payment goes first: it takes the platforms lock, which a person's
// delete holds while it clears this row, so taking it before the row keeps the two from deadlocking.
func changeStatus(
	ctx context.Context,
	tx TxManager,
	clock Clock,
	id string,
	change func(domain.Item, domain.Period) domain.Item,
	syncPayment func(context.Context, MemberPayments, domain.Item) error,
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
		result = change(item, period)
		if result.Status == item.Status {
			return nil
		}
		if item.PersonID != nil {
			if err := syncPayment(ctx, repo.MemberPayments(), item); err != nil {
				return err
			}
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

// MemberItems is what the platforms module asks of this one when a payment is recorded or deleted (D4,
// D8). Built on a repository from WithinTx, it runs in that payment's transaction.
type MemberItems struct {
	repo Repository
}

func NewMemberItems(repo Repository) *MemberItems {
	return &MemberItems{repo: repo}
}

// LockMonth is no error for a month not opened: there is no item to race.
func (m *MemberItems) LockMonth(ctx context.Context, month kernel.YearMonth) error {
	_, err := m.repo.LockPeriod(ctx, month)
	if errors.Is(err, domain.ErrPeriodNotFound) {
		return nil
	}
	return err
}

// LockMonthOfItem is no error for an item already gone.
func (m *MemberItems) LockMonthOfItem(ctx context.Context, itemID string) error {
	_, err := m.repo.LockPeriodOfItem(ctx, itemID)
	if errors.Is(err, domain.ErrItemNotFound) {
		return nil
	}
	return err
}

// MarkReceived returns the id of the item it marked. An item already received keeps its own payment, so it
// returns nil, as it does when the person has no item that month.
func (m *MemberItems) MarkReceived(ctx context.Context, personID string, month kernel.YearMonth, now time.Time) (*string, error) {
	item, err := m.repo.GetMemberItem(ctx, personID, month)
	if errors.Is(err, domain.ErrItemNotFound) {
		return nil, nil //nolint:nilnil // No item is an answer here, not a failure.
	}
	if err != nil {
		return nil, err
	}
	if item.Status == domain.StatusDone {
		return nil, nil //nolint:nilnil // The answer is that this payment settles no item.
	}
	if _, err := m.repo.SetItemStatus(ctx, item.Done(now)); err != nil {
		return nil, err
	}
	return &item.ID, nil
}

// Unmark is no error for an item already gone. The caller holds the month's row.
func (m *MemberItems) Unmark(ctx context.Context, itemID string) error {
	period, err := m.repo.LockPeriodOfItem(ctx, itemID)
	if errors.Is(err, domain.ErrItemNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	item, err := m.repo.GetItem(ctx, itemID)
	if err != nil {
		return err
	}
	undone := item.Undone(period)
	if undone.Status == item.Status {
		return nil
	}
	_, err = m.repo.SetItemStatus(ctx, undone)
	return err
}
