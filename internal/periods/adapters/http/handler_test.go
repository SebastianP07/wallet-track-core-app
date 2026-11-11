package http_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	periodshttp "github.com/SebastianP07/wallet-track-core-app/internal/periods/adapters/http"
	"github.com/SebastianP07/wallet-track-core-app/internal/periods/application"
	"github.com/SebastianP07/wallet-track-core-app/internal/periods/domain"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
)

// openingRepo knows one month, November 2026; any call other than SetOpeningBalance panics on the embedded
// nil interface.
type openingRepo struct {
	application.Repository
	calls int
}

func (r *openingRepo) SetOpeningBalance(_ context.Context, month kernel.YearMonth, amount *kernel.Money) (domain.Period, error) {
	r.calls++
	if month != (kernel.YearMonth{Year: 2026, Month: time.November}) {
		return domain.Period{}, domain.ErrPeriodNotFound
	}
	return domain.Period{ID: "3f1c2a4e-0000-4000-8000-000000000001", Month: month, Status: domain.PeriodClosed, OpeningBalance: amount}, nil
}

func passThrough(next http.Handler) http.Handler { return next }

type problem struct {
	Errors []struct {
		Field   string `json:"field"`
		Message string `json:"message"`
	} `json:"errors"`
}

func TestUpdatePeriodOpeningBalance(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		body        string
		wantStatus  int
		wantField   string
		wantOpening string
	}{
		{name: "sets a value", path: "2026-11", body: `{"openingBalance": 24165723}`, wantStatus: http.StatusOK, wantOpening: "24165723"},
		{name: "sets a negative value", path: "2026-11", body: `{"openingBalance": -5}`, wantStatus: http.StatusOK, wantOpening: "-5"},
		{name: "null clears it", path: "2026-11", body: `{"openingBalance": null}`, wantStatus: http.StatusOK, wantOpening: "null"},
		{name: "the field is required", path: "2026-11", body: `{}`, wantStatus: http.StatusBadRequest, wantField: "openingBalance"},
		{name: "a string is refused", path: "2026-11", body: `{"openingBalance": "100"}`, wantStatus: http.StatusBadRequest, wantField: "openingBalance"},
		{name: "a decimal is refused", path: "2026-11", body: `{"openingBalance": 100.5}`, wantStatus: http.StatusBadRequest, wantField: "openingBalance"},
		{name: "an exponent is refused", path: "2026-11", body: `{"openingBalance": 1e3}`, wantStatus: http.StatusBadRequest, wantField: "openingBalance"},
		{name: "past the bound is refused", path: "2026-11", body: `{"openingBalance": 9007199254740992}`, wantStatus: http.StatusBadRequest, wantField: "openingBalance"},
		{name: "past int64 is refused", path: "2026-11", body: `{"openingBalance": 99999999999999999999}`, wantStatus: http.StatusBadRequest, wantField: "openingBalance"},
		{name: "an unknown field is refused", path: "2026-11", body: `{"openingBalance": 1, "status": "open"}`, wantStatus: http.StatusBadRequest},
		{name: "a malformed month is refused", path: "2026-13", body: `{"openingBalance": 1}`, wantStatus: http.StatusBadRequest, wantField: "period"},
		{name: "a month not opened is not found", path: "2026-12", body: `{"openingBalance": 1}`, wantStatus: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &openingRepo{}
			mux := http.NewServeMux()
			periodshttp.NewHandler(periodshttp.Config{
				UseCases:       periodshttp.UseCases{SetOpeningBalance: application.NewSetOpeningBalance(repo)},
				RequireSession: passThrough,
				RequireCSRF:    passThrough,
				Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
			}).Register(mux)

			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/api/v1/periods/"+tt.path, strings.NewReader(tt.body))
			mux.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantStatus == http.StatusBadRequest && repo.calls != 0 {
				t.Errorf("the use case ran %d times on a refused request", repo.calls)
			}
			if tt.wantField != "" {
				var body problem
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode problem: %v", err)
				}
				if len(body.Errors) != 1 || body.Errors[0].Field != tt.wantField {
					t.Errorf("errors = %+v, want one on %s", body.Errors, tt.wantField)
				}
			}
			if tt.wantOpening != "" {
				var body map[string]json.RawMessage
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode period: %v", err)
				}
				if got := string(body["openingBalance"]); got != tt.wantOpening {
					t.Errorf("openingBalance = %s, want %s", got, tt.wantOpening)
				}
			}
		})
	}
}
