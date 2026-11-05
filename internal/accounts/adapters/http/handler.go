// Package http is the inbound HTTP adapter of the accounts module.
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

	"github.com/SebastianP07/wallet-track-core-app/internal/accounts/application"
	"github.com/SebastianP07/wallet-track-core-app/internal/accounts/domain"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/platform"
)

const basePath = "/api/v1"

var (
	uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	// Plain JSON integers only: 1.0, 1e3 and "1" all name a whole number but are not one on the wire.
	integerPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)
)

// UseCases are the operations this adapter exposes.
type UseCases struct {
	List   *application.ListAccounts
	Get    *application.GetAccount
	Create *application.CreateAccount
	Update *application.UpdateAccount
	Delete *application.DeleteAccount
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

// Register adds the accounts routes of the contract.
func (h *Handler) Register(mux *http.ServeMux) {
	read := func(pattern string, handler http.HandlerFunc) {
		mux.Handle("GET "+basePath+pattern, platform.Chain(handler, h.RequireSession))
	}
	write := func(method, pattern string, handler http.HandlerFunc) {
		mux.Handle(method+" "+basePath+pattern, platform.Chain(handler, h.RequireCSRF, h.RequireSession))
	}

	read("/accounts", h.list)
	write(http.MethodPost, "/accounts", h.create)
	read("/accounts/{id}", h.get)
	write(http.MethodPatch, "/accounts/{id}", h.update)
	write(http.MethodDelete, "/accounts/{id}", h.delete)
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

// accountBody keeps each field raw, so that a wrong type or a null is reported on its own field
// and an absent field can be told apart from a null one.
type accountBody struct {
	Name    json.RawMessage `json:"name"`
	Kind    json.RawMessage `json:"kind"`
	Balance json.RawMessage `json:"balance"`
}

func toResponse(account domain.Account) accountResponse {
	return accountResponse{
		ID:        account.ID,
		Name:      account.Name,
		Kind:      string(account.Kind),
		Balance:   int64(account.Balance),
		CreatedAt: account.CreatedAt,
		UpdatedAt: account.UpdatedAt,
	}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	list, err := h.UseCases.List.Execute(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	items := make([]accountResponse, 0, len(list.Items))
	for _, account := range list.Items {
		items = append(items, toResponse(account))
	}
	platform.WriteJSON(w, http.StatusOK, accountListResponse{
		Items:        items,
		CurrentTotal: int64(list.Totals.Current),
		SavingsTotal: int64(list.Totals.Savings),
	})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	account, err := h.UseCases.Get.Execute(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, toResponse(account))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var body accountBody
	if err := platform.DecodeJSON(w, r, &body); err != nil {
		writeBadRequest(w)
		return
	}
	var fields []platform.FieldError
	name := requiredString(body.Name, "name", &fields)
	kind := requiredString(body.Kind, "kind", &fields)
	var balance int64
	if value := optionalBalance(body.Balance, &fields); value != nil {
		balance = *value
	}
	if len(fields) > 0 {
		writeBadRequest(w, fields...)
		return
	}
	account, err := domain.CreateNewAccount(name, kind, balance)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	created, err := h.UseCases.Create.Execute(r.Context(), account)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	platform.WriteJSON(w, http.StatusCreated, toResponse(created))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body accountBody
	if err := platform.DecodeJSON(w, r, &body); err != nil {
		writeBadRequest(w)
		return
	}
	if body.Name == nil && body.Kind == nil && body.Balance == nil {
		platform.WriteProblem(w, http.StatusBadRequest, "Send at least one of name, kind or balance.")
		return
	}
	var fields []platform.FieldError
	name := optionalString(body.Name, "name", &fields)
	kind := optionalString(body.Kind, "kind", &fields)
	balance := optionalBalance(body.Balance, &fields)
	if len(fields) > 0 {
		writeBadRequest(w, fields...)
		return
	}
	changes, err := domain.NewChanges(name, kind, balance)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	updated, err := h.UseCases.Update.Execute(r.Context(), id, changes)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, toResponse(updated))
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

// pathID answers the 400 itself and reports whether the handler may go on. Lowercased because the
// use cases compare it against ids as the database prints them.
func pathID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if !uuidPattern.MatchString(id) {
		writeBadRequest(w, platform.FieldError{Field: "id", Message: "must be a UUID"})
		return "", false
	}
	return strings.ToLower(id), true
}

func requiredString(raw json.RawMessage, field string, fields *[]platform.FieldError) string {
	if raw == nil {
		*fields = append(*fields, platform.FieldError{Field: field, Message: "is required"})
		return ""
	}
	value := optionalString(raw, field, fields)
	if value == nil {
		return ""
	}
	return *value
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

func optionalBalance(raw json.RawMessage, fields *[]platform.FieldError) *int64 {
	if raw == nil {
		return nil
	}
	text := string(raw)
	value, err := strconv.ParseInt(text, 10, 64)
	if !integerPattern.MatchString(text) || err != nil {
		*fields = append(*fields, platform.FieldError{Field: "balance", Message: domain.BalanceOutOfRangeMessage})
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
	case errors.Is(err, domain.ErrNotFound):
		platform.WriteProblem(w, http.StatusNotFound, "No such resource.")
	case errors.Is(err, domain.ErrTotalOutOfRange) && r.Method != http.MethodGet:
		platform.WriteProblem(w, http.StatusBadRequest, "This change would take a total beyond the largest amount the app can carry.")
	default:
		h.Logger.LogAttrs(r.Context(), slog.LevelError, "accounts request failed",
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
