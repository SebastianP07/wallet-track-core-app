// Package http is the inbound HTTP adapter of the credit cards module.
package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/SebastianP07/wallet-track-core-app/internal/cards/application"
	"github.com/SebastianP07/wallet-track-core-app/internal/cards/domain"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/platform"
)

const (
	basePath     = "/api/v1"
	monthMessage = "must be a month as YYYY-MM, from 2000-01 on"
)

var (
	uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	// Plain JSON integers only: 1.0, 1e1 and "1" all name a whole number but are not one on the wire.
	integerPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)
)

// UseCases are the operations this adapter exposes.
type UseCases struct {
	List   *application.ListCards
	Get    *application.GetCard
	Create *application.CreateCard
	Update *application.UpdateCard
	Delete *application.DeleteCard
	Totals *application.GetCardTotals
}

type Config struct {
	UseCases UseCases
	// RequireSession and RequireCSRF come from cmd/, so this module imports no other module's adapters.
	RequireSession platform.Middleware
	RequireCSRF    platform.Middleware
	Logger         *slog.Logger
}

type Handler struct {
	Config
}

func NewHandler(cfg Config) *Handler {
	return &Handler{Config: cfg}
}

// Register adds the credit card routes of the contract and a month's per-card totals.
func (h *Handler) Register(mux *http.ServeMux) {
	read := func(pattern string, handler http.HandlerFunc) {
		mux.Handle("GET "+basePath+pattern, platform.Chain(handler, h.RequireSession))
	}
	write := func(method, pattern string, handler http.HandlerFunc) {
		mux.Handle(method+" "+basePath+pattern, platform.Chain(handler, h.RequireCSRF, h.RequireSession))
	}

	read("/credit-cards", h.list)
	write(http.MethodPost, "/credit-cards", h.create)
	read("/credit-cards/{id}", h.get)
	write(http.MethodPatch, "/credit-cards/{id}", h.update)
	write(http.MethodDelete, "/credit-cards/{id}", h.delete)
	read("/periods/{period}/card-totals", h.totals)
}

type cardResponse struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Issuer       string    `json:"issuer"`
	StatementDay int       `json:"statementDay"`
	DueDay       int       `json:"dueDay"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type cardListResponse struct {
	Items []cardResponse `json:"items"`
}

type cardTotalResponse struct {
	CardID   *string `json:"cardId"`
	CardName *string `json:"cardName"`
	Total    int64   `json:"total"`
}

type monthTotalsResponse struct {
	Items []cardTotalResponse `json:"items"`
	Total int64               `json:"total"`
}

// cardBody keeps each field raw, so that a wrong type or a null is reported on its own field.
type cardBody struct {
	Name         json.RawMessage `json:"name"`
	Issuer       json.RawMessage `json:"issuer"`
	StatementDay json.RawMessage `json:"statementDay"`
	DueDay       json.RawMessage `json:"dueDay"`
}

func toResponse(card domain.Card) cardResponse {
	return cardResponse{
		ID:           card.ID,
		Name:         card.Name,
		Issuer:       card.Issuer,
		StatementDay: card.StatementDay,
		DueDay:       card.DueDay,
		CreatedAt:    card.CreatedAt,
		UpdatedAt:    card.UpdatedAt,
	}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	cards, err := h.UseCases.List.Execute(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	items := make([]cardResponse, 0, len(cards))
	for _, card := range cards {
		items = append(items, toResponse(card))
	}
	platform.WriteJSON(w, http.StatusOK, cardListResponse{Items: items})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	card, err := h.UseCases.Get.Execute(r.Context(), id)
	h.writeCard(w, r, http.StatusOK, card, err)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var body cardBody
	if err := platform.DecodeJSON(w, r, &body); err != nil {
		writeBadRequest(w)
		return
	}
	var fields []platform.FieldError
	for _, required := range []struct {
		field string
		raw   json.RawMessage
	}{
		{"name", body.Name},
		{"issuer", body.Issuer},
		{"statementDay", body.StatementDay},
		{"dueDay", body.DueDay},
	} {
		if required.raw == nil {
			fields = append(fields, platform.FieldError{Field: required.field, Message: "is required"})
		}
	}
	input := cardInput(body, &fields)
	if len(fields) > 0 {
		writeBadRequest(w, fields...)
		return
	}
	card, err := domain.CreateNewCard(input)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	created, err := h.UseCases.Create.Execute(r.Context(), card)
	h.writeCard(w, r, http.StatusCreated, created, err)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body cardBody
	if err := platform.DecodeJSON(w, r, &body); err != nil {
		writeBadRequest(w)
		return
	}
	if body.Name == nil && body.Issuer == nil && body.StatementDay == nil && body.DueDay == nil {
		platform.WriteProblem(w, http.StatusBadRequest, "Send at least one of name, issuer, statementDay or dueDay.")
		return
	}
	var fields []platform.FieldError
	input := cardInput(body, &fields)
	if len(fields) > 0 {
		writeBadRequest(w, fields...)
		return
	}
	changes, err := domain.NewChanges(input)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	updated, err := h.UseCases.Update.Execute(r.Context(), id, changes)
	h.writeCard(w, r, http.StatusOK, updated, err)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.UseCases.Delete.Execute(r.Context(), id); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) totals(w http.ResponseWriter, r *http.Request) {
	month, err := kernel.ParseYearMonth(r.PathValue("period"))
	if err != nil {
		writeBadRequest(w, platform.FieldError{Field: "period", Message: monthMessage})
		return
	}
	totals, err := h.UseCases.Totals.Execute(r.Context(), month)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	items := make([]cardTotalResponse, 0, len(totals.Items))
	for _, item := range totals.Items {
		items = append(items, cardTotalResponse{CardID: item.CardID, CardName: item.CardName, Total: int64(item.Total)})
	}
	platform.WriteJSON(w, http.StatusOK, monthTotalsResponse{Items: items, Total: int64(totals.Total)})
}

func (h *Handler) writeCard(w http.ResponseWriter, r *http.Request, status int, card domain.Card, err error) {
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	platform.WriteJSON(w, status, toResponse(card))
}

func cardInput(body cardBody, fields *[]platform.FieldError) domain.CardInput {
	return domain.CardInput{
		Name:         optionalString(body.Name, "name", fields),
		Issuer:       optionalString(body.Issuer, "issuer", fields),
		StatementDay: optionalDay(body.StatementDay, "statementDay", fields),
		DueDay:       optionalDay(body.DueDay, "dueDay", fields),
	}
}

// pathID answers the 400 itself and reports whether the handler may go on.
func pathID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if !uuidPattern.MatchString(id) {
		writeBadRequest(w, platform.FieldError{Field: "id", Message: "must be a UUID"})
		return "", false
	}
	return strings.ToLower(id), true
}

func optionalString(raw json.RawMessage, field string, fields *[]platform.FieldError) *string {
	if raw == nil {
		return nil
	}
	var value *string
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		*fields = append(*fields, platform.FieldError{Field: field, Message: "must be a string"})
		return nil
	}
	return value
}

func optionalDay(raw json.RawMessage, field string, fields *[]platform.FieldError) *int {
	if raw == nil {
		return nil
	}
	text := string(raw)
	value, err := strconv.Atoi(text)
	if !integerPattern.MatchString(text) || err != nil {
		*fields = append(*fields, platform.FieldError{Field: field, Message: domain.DayOutOfRangeMessage})
		return nil
	}
	return &value
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var invalid *domain.ValidationError
	switch {
	case errors.As(err, &invalid):
		fields := make([]platform.FieldError, 0, len(invalid.Violations))
		for _, violation := range invalid.Violations {
			fields = append(fields, platform.FieldError{Field: violation.Field, Message: violation.Message})
		}
		writeBadRequest(w, fields...)
	case errors.Is(err, domain.ErrNotFound), errors.Is(err, domain.ErrPeriodNotFound):
		platform.WriteProblem(w, http.StatusNotFound, "No such resource.")
	default:
		h.Logger.LogAttrs(r.Context(), slog.LevelError, "credit cards request failed",
			slog.String("error", err.Error()),
			slog.String("path", r.URL.Path),
			slog.String("request_id", platform.RequestIDFrom(r.Context())),
		)
		platform.WriteProblem(w, http.StatusInternalServerError, "Something went wrong.")
	}
}

func writeBadRequest(w http.ResponseWriter, fields ...platform.FieldError) {
	platform.WriteProblem(w, http.StatusBadRequest, "The request is not valid.", fields...)
}
