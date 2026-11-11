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
	// SetOpeningBalance returns domain.ErrPeriodNotFound for a month not opened. A nil amount clears it.
	SetOpeningBalance(ctx context.Context, month kernel.YearMonth, amount *kernel.Money) (domain.Period, error)
	// CarryPendingExpenses makes the month's pending fixed and occasional items overdue, owed in to (D7).
	CarryPendingExpenses(ctx context.Context, periodID string, to kernel.YearMonth) error
	// CarryOwedExpenses moves the overdue expenses owed in from, whatever their month of origin, to to (D7).
	CarryOwedExpenses(ctx context.Context, from, to kernel.YearMonth) error
	// MarkPendingIncomeOverdue leaves the month's unreceived income overdue in the month itself (D7).
	MarkPendingIncomeOverdue(ctx context.Context, periodID string) error
	// AddItem does nothing when the month already has an item of that template.
	AddItem(ctx context.Context, item domain.NewItem) error
	// AddMemberItem does nothing when the month already has an income item of that person (D4).
	AddMemberItem(ctx context.Context, item domain.NewMemberItem) error
	// AddPlatformItem does nothing when the month already has a card charge of that platform (D14).
	AddPlatformItem(ctx context.Context, item domain.NewPlatformItem) error
	// MemberIncome works inside this repository's transaction. Only a repository from WithinTx has it.
	MemberIncome() MemberIncome
	// PlatformCharges works inside this repository's transaction. Only a repository from WithinTx has it.
	PlatformCharges() PlatformCharges
	// AccountBalances works inside this repository's transaction. Only a repository from WithinTx has it.
	AccountBalances() AccountBalances

	ListTemplates(ctx context.Context, filter TemplateFilter) ([]domain.Template, error)
	// LockActiveTemplates holds the rows until the transaction ends, so that a template being cloned
	// cannot be deleted before its item is stored.
	LockActiveTemplates(ctx context.Context) ([]domain.Template, error)
	GetTemplate(ctx context.Context, id string) (domain.Template, error)
	LockTemplate(ctx context.Context, id string) (domain.Template, error)
	CreateTemplate(ctx context.Context, template domain.NewTemplate) (domain.Template, error)
	UpdateTemplate(ctx context.Context, template domain.Template) (domain.Template, error)
	DeleteTemplate(ctx context.Context, id string) (found bool, err error)
	// ReorderTemplates gives the listed templates, in the listed order, the positions they hold between them now,
	// so templates left out keep theirs (D13).
	ReorderTemplates(ctx context.Context, ids []string) error
}

// MemberIncome is what this module asks of the platforms module when opening a month: who owes it, and
// how much (D4). A sum beyond the bound comes back as kernel.ErrMoneyOutOfRange.
type MemberIncome interface {
	Owing(ctx context.Context, month kernel.YearMonth) ([]domain.OwingMember, error)
}

// PlatformCharges is what this module asks of the platforms module when opening a month: every platform
// with a card, whose card charge the month gets (D14).
type PlatformCharges interface {
	WithCard(ctx context.Context) ([]domain.PlatformCharge, error)
}

// AccountBalances is what this module asks of the accounts module when opening a month: Total actual, the
// month's opening balance (D6, D9). A sum beyond the bound comes back as kernel.ErrMoneyOutOfRange.
type AccountBalances interface {
	CurrentTotal(ctx context.Context) (kernel.Money, error)
}

type TxManager interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context, repo Repository) error) error
}
