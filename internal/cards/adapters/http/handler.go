// Package http is the inbound HTTP adapter of the credit cards module.
package http

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/SebastianP07/wallet-track-core-app/internal/cards/application"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/platform"
)

type Config struct {
	List *application.ListCards
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

// Register adds the credit cards routes built so far.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/credit-cards", platform.Chain(http.HandlerFunc(h.list), h.RequireSession))
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

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	cards, err := h.List.Execute(r.Context())
	if err != nil {
		h.Logger.LogAttrs(r.Context(), slog.LevelError, "credit cards request failed",
			slog.String("error", err.Error()),
			slog.String("path", r.URL.Path),
			slog.String("request_id", platform.RequestIDFrom(r.Context())),
		)
		platform.WriteProblem(w, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	items := make([]cardResponse, 0, len(cards))
	for _, card := range cards {
		items = append(items, cardResponse{
			ID:           card.ID,
			Name:         card.Name,
			Issuer:       card.Issuer,
			StatementDay: card.StatementDay,
			DueDay:       card.DueDay,
			CreatedAt:    card.CreatedAt,
			UpdatedAt:    card.UpdatedAt,
		})
	}
	platform.WriteJSON(w, http.StatusOK, cardListResponse{Items: items})
}
