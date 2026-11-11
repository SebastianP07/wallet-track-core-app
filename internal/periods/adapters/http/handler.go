// Package http is the inbound HTTP adapter of the periods module: periods and recurring templates.
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

	"github.com/SebastianP07/wallet-track-core-app/internal/periods/application"
	"github.com/SebastianP07/wallet-track-core-app/internal/periods/domain"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/platform"
)

const (
	basePath     = "/api/v1"
	monthMessage = "must be a month as YYYY-MM, from 2000-01 on"
)

var (
	uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	// Plain JSON integers only: 1.0, 1e3 and "1" all name a whole number but are not one on the wire.
	integerPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)
)

// UseCases are the operations this adapter exposes.
type UseCases struct {
	ListPeriods *application.ListPeriods
	GetPeriod   *application.GetPeriod
	OpenPeriod  *application.OpenPeriod
	ClosePeriod *application.ClosePeriod
	// SetOpeningBalance answers PATCH /periods/{period}.
	SetOpeningBalance *application.SetOpeningBalance
	ListTemplates     *application.ListTemplates
	GetTemplate       *application.GetTemplate
	CreateTemplate    *application.CreateTemplate
	UpdateTemplate    *application.UpdateTemplate
	DeleteTemplate    *application.DeleteTemplate
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

// Register adds the periods and recurring templates routes of the contract.
func (h *Handler) Register(mux *http.ServeMux) {
	read := func(pattern string, handler http.HandlerFunc) {
		mux.Handle("GET "+basePath+pattern, platform.Chain(handler, h.RequireSession))
	}
	write := func(method, pattern string, handler http.HandlerFunc) {
		mux.Handle(method+" "+basePath+pattern, platform.Chain(handler, h.RequireCSRF, h.RequireSession))
	}

	read("/periods", h.listPeriods)
	write(http.MethodPost, "/periods", h.openPeriod)
	read("/periods/{period}", h.getPeriod)
	write(http.MethodPatch, "/periods/{period}", h.updatePeriod)
	write(http.MethodPost, "/periods/{period}/close", h.closePeriod)

	read("/recurring-templates", h.listTemplates)
	write(http.MethodPost, "/recurring-templates", h.createTemplate)
	read("/recurring-templates/{id}", h.getTemplate)
	write(http.MethodPatch, "/recurring-templates/{id}", h.updateTemplate)
	write(http.MethodDelete, "/recurring-templates/{id}", h.deleteTemplate)
}

type periodResponse struct {
	ID             string    `json:"id"`
	Period         string    `json:"period"`
	Status         string    `json:"status"`
	OpeningBalance *int64    `json:"openingBalance"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type periodListResponse struct {
	Items []periodResponse `json:"items"`
}

type templateResponse struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	Amount    int64     `json:"amount"`
	DueDay    *int      `json:"dueDay"`
	CardID    *string   `json:"cardId"`
	TagID     *string   `json:"tagId"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type templateListResponse struct {
	Items []templateResponse `json:"items"`
}

type openPeriodBody struct {
	Period json.RawMessage `json:"period"`
}

// updatePeriodBody keeps the field raw, so an absent openingBalance can be told apart from a null one.
type updatePeriodBody struct {
	OpeningBalance json.RawMessage `json:"openingBalance"`
}

// templateBody keeps each field raw, so that a wrong type or a null is reported on its own field
// and an absent field can be told apart from a null one.
type templateBody struct {
	Kind   json.RawMessage `json:"kind"`
	Name   json.RawMessage `json:"name"`
	Amount json.RawMessage `json:"amount"`
	DueDay json.RawMessage `json:"dueDay"`
	CardID json.RawMessage `json:"cardId"`
	TagID  json.RawMessage `json:"tagId"`
	Active json.RawMessage `json:"active"`
}

func toPeriodResponse(period domain.Period) periodResponse {
	return periodResponse{
		ID:             period.ID,
		Period:         period.Month.String(),
		Status:         string(period.Status),
		OpeningBalance: toOptionalAmount(period.OpeningBalance),
		CreatedAt:      period.CreatedAt,
		UpdatedAt:      period.UpdatedAt,
	}
}

func toOptionalAmount(amount *kernel.Money) *int64 {
	if amount == nil {
		return nil
	}
	value := int64(*amount)
	return &value
}

func toTemplateResponse(template domain.Template) templateResponse {
	return templateResponse{
		ID:        template.ID,
		Kind:      string(template.Kind),
		Name:      template.Name,
		Amount:    int64(template.Amount),
		DueDay:    template.DueDay,
		CardID:    template.CardID,
		TagID:     template.TagID,
		Active:    template.Active,
		CreatedAt: template.CreatedAt,
		UpdatedAt: template.UpdatedAt,
	}
}

func (h *Handler) listPeriods(w http.ResponseWriter, r *http.Request) {
	periods, err := h.UseCases.ListPeriods.Execute(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	items := make([]periodResponse, 0, len(periods))
	for _, period := range periods {
		items = append(items, toPeriodResponse(period))
	}
	platform.WriteJSON(w, http.StatusOK, periodListResponse{Items: items})
}

func (h *Handler) getPeriod(w http.ResponseWriter, r *http.Request) {
	month, ok := pathMonth(w, r)
	if !ok {
		return
	}
	period, err := h.UseCases.GetPeriod.Execute(r.Context(), month)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, toPeriodResponse(period))
}

func (h *Handler) openPeriod(w http.ResponseWriter, r *http.Request) {
	var body openPeriodBody
	if err := platform.DecodeJSON(w, r, &body); err != nil {
		writeBadRequest(w)
		return
	}
	var fields []platform.FieldError
	raw := requiredString(body.Period, "period", &fields)
	if len(fields) > 0 {
		writeBadRequest(w, fields...)
		return
	}
	month, err := kernel.ParseYearMonth(raw)
	if err != nil {
		writeBadRequest(w, platform.FieldError{Field: "period", Message: monthMessage})
		return
	}
	opened, err := h.UseCases.OpenPeriod.Execute(r.Context(), month)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	status := http.StatusOK
	if opened.Created {
		status = http.StatusCreated
	}
	platform.WriteJSON(w, status, toPeriodResponse(opened.Period))
}

func (h *Handler) updatePeriod(w http.ResponseWriter, r *http.Request) {
	month, ok := pathMonth(w, r)
	if !ok {
		return
	}
	var body updatePeriodBody
	if err := platform.DecodeJSON(w, r, &body); err != nil {
		writeBadRequest(w)
		return
	}
	if body.OpeningBalance == nil {
		writeBadRequest(w, platform.FieldError{Field: "openingBalance", Message: "is required"})
		return
	}
	var raw *int64
	if string(body.OpeningBalance) != "null" {
		value, ok := parseInteger(body.OpeningBalance)
		if !ok {
			writeBadRequest(w, platform.FieldError{Field: "openingBalance", Message: domain.OpeningBalanceMessage})
			return
		}
		raw = &value
	}
	amount, err := domain.NewOpeningBalance(raw)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	period, err := h.UseCases.SetOpeningBalance.Execute(r.Context(), month, amount)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, toPeriodResponse(period))
}

func (h *Handler) closePeriod(w http.ResponseWriter, r *http.Request) {
	month, ok := pathMonth(w, r)
	if !ok {
		return
	}
	period, err := h.UseCases.ClosePeriod.Execute(r.Context(), month)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, toPeriodResponse(period))
}

func (h *Handler) listTemplates(w http.ResponseWriter, r *http.Request) {
	var filter application.TemplateFilter
	if r.URL.Query().Has("active") {
		switch r.URL.Query().Get("active") {
		case "true":
			active := true
			filter.Active = &active
		case "false":
			active := false
			filter.Active = &active
		default:
			writeBadRequest(w, platform.FieldError{Field: "active", Message: "must be true or false"})
			return
		}
	}
	templates, err := h.UseCases.ListTemplates.Execute(r.Context(), filter)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	items := make([]templateResponse, 0, len(templates))
	for _, template := range templates {
		items = append(items, toTemplateResponse(template))
	}
	platform.WriteJSON(w, http.StatusOK, templateListResponse{Items: items})
}

func (h *Handler) getTemplate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	template, err := h.UseCases.GetTemplate.Execute(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, toTemplateResponse(template))
}

func (h *Handler) createTemplate(w http.ResponseWriter, r *http.Request) {
	var body templateBody
	if err := platform.DecodeJSON(w, r, &body); err != nil {
		writeBadRequest(w)
		return
	}
	var fields []platform.FieldError
	input := domain.NewTemplateInput{
		Kind:   requiredString(body.Kind, "kind", &fields),
		Name:   requiredString(body.Name, "name", &fields),
		DueDay: optionalDueDay(body.DueDay, &fields),
		Active: optionalBool(body.Active, "active", &fields),
	}
	if body.Amount == nil {
		fields = append(fields, platform.FieldError{Field: "amount", Message: "is required"})
	} else if amount := optionalAmount(body.Amount, &fields); amount != nil {
		input.Amount = *amount
	}
	// Only a tag may be null on create: a card is either required or not allowed.
	if body.CardID != nil && string(body.CardID) == "null" {
		fields = append(fields, platform.FieldError{Field: "cardId", Message: "must be a UUID"})
	} else {
		input.CardID = nullableID(body.CardID, "cardId", &fields).Value
	}
	input.TagID = nullableID(body.TagID, "tagId", &fields).Value
	if len(fields) > 0 {
		writeBadRequest(w, fields...)
		return
	}
	template, err := domain.CreateNewTemplate(input)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	created, err := h.UseCases.CreateTemplate.Execute(r.Context(), template)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	platform.WriteJSON(w, http.StatusCreated, toTemplateResponse(created))
}

func (h *Handler) updateTemplate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body templateBody
	if err := platform.DecodeJSON(w, r, &body); err != nil {
		writeBadRequest(w)
		return
	}
	if body.Kind != nil {
		writeBadRequest(w, platform.FieldError{Field: "kind", Message: "cannot be changed"})
		return
	}
	if body.Name == nil && body.Amount == nil && body.DueDay == nil && body.CardID == nil && body.TagID == nil && body.Active == nil {
		platform.WriteProblem(w, http.StatusBadRequest, "Send at least one of name, amount, dueDay, cardId, tagId or active.")
		return
	}
	var fields []platform.FieldError
	input := domain.TemplateChangesInput{
		Name:   optionalString(body.Name, "name", &fields),
		Amount: optionalAmount(body.Amount, &fields),
		DueDay: optionalDueDay(body.DueDay, &fields),
		CardID: nullableID(body.CardID, "cardId", &fields),
		TagID:  nullableID(body.TagID, "tagId", &fields),
		Active: optionalBool(body.Active, "active", &fields),
	}
	if len(fields) > 0 {
		writeBadRequest(w, fields...)
		return
	}
	changes, err := domain.NewTemplateChanges(input)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	updated, err := h.UseCases.UpdateTemplate.Execute(r.Context(), id, changes)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, toTemplateResponse(updated))
}

func (h *Handler) deleteTemplate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.UseCases.DeleteTemplate.Execute(r.Context(), id); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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

func optionalBool(raw json.RawMessage, field string, fields *[]platform.FieldError) *bool {
	if raw == nil {
		return nil
	}
	var value *bool
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		*fields = append(*fields, platform.FieldError{Field: field, Message: "must be true or false"})
		return nil
	}
	return value
}

func parseInteger(raw json.RawMessage) (int64, bool) {
	text := string(raw)
	value, err := strconv.ParseInt(text, 10, 64)
	if !integerPattern.MatchString(text) || err != nil {
		return 0, false
	}
	return value, true
}

func optionalAmount(raw json.RawMessage, fields *[]platform.FieldError) *int64 {
	if raw == nil {
		return nil
	}
	value, ok := parseInteger(raw)
	if !ok {
		*fields = append(*fields, platform.FieldError{Field: "amount", Message: domain.AmountOutOfRangeMessage})
		return nil
	}
	return &value
}

func optionalDueDay(raw json.RawMessage, fields *[]platform.FieldError) *int {
	if raw == nil {
		return nil
	}
	value, ok := parseInteger(raw)
	if !ok || value < 1 || value > 31 {
		*fields = append(*fields, platform.FieldError{Field: "dueDay", Message: domain.DueDayOutOfRangeMessage})
		return nil
	}
	day := int(value)
	return &day
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
	case errors.Is(err, domain.ErrPeriodNotFound), errors.Is(err, domain.ErrTemplateNotFound):
		platform.WriteProblem(w, http.StatusNotFound, "No such resource.")
	default:
		h.Logger.LogAttrs(r.Context(), slog.LevelError, "periods request failed",
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
