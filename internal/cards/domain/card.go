// Package domain holds what the app knows about credit cards (D3).
package domain

import "time"

type Card struct {
	ID           string
	Name         string
	Issuer       string
	StatementDay int
	DueDay       int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
