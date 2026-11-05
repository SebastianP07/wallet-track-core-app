// Package http is the inbound HTTP adapter of the tags module.
package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/SebastianP07/wallet-track-core-app/internal/shared/platform"
	"github.com/SebastianP07/wallet-track-core-app/internal/tags/application"
	"github.com/SebastianP07/wallet-track-core-app/internal/tags/domain"
)

const basePath = "/api/v1"

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// UseCases are the operations this adapter exposes.
type UseCases struct {
	List   *application.ListTags
	Create *application.CreateTag
	Update *application.UpdateTag
	Delete *application.DeleteTag
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

// Register adds the tag routes of the contract.
func (h *Handler) Register(mux *http.ServeMux) {
	write := func(method, pattern string, handler http.HandlerFunc) {
		mux.Handle(method+" "+basePath+pattern, platform.Chain(handler, h.RequireCSRF, h.RequireSession))
	}

	mux.Handle("GET "+basePath+"/tags", platform.Chain(http.HandlerFunc(h.listTags), h.RequireSession))
	write(http.MethodPost, "/tags", h.createTag)
	write(http.MethodPatch, "/tags/{id}", h.updateTag)
	write(http.MethodDelete, "/tags/{id}", h.deleteTag)
}

type tagResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Color     string    `json:"color"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type tagListResponse struct {
	Items []tagResponse `json:"items"`
}

// tagBody keeps each field raw, so that a wrong type or a null is reported on its own field.
type tagBody struct {
	Name  json.RawMessage `json:"name"`
	Color json.RawMessage `json:"color"`
}

func toResponse(tag domain.Tag) tagResponse {
	return tagResponse{
		ID:        tag.ID,
		Name:      tag.Name,
		Color:     tag.Color,
		CreatedAt: tag.CreatedAt,
		UpdatedAt: tag.UpdatedAt,
	}
}

func (h *Handler) listTags(w http.ResponseWriter, r *http.Request) {
	tags, err := h.UseCases.List.Execute(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	items := make([]tagResponse, 0, len(tags))
	for _, tag := range tags {
		items = append(items, toResponse(tag))
	}
	platform.WriteJSON(w, http.StatusOK, tagListResponse{Items: items})
}

func (h *Handler) createTag(w http.ResponseWriter, r *http.Request) {
	var body tagBody
	if err := platform.DecodeJSON(w, r, &body); err != nil {
		writeBadRequest(w)
		return
	}
	var fields []platform.FieldError
	name := optionalString(body.Name, "name", &fields)
	color := optionalString(body.Color, "color", &fields)
	if body.Name == nil {
		fields = append(fields, platform.FieldError{Field: "name", Message: "is required"})
	}
	if body.Color == nil {
		fields = append(fields, platform.FieldError{Field: "color", Message: "is required"})
	}
	if len(fields) > 0 {
		writeBadRequest(w, fields...)
		return
	}
	tag, err := domain.CreateNewTag(*name, *color)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	created, err := h.UseCases.Create.Execute(r.Context(), tag)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	platform.WriteJSON(w, http.StatusCreated, toResponse(created))
}

func (h *Handler) updateTag(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body tagBody
	if err := platform.DecodeJSON(w, r, &body); err != nil {
		writeBadRequest(w)
		return
	}
	if body.Name == nil && body.Color == nil {
		platform.WriteProblem(w, http.StatusBadRequest, "Send at least one of name or color.")
		return
	}
	var fields []platform.FieldError
	name := optionalString(body.Name, "name", &fields)
	color := optionalString(body.Color, "color", &fields)
	if len(fields) > 0 {
		writeBadRequest(w, fields...)
		return
	}
	changes, err := domain.NewChanges(name, color)
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

func (h *Handler) deleteTag(w http.ResponseWriter, r *http.Request) {
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
	case errors.Is(err, domain.ErrNameTaken):
		platform.WriteProblem(w, http.StatusConflict, "Another tag already has that name.")
	default:
		h.Logger.LogAttrs(r.Context(), slog.LevelError, "tags request failed",
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
