// Package http is the inbound HTTP adapter of the expenses module: a month's items of every kind and
// their done toggle.
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

	"github.com/SebastianP07/wallet-track-core-app/internal/expenses/application"
	"github.com/SebastianP07/wallet-track-core-app/internal/expenses/domain"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/platform"
)

const (
	basePath     = "/api/v1"
	dateLayout   = "2006-01-02"
	monthMessage = "must be a month as YYYY-MM, from 2000-01 on"
	// The contract's bound on one reorder, far beyond any month's items.
	maxOrderIDs = 1000
)

var (
	uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	// Plain JSON integers only: 1.0, 1e3 and "1" all name a whole number but are not one on the wire.
	integerPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)
)

// UseCases are the operations this adapter exposes.
type UseCases struct {
	List    *application.ListItems
	Get     *application.GetItem
	Create  *application.CreateItem
	Update  *application.UpdateItem
	Delete  *application.DeleteItem
	Done    *application.MarkItemDone
	Undone  *application.UnmarkItemDone
	Overdue *application.ListOverdueItems
	Reorder *application.ReorderItems
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

// Register adds the period item routes of the contract.
func (h *Handler) Register(mux *http.ServeMux) {
	read := func(pattern string, handler http.HandlerFunc) {
		mux.Handle("GET "+basePath+pattern, platform.Chain(handler, h.RequireSession))
	}
	write := func(method, pattern string, handler http.HandlerFunc) {
		mux.Handle(method+" "+basePath+pattern, platform.Chain(handler, h.RequireCSRF, h.RequireSession))
	}

	read("/periods/{period}/items", h.listItems)
	write(http.MethodPost, "/periods/{period}/items", h.createItem)
	write(http.MethodPut, "/periods/{period}/items/order", h.reorderItems)
	read("/period-items/{id}", h.getItem)
	write(http.MethodPatch, "/period-items/{id}", h.updateItem)
	write(http.MethodDelete, "/period-items/{id}", h.deleteItem)
	write(http.MethodPut, "/period-items/{id}/done", h.markDone)
	write(http.MethodDelete, "/period-items/{id}/done", h.unmarkDone)
	read("/periods/{period}/overdue-items", h.listOverdueItems)
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
	OwedIn     *string    `json:"owedIn"`
	Urgency    string     `json:"urgency"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
}

type kindTotalResponse struct {
	Total   int64 `json:"total"`
	Pending int64 `json:"pending"`
}

type totalsResponse struct {
	Fixed      kindTotalResponse `json:"fixed"`
	Occasional kindTotalResponse `json:"occasional"`
	Income     kindTotalResponse `json:"income"`
	CardCharge kindTotalResponse `json:"cardCharge"`
	Overdue    kindTotalResponse `json:"overdue"`
	ToPay      int64             `json:"toPay"`
	ToReceive  int64             `json:"toReceive"`
}

type itemListResponse struct {
	Items  []itemResponse `json:"items"`
	Totals totalsResponse `json:"totals"`
}

type overdueListResponse struct {
	Items        []itemResponse `json:"items"`
	ExpenseTotal int64          `json:"expenseTotal"`
	IncomeTotal  int64          `json:"incomeTotal"`
}

// itemBody keeps each field raw, so that a wrong type or a null is reported on its own field and an
// absent field can be told apart from a null one. Status is only here to be refused by name.
type itemBody struct {
	Kind    json.RawMessage `json:"kind"`
	Name    json.RawMessage `json:"name"`
	Amount  json.RawMessage `json:"amount"`
	DueDate json.RawMessage `json:"dueDate"`
	CardID  json.RawMessage `json:"cardId"`
	TagID   json.RawMessage `json:"tagId"`
	Status  json.RawMessage `json:"status"`
	// RepeatsMonthly is only for a create; an edit refuses it by name.
	RepeatsMonthly json.RawMessage `json:"repeatsMonthly"`
}

func toItemResponse(view application.ItemView) itemResponse {
	response := itemResponse{
		ID:         view.ID,
		Period:     view.Month.String(),
		Kind:       string(view.Kind),
		Name:       view.Name,
		Amount:     int64(view.Amount),
		TemplateID: view.TemplateID,
		PersonID:   view.PersonID,
		CardID:     view.CardID,
		TagID:      view.TagID,
		Status:     string(view.Status),
		DoneAt:     view.DoneAt,
		Urgency:    string(view.Urgency),
		CreatedAt:  view.CreatedAt,
		UpdatedAt:  view.UpdatedAt,
	}
	if view.DueDate != nil {
		due := view.DueDate.Format(dateLayout)
		response.DueDate = &due
	}
	if view.OwedIn != nil {
		owedIn := view.OwedIn.String()
		response.OwedIn = &owedIn
	}
	return response
}

func toKindTotal(total domain.KindTotal) kindTotalResponse {
	return kindTotalResponse{Total: int64(total.Total), Pending: int64(total.Pending)}
}

func (h *Handler) listItems(w http.ResponseWriter, r *http.Request) {
	month, ok := pathMonth(w, r)
	if !ok {
		return
	}
	filter, ok := itemFilter(w, r)
	if !ok {
		return
	}
	list, err := h.UseCases.List.Execute(r.Context(), month, filter)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	items := make([]itemResponse, 0, len(list.Items))
	for _, item := range list.Items {
		items = append(items, toItemResponse(item))
	}
	platform.WriteJSON(w, http.StatusOK, itemListResponse{
		Items: items,
		Totals: totalsResponse{
			Fixed:      toKindTotal(list.Totals.Fixed),
			Occasional: toKindTotal(list.Totals.Occasional),
			Income:     toKindTotal(list.Totals.Income),
			CardCharge: toKindTotal(list.Totals.CardCharge),
			Overdue:    toKindTotal(list.Totals.Overdue),
			ToPay:      int64(list.Totals.ToPay),
			ToReceive:  int64(list.Totals.ToReceive),
		},
	})
}

func (h *Handler) listOverdueItems(w http.ResponseWriter, r *http.Request) {
	month, ok := pathMonth(w, r)
	if !ok {
		return
	}
	list, err := h.UseCases.Overdue.Execute(r.Context(), month)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	items := make([]itemResponse, 0, len(list.Items))
	for _, item := range list.Items {
		items = append(items, toItemResponse(item))
	}
	platform.WriteJSON(w, http.StatusOK, overdueListResponse{
		Items:        items,
		ExpenseTotal: int64(list.Totals.Expense),
		IncomeTotal:  int64(list.Totals.Income),
	})
}

func itemFilter(w http.ResponseWriter, r *http.Request) (domain.ItemFilter, bool) {
	var (
		filter domain.ItemFilter
		fields []platform.FieldError
		query  = r.URL.Query()
	)
	if query.Has("kind") {
		kind, known := domain.ParseItemKind(query.Get("kind"))
		if known {
			filter.Kind = &kind
		} else {
			fields = append(fields, platform.FieldError{Field: "kind", Message: "must be one of fixed, occasional, income, card_charge"})
		}
	}
	if query.Has("status") {
		status, known := domain.ParseItemStatus(query.Get("status"))
		if known {
			filter.Status = &status
		} else {
			fields = append(fields, platform.FieldError{Field: "status", Message: "must be one of pending, done, overdue"})
		}
	}
	if query.Has("tagId") {
		tagID := query.Get("tagId")
		if uuidPattern.MatchString(tagID) {
			lowered := strings.ToLower(tagID)
			filter.TagID = &lowered
		} else {
			fields = append(fields, platform.FieldError{Field: "tagId", Message: "must be a UUID"})
		}
	}
	if len(fields) > 0 {
		writeBadRequest(w, fields...)
		return domain.ItemFilter{}, false
	}
	return filter, true
}

// orderBody keeps each field raw, so that a wrong type is reported on its own field.
type orderBody struct {
	Kind json.RawMessage `json:"kind"`
	IDs  json.RawMessage `json:"ids"`
}

func (h *Handler) reorderItems(w http.ResponseWriter, r *http.Request) {
	month, ok := pathMonth(w, r)
	if !ok {
		return
	}
	var body orderBody
	if err := platform.DecodeJSON(w, r, &body); err != nil {
		writeBadRequest(w)
		return
	}
	var fields []platform.FieldError
	rawKind := requiredString(body.Kind, "kind", &fields)
	ids := idList(body.IDs, "ids", &fields)
	if len(fields) > 0 {
		writeBadRequest(w, fields...)
		return
	}
	kind, err := domain.ParseOrderKind(rawKind)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if err := h.UseCases.Reorder.Execute(r.Context(), month, kind, ids); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// idList is a required array of UUIDs, lowered so that the same id always reads the same.
func idList(raw json.RawMessage, field string, fields *[]platform.FieldError) []string {
	var values []string
	if raw == nil || json.Unmarshal(raw, &values) != nil || values == nil || len(values) > maxOrderIDs {
		*fields = append(*fields, platform.FieldError{Field: field, Message: "must be an array of at most 1000 UUIDs"})
		return nil
	}
	ids := make([]string, 0, len(values))
	for _, value := range values {
		if !uuidPattern.MatchString(value) {
			*fields = append(*fields, platform.FieldError{Field: field, Message: "must be an array of at most 1000 UUIDs"})
			return nil
		}
		ids = append(ids, strings.ToLower(value))
	}
	return ids
}

func (h *Handler) getItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	item, err := h.UseCases.Get.Execute(r.Context(), id)
	h.writeItem(w, r, http.StatusOK, item, err)
}

func (h *Handler) createItem(w http.ResponseWriter, r *http.Request) {
	month, ok := pathMonth(w, r)
	if !ok {
		return
	}
	var body itemBody
	if err := platform.DecodeJSON(w, r, &body); err != nil {
		writeBadRequest(w)
		return
	}
	var fields []platform.FieldError
	input := domain.NewItemInput{
		Kind:           requiredString(body.Kind, "kind", &fields),
		Name:           requiredString(body.Name, "name", &fields),
		DueDate:        presentDate(body.DueDate),
		CardID:         nullableID(body.CardID, "cardId", &fields),
		TagID:          nullableID(body.TagID, "tagId", &fields).Value,
		RepeatsMonthly: optionalBool(body.RepeatsMonthly, "repeatsMonthly", &fields),
	}
	if body.Amount == nil {
		fields = append(fields, platform.FieldError{Field: "amount", Message: "is required"})
	} else if amount := optionalAmount(body.Amount, &fields); amount != nil {
		input.Amount = *amount
	}
	if body.Status != nil {
		fields = append(fields, platform.FieldError{Field: "status", Message: "cannot be set"})
	}
	if len(fields) > 0 {
		writeBadRequest(w, fields...)
		return
	}
	item, err := domain.CreateNewItem(input)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	created, err := h.UseCases.Create.Execute(r.Context(), month, item)
	h.writeItem(w, r, http.StatusCreated, created, err)
}

func (h *Handler) updateItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body itemBody
	if err := platform.DecodeJSON(w, r, &body); err != nil {
		writeBadRequest(w)
		return
	}
	var fields []platform.FieldError
	if body.Kind != nil {
		fields = append(fields, platform.FieldError{Field: "kind", Message: "cannot be changed"})
	}
	if body.Status != nil {
		fields = append(fields, platform.FieldError{Field: "status", Message: "cannot be changed here; use the done endpoints"})
	}
	if body.RepeatsMonthly != nil {
		fields = append(fields, platform.FieldError{Field: "repeatsMonthly", Message: "cannot be changed here; edit or stop the template"})
	}
	if len(fields) > 0 {
		writeBadRequest(w, fields...)
		return
	}
	if body.Name == nil && body.Amount == nil && body.DueDate == nil && body.CardID == nil && body.TagID == nil {
		platform.WriteProblem(w, http.StatusBadRequest, "Send at least one of name, amount, dueDate, cardId or tagId.")
		return
	}
	input := domain.ItemChangesInput{
		Name:    optionalString(body.Name, "name", &fields),
		Amount:  optionalAmount(body.Amount, &fields),
		DueDate: presentDate(body.DueDate),
		CardID:  nullableID(body.CardID, "cardId", &fields),
		TagID:   nullableID(body.TagID, "tagId", &fields),
	}
	if len(fields) > 0 {
		writeBadRequest(w, fields...)
		return
	}
	changes, err := domain.NewItemChanges(input)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	updated, err := h.UseCases.Update.Execute(r.Context(), id, changes)
	h.writeItem(w, r, http.StatusOK, updated, err)
}

func (h *Handler) deleteItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	stopRepeating := false
	if query := r.URL.Query(); query.Has("stopRepeating") {
		switch query.Get("stopRepeating") {
		case "true":
			stopRepeating = true
		case "false":
		default:
			writeBadRequest(w, platform.FieldError{Field: "stopRepeating", Message: "must be true or false"})
			return
		}
	}
	if err := h.UseCases.Delete.Execute(r.Context(), id, stopRepeating); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) markDone(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	item, err := h.UseCases.Done.Execute(r.Context(), id)
	h.writeItem(w, r, http.StatusOK, item, err)
}

func (h *Handler) unmarkDone(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	item, err := h.UseCases.Undone.Execute(r.Context(), id)
	h.writeItem(w, r, http.StatusOK, item, err)
}

func (h *Handler) writeItem(w http.ResponseWriter, r *http.Request, status int, item application.ItemView, err error) {
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	platform.WriteJSON(w, status, toItemResponse(item))
}

func pathMonth(w http.ResponseWriter, r *http.Request) (kernel.YearMonth, bool) {
	month, err := kernel.ParseYearMonth(r.PathValue("period"))
	if err != nil {
		writeBadRequest(w, platform.FieldError{Field: "period", Message: monthMessage})
		return kernel.YearMonth{}, false
	}
	return month, true
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

// optionalBool is false when absent.
func optionalBool(raw json.RawMessage, field string, fields *[]platform.FieldError) bool {
	if raw == nil {
		return false
	}
	var value *bool
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		*fields = append(*fields, platform.FieldError{Field: field, Message: "must be a boolean"})
		return false
	}
	return *value
}

// presentDate leaves every rule to the domain: only whether the field was sent, and as what text. A
// null or a number comes through as empty, which is no date either.
func presentDate(raw json.RawMessage) *string {
	if raw == nil {
		return nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		value = ""
	}
	return &value
}

func optionalAmount(raw json.RawMessage, fields *[]platform.FieldError) *int64 {
	if raw == nil {
		return nil
	}
	text := string(raw)
	value, err := strconv.ParseInt(text, 10, 64)
	if !integerPattern.MatchString(text) || err != nil {
		*fields = append(*fields, platform.FieldError{Field: "amount", Message: domain.AmountOutOfRangeMessage})
		return nil
	}
	return &value
}

func nullableID(raw json.RawMessage, field string, fields *[]platform.FieldError) domain.OptionalID {
	if raw == nil {
		return domain.OptionalID{}
	}
	var value *string
	if err := json.Unmarshal(raw, &value); err != nil || (value != nil && !uuidPattern.MatchString(*value)) {
		*fields = append(*fields, platform.FieldError{Field: field, Message: "must be a UUID or null"})
		return domain.OptionalID{}
	}
	if value == nil {
		return domain.OptionalID{Set: true}
	}
	id := strings.ToLower(*value)
	return domain.OptionalID{Set: true, Value: &id}
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
	case errors.Is(err, domain.ErrPeriodNotFound), errors.Is(err, domain.ErrItemNotFound):
		platform.WriteProblem(w, http.StatusNotFound, "No such resource.")
	case errors.Is(err, domain.ErrMonthClosed):
		platform.WriteProblem(w, http.StatusConflict, "This month is closed, so it takes no new items.")
	case errors.Is(err, kernel.ErrMoneyOutOfRange):
		// From the member's payment an item's toggle records: what is owed, times the months it covers.
		writeBadRequest(w, platform.FieldError{Field: "amount", Message: "the member's payment would pass 9007199254740991"})
	default:
		h.Logger.LogAttrs(r.Context(), slog.LevelError, "expenses request failed",
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
