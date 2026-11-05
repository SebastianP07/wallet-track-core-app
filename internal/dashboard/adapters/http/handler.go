// Package http is the inbound HTTP adapter of the dashboard module. Every section keeps the wire shape it
// has at its own endpoint, as the contract's schemas say.
package http

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/SebastianP07/wallet-track-core-app/internal/dashboard/application"
	"github.com/SebastianP07/wallet-track-core-app/internal/dashboard/domain"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/platform"
)

const (
	basePath     = "/api/v1"
	dateLayout   = "2006-01-02"
	monthMessage = "must be a month as YYYY-MM, from 2000-01 on"
)

type Config struct {
	Get *application.GetDashboard
	// RequireSession comes from cmd/, so this module imports no other module's adapters.
	RequireSession platform.Middleware
	Logger         *slog.Logger
}

type Handler struct {
	Config
}

func NewHandler(cfg Config) *Handler {
	return &Handler{Config: cfg}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.Handle("GET "+basePath+"/dashboard", platform.Chain(http.HandlerFunc(h.get), h.RequireSession))
}

type periodResponse struct {
	ID        string    `json:"id"`
	Period    string    `json:"period"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type kpisResponse struct {
	CurrentTotal int64 `json:"currentTotal"`
	ToPay        int64 `json:"toPay"`
	ToReceive    int64 `json:"toReceive"`
	Surplus      int64 `json:"surplus"`
	Savings      int64 `json:"savings"`
	Incoming     int64 `json:"incoming"`
	Outgoing     int64 `json:"outgoing"`
	Balance      int64 `json:"balance"`
}

type toPayBreakdownResponse struct {
	Fixed      int64 `json:"fixed"`
	Occasional int64 `json:"occasional"`
}

type accountResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	Balance   int64     `json:"balance"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type accountListResponse struct {
	Items        []accountResponse `json:"items"`
	CurrentTotal int64             `json:"currentTotal"`
	SavingsTotal int64             `json:"savingsTotal"`
}

type itemResponse struct {
	ID         string     `json:"id"`
	Period     string     `json:"period"`
	Kind       string     `json:"kind"`
	Name       string     `json:"name"`
	Amount     int64      `json:"amount"`
	DueDate    *string    `json:"dueDate"`
	TemplateID *string    `json:"templateId"`
	PersonID   *string    `json:"personId"`
	CardID     *string    `json:"cardId"`
	TagID      *string    `json:"tagId"`
	Status     string     `json:"status"`
	DoneAt     *time.Time `json:"doneAt"`
	Urgency    string     `json:"urgency"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
}

type kindTotalResponse struct {
	Total   int64 `json:"total"`
	Pending int64 `json:"pending"`
}

type itemTotalsResponse struct {
	Fixed      kindTotalResponse `json:"fixed"`
	Occasional kindTotalResponse `json:"occasional"`
	Income     kindTotalResponse `json:"income"`
	CardCharge kindTotalResponse `json:"cardCharge"`
	ToPay      int64             `json:"toPay"`
	ToReceive  int64             `json:"toReceive"`
}

type itemListResponse struct {
	Items  []itemResponse     `json:"items"`
	Totals itemTotalsResponse `json:"totals"`
}

type overdueListResponse struct {
	Items        []itemResponse `json:"items"`
	ExpenseTotal int64          `json:"expenseTotal"`
	IncomeTotal  int64          `json:"incomeTotal"`
}

type cardTotalResponse struct {
	CardID   *string `json:"cardId"`
	CardName *string `json:"cardName"`
	Total    int64   `json:"total"`
}

type cardTotalsResponse struct {
	Items []cardTotalResponse `json:"items"`
	Total int64               `json:"total"`
}

type memberResponse struct {
	PersonID string `json:"personId"`
	Name     string `json:"name"`
}

type platformResponse struct {
	ID             string           `json:"id"`
	Name           string           `json:"name"`
	MonthlyCost    int64            `json:"monthlyCost"`
	CardID         *string          `json:"cardId"`
	SharePerMember int64            `json:"sharePerMember"`
	Members        []memberResponse `json:"members"`
	CreatedAt      time.Time        `json:"createdAt"`
	UpdatedAt      time.Time        `json:"updatedAt"`
}

type platformListResponse struct {
	Items []platformResponse `json:"items"`
}

type personResponse struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	IsOwner         bool      `json:"isOwner"`
	BillingMonths   int       `json:"billingMonths"`
	MonthlyOwed     int64     `json:"monthlyOwed"`
	LastPaymentDate *string   `json:"lastPaymentDate"`
	CoveredUntil    *string   `json:"coveredUntil"`
	CoveredInPeriod *bool     `json:"coveredInPeriod,omitempty"`
	PeriodPaymentID *string   `json:"periodPaymentId,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type personListResponse struct {
	Items []personResponse `json:"items"`
}

type dashboardResponse struct {
	Period         periodResponse         `json:"period"`
	Kpis           kpisResponse           `json:"kpis"`
	ToPayBreakdown toPayBreakdownResponse `json:"toPayBreakdown"`
	Accounts       accountListResponse    `json:"accounts"`
	Items          itemListResponse       `json:"items"`
	UpcomingDue    []itemResponse         `json:"upcomingDue"`
	CardCharges    cardTotalsResponse     `json:"cardCharges"`
	Platforms      platformListResponse   `json:"platforms"`
	People         personListResponse     `json:"people"`
	Overdue        overdueListResponse    `json:"overdue"`
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	month, err := kernel.ParseYearMonth(r.URL.Query().Get("period"))
	if err != nil {
		platform.WriteProblem(w, http.StatusBadRequest, "The request is not valid.",
			platform.FieldError{Field: "period", Message: monthMessage})
		return
	}
	dashboard, err := h.Get.Execute(r.Context(), month)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, toResponse(dashboard))
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, domain.ErrPeriodNotFound) {
		platform.WriteProblem(w, http.StatusNotFound, "No such resource.")
		return
	}
	h.Logger.LogAttrs(r.Context(), slog.LevelError, "dashboard request failed",
		slog.String("error", err.Error()),
		slog.String("path", r.URL.Path),
		slog.String("request_id", platform.RequestIDFrom(r.Context())),
	)
	platform.WriteProblem(w, http.StatusInternalServerError, "Something went wrong.")
}

func toResponse(d application.Dashboard) dashboardResponse {
	return dashboardResponse{
		Period: periodResponse{
			ID:        d.Period.ID,
			Period:    d.Period.Month.String(),
			Status:    d.Period.Status,
			CreatedAt: d.Period.CreatedAt,
			UpdatedAt: d.Period.UpdatedAt,
		},
		Kpis: kpisResponse{
			CurrentTotal: int64(d.Kpis.CurrentTotal),
			ToPay:        int64(d.Kpis.ToPay),
			ToReceive:    int64(d.Kpis.ToReceive),
			Surplus:      int64(d.Kpis.Surplus),
			Savings:      int64(d.Kpis.Savings),
			Incoming:     int64(d.Kpis.Incoming),
			Outgoing:     int64(d.Kpis.Outgoing),
			Balance:      int64(d.Kpis.Balance),
		},
		ToPayBreakdown: toPayBreakdownResponse{
			Fixed:      int64(d.ToPayBreakdown.Fixed),
			Occasional: int64(d.ToPayBreakdown.Occasional),
		},
		Accounts:    toAccounts(d.Accounts),
		Items:       itemListResponse{Items: toItems(d.Items.Items), Totals: toItemTotals(d.Items.Totals)},
		UpcomingDue: toItems(d.UpcomingDue),
		CardCharges: toCardCharges(d.CardCharges),
		Platforms:   toPlatforms(d.Platforms),
		People:      toPeople(d.People),
		Overdue: overdueListResponse{
			Items:        toItems(d.Overdue.Items),
			ExpenseTotal: int64(d.Overdue.ExpenseTotal),
			IncomeTotal:  int64(d.Overdue.IncomeTotal),
		},
	}
}

func toAccounts(accounts domain.Accounts) accountListResponse {
	items := make([]accountResponse, 0, len(accounts.Items))
	for _, account := range accounts.Items {
		items = append(items, accountResponse{
			ID:        account.ID,
			Name:      account.Name,
			Kind:      account.Kind,
			Balance:   int64(account.Balance),
			CreatedAt: account.CreatedAt,
			UpdatedAt: account.UpdatedAt,
		})
	}
	return accountListResponse{
		Items:        items,
		CurrentTotal: int64(accounts.CurrentTotal),
		SavingsTotal: int64(accounts.SavingsTotal),
	}
}

func toItems(items []domain.Item) []itemResponse {
	responses := make([]itemResponse, 0, len(items))
	for _, item := range items {
		responses = append(responses, itemResponse{
			ID:         item.ID,
			Period:     item.Month.String(),
			Kind:       item.Kind,
			Name:       item.Name,
			Amount:     int64(item.Amount),
			DueDate:    formatDate(item.DueDate),
			TemplateID: item.TemplateID,
			PersonID:   item.PersonID,
			CardID:     item.CardID,
			TagID:      item.TagID,
			Status:     item.Status,
			DoneAt:     item.DoneAt,
			Urgency:    item.Urgency,
			CreatedAt:  item.CreatedAt,
			UpdatedAt:  item.UpdatedAt,
		})
	}
	return responses
}

func toItemTotals(totals domain.ItemTotals) itemTotalsResponse {
	return itemTotalsResponse{
		Fixed:      toKindTotal(totals.Fixed),
		Occasional: toKindTotal(totals.Occasional),
		Income:     toKindTotal(totals.Income),
		CardCharge: toKindTotal(totals.CardCharge),
		ToPay:      int64(totals.ToPay),
		ToReceive:  int64(totals.ToReceive),
	}
}

func toKindTotal(total domain.KindTotal) kindTotalResponse {
	return kindTotalResponse{Total: int64(total.Total), Pending: int64(total.Pending)}
}

func toCardCharges(charges domain.CardCharges) cardTotalsResponse {
	items := make([]cardTotalResponse, 0, len(charges.Items))
	for _, item := range charges.Items {
		items = append(items, cardTotalResponse{CardID: item.CardID, CardName: item.CardName, Total: int64(item.Total)})
	}
	return cardTotalsResponse{Items: items, Total: int64(charges.Total)}
}

func toPlatforms(platforms []domain.Platform) platformListResponse {
	items := make([]platformResponse, 0, len(platforms))
	for _, p := range platforms {
		members := make([]memberResponse, 0, len(p.Members))
		for _, member := range p.Members {
			members = append(members, memberResponse(member))
		}
		items = append(items, platformResponse{
			ID:             p.ID,
			Name:           p.Name,
			MonthlyCost:    int64(p.MonthlyCost),
			CardID:         p.CardID,
			SharePerMember: int64(p.SharePerMember),
			Members:        members,
			CreatedAt:      p.CreatedAt,
			UpdatedAt:      p.UpdatedAt,
		})
	}
	return platformListResponse{Items: items}
}

func toPeople(people []domain.Person) personListResponse {
	items := make([]personResponse, 0, len(people))
	for _, person := range people {
		items = append(items, personResponse{
			ID:              person.ID,
			Name:            person.Name,
			IsOwner:         person.IsOwner,
			BillingMonths:   person.BillingMonths,
			MonthlyOwed:     int64(person.MonthlyOwed),
			LastPaymentDate: formatDate(person.LastPaymentDate),
			CoveredUntil:    formatDate(person.CoveredUntil),
			CoveredInPeriod: person.CoveredInPeriod,
			PeriodPaymentID: person.PeriodPaymentID,
			CreatedAt:       person.CreatedAt,
			UpdatedAt:       person.UpdatedAt,
		})
	}
	return personListResponse{Items: items}
}

func formatDate(date *time.Time) *string {
	if date == nil {
		return nil
	}
	text := date.Format(dateLayout)
	return &text
}
