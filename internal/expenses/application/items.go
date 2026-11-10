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
	owed, err := u.repo.ListOwedItems(ctx, month)
	if err != nil {
		return ItemList{}, fmt.Errorf("list owed items: %w", err)
	}
	totals, err := domain.ComputeTotals(items, owed)
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

// OverdueList is a month's Overdue section (D7): the expenses earlier months left unpaid that it owes,
// oldest first, then the income every closed month left unreceived, as before.
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

func (u *ListOverdueItems) Execute(ctx context.Context, month kernel.YearMonth) (OverdueList, error) {
	if _, err := u.repo.GetPeriod(ctx, month); err != nil {
		return OverdueList{}, err
	}
	owed, err := u.repo.ListOwedItems(ctx, month)
	if err != nil {
		return OverdueList{}, fmt.Errorf("list owed items: %w", err)
	}
	income, err := u.repo.ListOverdueIncome(ctx)
	if err != nil {
		return OverdueList{}, fmt.Errorf("list overdue income: %w", err)
	}
	// A paid one stays in the month's totals but leaves the list.
	items := slices.DeleteFunc(owed, func(item domain.Item) bool { return item.Status != domain.StatusOverdue })
	items = append(items, income...)
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
		if err != nil || !item.RepeatsMonthly {
			return err
		}
		// The item goes first, so its own rules, such as an existing tag, answer before the template's.
		templateID, err := repo.Templates().CreateFor(ctx, string(item.Kind), item.Name, item.Amount, dueDayOf(item), item.TagID)
		if err != nil {
			return fmt.Errorf("create template: %w", err)
		}
		created, err = repo.SetItemTemplate(ctx, created.ID, templateID)
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
		if items[index].KeptByPlatform() {
			return domain.ErrKeptByPlatform
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

// Execute removes the item from its own month only. With stopRepeating it also deactivates the item's
// template in the same transaction, so months opened from now on stop getting it. Items of other months,
// paid or not, stay as they are. An overdue item leaves the totals of the month that owed it (D1, D7). A
// platform's card charge goes only with its platform's card (D14).
func (u *DeleteItem) Execute(ctx context.Context, id string, stopRepeating bool) error {
	return u.tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		if _, err := repo.LockPeriodOfItem(ctx, id); err != nil {
			return err
		}
		item, err := repo.GetItem(ctx, id)
		if err != nil {
			return err
		}
		if item.KeptByPlatform() {
			return domain.ErrKeptByPlatform
		}
		if stopRepeating && item.TemplateID != nil {
			if err := repo.Templates().Stop(ctx, *item.TemplateID); err != nil {
				return fmt.Errorf("stop template: %w", err)
			}
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

type ReorderItems struct {
	tx TxManager
}

func NewReorderItems(tx TxManager) *ReorderItems {
	return &ReorderItems{tx: tx}
}

// Execute stores the owner's order of one kind's items in a month, ids being every one of them in the new order,
// and puts the templates of those items in the same order, so the next month opened follows it (D13). Order
// moves no total, so a closed month can be reordered too. The month's row is held, so no item is added or removed
// meanwhile.
func (u *ReorderItems) Execute(ctx context.Context, month kernel.YearMonth, kind domain.ItemKind, ids []string) error {
	return u.tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		period, err := repo.LockPeriod(ctx, month)
		if err != nil {
			return err
		}
		current, err := repo.ListKindOrder(ctx, period.ID, kind)
		if err != nil {
			return fmt.Errorf("list items in order: %w", err)
		}
		templates, err := domain.TemplatesInOrder(current, ids)
		if err != nil {
			return err
		}
		if err := repo.SetItemOrder(ctx, ids); err != nil {
			return fmt.Errorf("set item order: %w", err)
		}
		if len(templates) == 0 {
			return nil
		}
		if err := repo.Templates().Reorder(ctx, templates); err != nil {
			return fmt.Errorf("reorder templates: %w", err)
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
	return changeStatus(ctx, u.tx, u.clock, id, func(_ context.Context, _ Repository, item domain.Item, _ domain.Period) (domain.Item, error) {
		return item.Done(now), nil
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

// Execute removes the payment that marking this item recorded, and no other (D4, D8). An expense of a
// closed month goes back to overdue in the first month from the one that owed it that is not closed (D7).
func (u *UnmarkItemDone) Execute(ctx context.Context, id string) (ItemView, error) {
	return changeStatus(ctx, u.tx, u.clock, id, undo,
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
	change func(context.Context, Repository, domain.Item, domain.Period) (domain.Item, error),
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
		result, err = change(ctx, repo, item, period)
		if err != nil {
			return err
		}
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

// undo locks each month it asks about, after the item's own, so it takes period rows in ascending order as a
// close does.
func undo(ctx context.Context, repo Repository, item domain.Item, period domain.Period) (domain.Item, error) {
	if !item.NeedsOwedMonth(period) {
		return item.Undone(period, kernel.YearMonth{}), nil
	}
	owedIn, err := firstMonthNotClosed(ctx, repo, item.UndoneOwedFrom())
	if err != nil {
		return domain.Item{}, err
	}
	return item.Undone(period, owedIn), nil
}

func firstMonthNotClosed(ctx context.Context, repo Repository, from kernel.YearMonth) (kernel.YearMonth, error) {
	return kernel.FirstMonthNotClosed(from, func(month kernel.YearMonth) (bool, error) {
		period, err := repo.LockPeriod(ctx, month)
		if errors.Is(err, domain.ErrPeriodNotFound) {
			return false, nil
		}
		return period.Status == domain.PeriodClosed, err
	})
}

// dueDayOf is the day of the month a fixed item's template keeps (D11). Other kinds have none.
func dueDayOf(item domain.NewItem) *int {
	if item.Kind != domain.ItemFixed || item.DueDate == nil {
		return nil
	}
	day := item.DueDate.Day()
	return &day
}

// checkTotals bounds a month's own items. What other months leave owed in it is capped instead (D7).
func checkTotals(items []domain.Item) error {
	if _, err := domain.ComputeTotals(items, nil); err != nil {
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
	// Only person-linked income comes here, which no later month ever owes (D7).
	undone, err := undo(ctx, m.repo, item, period)
	if err != nil {
		return err
	}
	if undone.Status == item.Status {
		return nil
	}
	_, err = m.repo.SetItemStatus(ctx, undone)
	return err
}

// PlatformCharges is what the platforms module asks of this one when a platform changes, so that every open
// month's card charge of that platform follows it in the same transaction (D14). Built on a repository from
// WithinTx, it runs in that platform write's transaction.
type PlatformCharges struct {
	repo Repository
}

func NewPlatformCharges(repo Repository) *PlatformCharges {
	return &PlatformCharges{repo: repo}
}

// LockOpenMonths holds every open month's row, oldest first, as every item write takes its month first.
func (c *PlatformCharges) LockOpenMonths(ctx context.Context) error {
	return c.repo.LockOpenPeriods(ctx)
}

// Sync makes each open month's charge of the platform match it: a platform with a card gets a pending charge
// where a month has none, and its pending ones take its name, cost and card; a platform with no card loses
// its pending ones. Done charges and closed months stay as they are. It returns kernel.ErrMoneyOutOfRange when
// an open month's card charges would add up to more than the contract can carry.
func (c *PlatformCharges) Sync(ctx context.Context, platformID, name string, amount kernel.Money, cardID *string) error {
	if cardID == nil {
		return c.Remove(ctx, platformID)
	}
	charge := domain.PlatformCharge{PlatformID: platformID, Name: name, Amount: amount, CardID: *cardID}
	if err := c.repo.UpdatePlatformCharges(ctx, charge); err != nil {
		return fmt.Errorf("update platform charges: %w", err)
	}
	if err := c.repo.AddPlatformCharges(ctx, charge); err != nil {
		return fmt.Errorf("add platform charges: %w", err)
	}
	outOfRange, err := c.repo.OpenCardChargesOutOfRange(ctx)
	if err != nil {
		return fmt.Errorf("check card charge totals: %w", err)
	}
	if outOfRange {
		return fmt.Errorf("%w: card charges of an open month", kernel.ErrMoneyOutOfRange)
	}
	return nil
}

// Remove deletes the platform's pending charges of open months, before the platform itself goes. Done ones
// stay, unlinked once the platform is deleted, as history.
func (c *PlatformCharges) Remove(ctx context.Context, platformID string) error {
	if err := c.repo.RemovePlatformCharges(ctx, platformID); err != nil {
		return fmt.Errorf("remove platform charges: %w", err)
	}
	return nil
}
