// Package http is the inbound HTTP adapter of the platforms module: people, their payments, platforms
// and memberships.
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

	"github.com/SebastianP07/wallet-track-core-app/internal/platforms/application"
	"github.com/SebastianP07/wallet-track-core-app/internal/platforms/domain"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/platform"
)

const (
	basePath     = "/api/v1"
	dateLayout   = "2006-01-02"
	monthMessage = "must be a month as YYYY-MM, from 2000-01 on"
)

var (
	uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	// Plain JSON integers only: 1.0, 1e3 and "1" all name a whole number but are not one on the wire.
	integerPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)
)

// UseCases are the operations this adapter exposes.
type UseCases struct {
	ListPeople     *application.ListPeople
	GetPerson      *application.GetPerson
	CreatePerson   *application.CreatePerson
	UpdatePerson   *application.UpdatePerson
	DeletePerson   *application.DeletePerson
	ListPayments   *application.ListPersonPayments
	RecordPayment  *application.RecordPayment
	DeletePayment  *application.DeletePayment
	ListPlatforms  *application.ListPlatforms
	GetPlatform    *application.GetPlatform
	CreatePlatform *application.CreatePlatform
	UpdatePlatform *application.UpdatePlatform
	DeletePlatform *application.DeletePlatform
	AddMember      *application.AddMember
	RemoveMember   *application.RemoveMember
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

// Register adds the people and platforms routes of the contract.
func (h *Handler) Register(mux *http.ServeMux) {
	read := func(pattern string, handler http.HandlerFunc) {
		mux.Handle("GET "+basePath+pattern, platform.Chain(handler, h.RequireSession))
	}
	write := func(method, pattern string, handler http.HandlerFunc) {
		mux.Handle(method+" "+basePath+pattern, platform.Chain(handler, h.RequireCSRF, h.RequireSession))
	}

	read("/people", h.listPeople)
	write(http.MethodPost, "/people", h.createPerson)
	read("/people/{id}", h.getPerson)
	write(http.MethodPatch, "/people/{id}", h.updatePerson)
	write(http.MethodDelete, "/people/{id}", h.deletePerson)
	read("/people/{id}/payments", h.listPayments)
	write(http.MethodPost, "/people/{id}/payments", h.recordPayment)
	write(http.MethodDelete, "/platform-payments/{id}", h.deletePayment)
	read("/platforms", h.listPlatforms)
	write(http.MethodPost, "/platforms", h.createPlatform)
	read("/platforms/{id}", h.getPlatform)
	write(http.MethodPatch, "/platforms/{id}", h.updatePlatform)
	write(http.MethodDelete, "/platforms/{id}", h.deletePlatform)
	write(http.MethodPut, "/platforms/{id}/members/{personId}", h.addMember)
	write(http.MethodDelete, "/platforms/{id}/members/{personId}", h.removeMember)
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

type paymentResponse struct {
	ID            string    `json:"id"`
	PersonID      string    `json:"personId"`
	PaidAt        string    `json:"paidAt"`
	MonthsCovered int       `json:"monthsCovered"`
	Amount        int64     `json:"amount"`
	CoveredUntil  string    `json:"coveredUntil"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type paymentListResponse struct {
	Items []paymentResponse `json:"items"`
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

// These keep each field raw, so that a wrong type or a null is reported on its own field and an absent
// field can be told apart from a null one.
type personBody struct {
	Name          json.RawMessage `json:"name"`
	IsOwner       json.RawMessage `json:"isOwner"`
	BillingMonths json.RawMessage `json:"billingMonths"`
}

type paymentBody struct {
	PaidAt        json.RawMessage `json:"paidAt"`
	MonthsCovered json.RawMessage `json:"monthsCovered"`
}

type platformBody struct {
	Name        json.RawMessage `json:"name"`
	MonthlyCost json.RawMessage `json:"monthlyCost"`
	CardID      json.RawMessage `json:"cardId"`
}

func formatDate(date *time.Time) *string {
	if date == nil {
		return nil
	}
	text := date.Format(dateLayout)
	return &text
}

func toPersonResponse(view domain.PersonView) personResponse {
	return personResponse{
		ID:              view.ID,
		Name:            view.Name,
		IsOwner:         view.IsOwner,
		BillingMonths:   view.BillingMonths,
		MonthlyOwed:     int64(view.MonthlyOwed),
		LastPaymentDate: formatDate(view.LastPaymentDate),
		CoveredUntil:    formatDate(view.CoveredUntil),
		CoveredInPeriod: view.CoveredInPeriod,
		PeriodPaymentID: view.PeriodPaymentID,
		CreatedAt:       view.CreatedAt,
		UpdatedAt:       view.UpdatedAt,
	}
}

func toPaymentResponse(payment domain.Payment) paymentResponse {
	return paymentResponse{
		ID:            payment.ID,
		PersonID:      payment.PersonID,
		PaidAt:        payment.PaidAt.Format(dateLayout),
		MonthsCovered: payment.MonthsCovered,
		Amount:        int64(payment.Amount),
		CoveredUntil:  payment.CoveredUntil.Format(dateLayout),
		CreatedAt:     payment.CreatedAt,
		UpdatedAt:     payment.UpdatedAt,
	}
}

func toPlatformResponse(p domain.Platform) platformResponse {
	members := make([]memberResponse, 0, len(p.Members))
	for _, member := range p.Members {
		members = append(members, memberResponse(member))
	}
	return platformResponse{
		ID:             p.ID,
		Name:           p.Name,
		MonthlyCost:    int64(p.MonthlyCost),
		CardID:         p.CardID,
		SharePerMember: int64(p.SharePerMember()),
		Members:        members,
		CreatedAt:      p.CreatedAt,
		UpdatedAt:      p.UpdatedAt,
	}
}

func (h *Handler) listPeople(w http.ResponseWriter, r *http.Request) {
	var month *kernel.YearMonth
	if query := r.URL.Query(); query.Has("period") {
		parsed, err := kernel.ParseYearMonth(query.Get("period"))
		if err != nil {
			writeBadRequest(w, platform.FieldError{Field: "period", Message: monthMessage})
			return
		}
		month = &parsed
	}
	people, err := h.UseCases.ListPeople.Execute(r.Context(), month)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	items := make([]personResponse, 0, len(people))
	for _, person := range people {
		items = append(items, toPersonResponse(person))
	}
	platform.WriteJSON(w, http.StatusOK, personListResponse{Items: items})
}

func (h *Handler) getPerson(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	person, err := h.UseCases.GetPerson.Execute(r.Context(), id)
	h.writePerson(w, r, http.StatusOK, person, err)
}

func (h *Handler) createPerson(w http.ResponseWriter, r *http.Request) {
	var body personBody
	if err := platform.DecodeJSON(w, r, &body); err != nil {
		writeBadRequest(w)
		return
	}
	var fields []platform.FieldError
	if body.Name == nil {
		fields = append(fields, platform.FieldError{Field: "name", Message: "is required"})
	}
	input := personInput(body, &fields)
	if len(fields) > 0 {
		writeBadRequest(w, fields...)
		return
	}
	person, err := domain.CreateNewPerson(input)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	created, err := h.UseCases.CreatePerson.Execute(r.Context(), person)
	h.writePerson(w, r, http.StatusCreated, created, err)
}

func (h *Handler) updatePerson(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body personBody
	if err := platform.DecodeJSON(w, r, &body); err != nil {
		writeBadRequest(w)
		return
	}
	if body.Name == nil && body.IsOwner == nil && body.BillingMonths == nil {
		platform.WriteProblem(w, http.StatusBadRequest, "Send at least one of name, isOwner or billingMonths.")
		return
	}
	var fields []platform.FieldError
	input := personInput(body, &fields)
	if len(fields) > 0 {
		writeBadRequest(w, fields...)
		return
	}
	changes, err := domain.NewPersonChanges(input)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	updated, err := h.UseCases.UpdatePerson.Execute(r.Context(), id, changes)
	h.writePerson(w, r, http.StatusOK, updated, err)
}

func (h *Handler) deletePerson(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.UseCases.DeletePerson.Execute(r.Context(), id); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listPayments(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	payments, err := h.UseCases.ListPayments.Execute(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	items := make([]paymentResponse, 0, len(payments))
	for _, payment := range payments {
		items = append(items, toPaymentResponse(payment))
	}
	platform.WriteJSON(w, http.StatusOK, paymentListResponse{Items: items})
}

func (h *Handler) recordPayment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body paymentBody
	if err := platform.DecodeJSON(w, r, &body); err != nil {
		writeBadRequest(w)
		return
	}
	var fields []platform.FieldError
	var paidAt string
	if body.PaidAt == nil {
		fields = append(fields, platform.FieldError{Field: "paidAt", Message: "is required"})
	} else if err := json.Unmarshal(body.PaidAt, &paidAt); err != nil {
		fields = append(fields, platform.FieldError{Field: "paidAt", Message: domain.PaidAtMessage})
	}
	months := optionalMonths(body.MonthsCovered, "monthsCovered", &fields)
	if len(fields) > 0 {
		writeBadRequest(w, fields...)
		return
	}
	request, err := domain.NewPaymentRequest(paidAt, months)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	payment, err := h.UseCases.RecordPayment.Execute(r.Context(), id, request)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	platform.WriteJSON(w, http.StatusCreated, toPaymentResponse(payment))
}

func (h *Handler) deletePayment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.UseCases.DeletePayment.Execute(r.Context(), id); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listPlatforms(w http.ResponseWriter, r *http.Request) {
	platforms, err := h.UseCases.ListPlatforms.Execute(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	items := make([]platformResponse, 0, len(platforms))
	for _, p := range platforms {
		items = append(items, toPlatformResponse(p))
	}
	platform.WriteJSON(w, http.StatusOK, platformListResponse{Items: items})
}

func (h *Handler) getPlatform(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	p, err := h.UseCases.GetPlatform.Execute(r.Context(), id)
	h.writePlatform(w, r, http.StatusOK, p, err)
}

func (h *Handler) createPlatform(w http.ResponseWriter, r *http.Request) {
	var body platformBody
	if err := platform.DecodeJSON(w, r, &body); err != nil {
		writeBadRequest(w)
		return
	}
	var fields []platform.FieldError
	if body.Name == nil {
		fields = append(fields, platform.FieldError{Field: "name", Message: "is required"})
	}
	if body.MonthlyCost == nil {
		fields = append(fields, platform.FieldError{Field: "monthlyCost", Message: "is required"})
	}
	input := platformInput(body, &fields)
	if len(fields) > 0 {
		writeBadRequest(w, fields...)
		return
	}
	p, err := domain.CreateNewPlatform(input)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	created, err := h.UseCases.CreatePlatform.Execute(r.Context(), p)
	h.writePlatform(w, r, http.StatusCreated, created, err)
}

func (h *Handler) updatePlatform(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body platformBody
	if err := platform.DecodeJSON(w, r, &body); err != nil {
		writeBadRequest(w)
		return
	}
	if body.Name == nil && body.MonthlyCost == nil && body.CardID == nil {
		platform.WriteProblem(w, http.StatusBadRequest, "Send at least one of name, monthlyCost or cardId.")
		return
	}
	var fields []platform.FieldError
	input := platformInput(body, &fields)
	if len(fields) > 0 {
		writeBadRequest(w, fields...)
		return
	}
	changes, err := domain.NewPlatformChanges(input)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	updated, err := h.UseCases.UpdatePlatform.Execute(r.Context(), id, changes)
	h.writePlatform(w, r, http.StatusOK, updated, err)
}

func (h *Handler) deletePlatform(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.UseCases.DeletePlatform.Execute(r.Context(), id); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) addMember(w http.ResponseWriter, r *http.Request) {
	platformID, personID, ok := memberPath(w, r)
	if !ok {
		return
	}
	p, err := h.UseCases.AddMember.Execute(r.Context(), platformID, personID)
	h.writePlatform(w, r, http.StatusOK, p, err)
}

func (h *Handler) removeMember(w http.ResponseWriter, r *http.Request) {
	platformID, personID, ok := memberPath(w, r)
	if !ok {
		return
	}
	p, err := h.UseCases.RemoveMember.Execute(r.Context(), platformID, personID)
	h.writePlatform(w, r, http.StatusOK, p, err)
}

func (h *Handler) writePerson(w http.ResponseWriter, r *http.Request, status int, person domain.PersonView, err error) {
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	platform.WriteJSON(w, status, toPersonResponse(person))
}

func (h *Handler) writePlatform(w http.ResponseWriter, r *http.Request, status int, p domain.Platform, err error) {
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	platform.WriteJSON(w, status, toPlatformResponse(p))
}

func personInput(body personBody, fields *[]platform.FieldError) domain.PersonInput {
	input := domain.PersonInput{
		Name:          optionalString(body.Name, "name", fields),
		BillingMonths: optionalMonths(body.BillingMonths, "billingMonths", fields),
	}
	if body.IsOwner != nil {
		var value *bool
		if err := json.Unmarshal(body.IsOwner, &value); err != nil || value == nil {
			*fields = append(*fields, platform.FieldError{Field: "isOwner", Message: "must be true or false"})
		} else {
			input.IsOwner = value
		}
	}
	return input
}

func platformInput(body platformBody, fields *[]platform.FieldError) domain.PlatformInput {
	input := domain.PlatformInput{
		Name:        optionalString(body.Name, "name", fields),
		MonthlyCost: optionalCost(body.MonthlyCost, fields),
	}
	if body.CardID != nil {
		var value *string
		if err := json.Unmarshal(body.CardID, &value); err != nil || (value != nil && !uuidPattern.MatchString(*value)) {
			*fields = append(*fields, platform.FieldError{Field: "cardId", Message: "must be a UUID or null"})
		} else {
			input.CardID = domain.OptionalCard{Set: true}
			if value != nil {
				lowered := strings.ToLower(*value)
				input.CardID.Value = &lowered
			}
		}
	}
	return input
}

func memberPath(w http.ResponseWriter, r *http.Request) (platformID, personID string, ok bool) {
	if platformID, ok = pathID(w, r, "id"); !ok {
		return "", "", false
	}
	if personID, ok = pathID(w, r, "personId"); !ok {
		return "", "", false
	}
	return platformID, personID, true
}

// pathID answers the 400 itself and reports whether the handler may go on.
func pathID(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	id := r.PathValue(name)
	if !uuidPattern.MatchString(id) {
		writeBadRequest(w, platform.FieldError{Field: name, Message: "must be a UUID"})
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

func optionalCost(raw json.RawMessage, fields *[]platform.FieldError) *int64 {
	if raw == nil {
		return nil
	}
	text := string(raw)
	value, err := strconv.ParseInt(text, 10, 64)
	if !integerPattern.MatchString(text) || err != nil {
		*fields = append(*fields, platform.FieldError{Field: "monthlyCost", Message: domain.MoneyMessage})
		return nil
	}
	return &value
}

func optionalMonths(raw json.RawMessage, field string, fields *[]platform.FieldError) *int {
	if raw == nil {
		return nil
	}
	text := string(raw)
	value, err := strconv.Atoi(text)
	if !integerPattern.MatchString(text) || err != nil {
		*fields = append(*fields, platform.FieldError{Field: field, Message: domain.BillingMonthsMessage})
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
	case errors.Is(err, domain.ErrPersonNotFound),
		errors.Is(err, domain.ErrPlatformNotFound),
		errors.Is(err, domain.ErrPaymentNotFound):
		platform.WriteProblem(w, http.StatusNotFound, "No such resource.")
	case errors.Is(err, domain.ErrOwnerExists):
		platform.WriteProblem(w, http.StatusConflict, "Another person is already the owner.")
	case errors.Is(err, domain.ErrPersonHasPayments):
		platform.WriteProblem(w, http.StatusConflict, "This person has payments, which are kept as history, so they cannot be deleted.")
	default:
		h.Logger.LogAttrs(r.Context(), slog.LevelError, "platforms request failed",
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
