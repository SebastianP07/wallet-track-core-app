package domain

import (
	"time"

	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
)

// accountKindPrefix keeps an account row from being read as an item: a balance is today's whichever month is
// exported (D6, D9), and summing it with the month's items would mean nothing.
const accountKindPrefix = "account_"

type Tag struct {
	ID   string
	Name string
}

// ExportRow is one line of a month's export. An account has no due date, status or tag.
type ExportRow struct {
	Kind    string
	Name    string
	Amount  kernel.Money
	DueDate *time.Time
	Status  string
	Tag     string
}

// ExportRows lists the month's items of every kind, then the accounts. No totals: those are the Dashboard's.
func ExportRows(items []Item, accounts []Account, tags []Tag) []ExportRow {
	tagNames := make(map[string]string, len(tags))
	for _, tag := range tags {
		tagNames[tag.ID] = tag.Name
	}
	rows := make([]ExportRow, 0, len(items)+len(accounts))
	for _, item := range items {
		row := ExportRow{
			Kind:    item.Kind,
			Name:    item.Name,
			Amount:  item.Amount,
			DueDate: item.DueDate,
			Status:  item.Status,
		}
		if item.TagID != nil {
			row.Tag = tagNames[*item.TagID]
		}
		rows = append(rows, row)
	}
	for _, account := range accounts {
		rows = append(rows, ExportRow{
			Kind:   accountKindPrefix + account.Kind,
			Name:   account.Name,
			Amount: account.Balance,
		})
	}
	return rows
}
