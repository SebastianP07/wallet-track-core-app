package platform

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"
)

const maxJSONBodyBytes = 16 << 10

// FieldError names one rejected value of a 400.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type problem struct {
	Type   string       `json:"type"`
	Title  string       `json:"title"`
	Status int          `json:"status"`
	Detail string       `json:"detail,omitempty"`
	Errors []FieldError `json:"errors,omitempty"`
}

// WriteProblem sends an RFC 9457 body, the only error format of the API.
func WriteProblem(w http.ResponseWriter, status int, detail string, fields ...FieldError) {
	w.Header().Set("Content-Type", "application/problem+json")
	writeBody(w, status, problem{
		Type:   "about:blank",
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
		Errors: fields,
	})
}

// WriteTooManyRequests is the single 429 of the API, so a rate limit cannot be told apart from
// a lockout. The body never varies and Retry-After is rounded up.
func WriteTooManyRequests(w http.ResponseWriter, retryAfter time.Duration) {
	seconds := int64((retryAfter + time.Second - 1) / time.Second)
	w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
	WriteProblem(w, http.StatusTooManyRequests, "Too many attempts. Try again later.")
}

// DecodeJSON reads one JSON document of at most maxJSONBodyBytes, refusing unknown fields.
func DecodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.More() {
		return errors.New("trailing data after the JSON body")
	}
	return nil
}

func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	writeBody(w, status, body)
}

func writeBody(w http.ResponseWriter, status int, body any) {
	encoded, err := json.Marshal(body)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(status)
	// A failed write means the client is gone; there is nobody left to tell.
	if _, err := w.Write(encoded); err != nil {
		return
	}
}
