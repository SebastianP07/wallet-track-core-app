package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/SebastianP07/wallet-track-core-app/internal/auth/application"
	"github.com/SebastianP07/wallet-track-core-app/internal/auth/domain"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/platform"
)

type secondFactorProofRequest struct {
	Method string `json:"method"`
	Code   string `json:"code"`
}

type passkeyAssertionRequest struct {
	Credential json.RawMessage `json:"credential"`
}

type passkeyRegistrationRequest struct {
	Name       string          `json:"name"`
	Credential json.RawMessage `json:"credential"`
}

type passkeyRenameRequest struct {
	Name string `json:"name"`
}

type passkeyResponse struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
}

type passkeyListResponse struct {
	Items []passkeyResponse `json:"items"`
}

func toPasskeyResponse(passkey domain.Passkey) passkeyResponse {
	response := passkeyResponse{ID: passkey.ID, Name: passkey.Name, CreatedAt: passkey.CreatedAt}
	if !passkey.LastUsedAt.IsZero() {
		response.LastUsedAt = &passkey.LastUsedAt
	}
	return response
}

// decodeProof answers the 400 itself and reports whether the handler may go on.
func decodeProof(w http.ResponseWriter, r *http.Request) (application.SecondFactorProof, bool) {
	var body secondFactorProofRequest
	if err := platform.DecodeJSON(w, r, &body); err != nil {
		writeBadRequest(w)
		return application.SecondFactorProof{}, false
	}
	switch application.ProofMethod(body.Method) {
	case application.ProofTOTP:
		if !totpCodePattern.MatchString(body.Code) {
			writeBadRequest(w, platform.FieldError{Field: "code", Message: "must be 6 digits"})
			return application.SecondFactorProof{}, false
		}
	case application.ProofRecoveryCode:
		if length := utf8.RuneCountInString(body.Code); length < 1 || length > recoveryCodeMaxChars {
			writeBadRequest(w, platform.FieldError{Field: "code", Message: "must have between 1 and 64 characters"})
			return application.SecondFactorProof{}, false
		}
	default:
		writeBadRequest(w, platform.FieldError{Field: "method", Message: "must be totp or recovery_code"})
		return application.SecondFactorProof{}, false
	}
	return application.SecondFactorProof{Method: application.ProofMethod(body.Method), Code: body.Code}, true
}

func decodeCredential(w http.ResponseWriter, credential json.RawMessage) bool {
	if len(credential) == 0 || string(credential) == "null" {
		writeBadRequest(w, platform.FieldError{Field: "credential", Message: "is required"})
		return false
	}
	return true
}

func decodePasskeyName(w http.ResponseWriter, raw string) (string, bool) {
	name, err := domain.NormalizePasskeyName(raw)
	if err != nil {
		writeBadRequest(w, platform.FieldError{Field: "name", Message: "must have between 1 and 120 characters"})
		return "", false
	}
	return name, true
}

func pathID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if !uuidPattern.MatchString(id) {
		writeBadRequest(w, platform.FieldError{Field: "id", Message: "must be a UUID"})
		return "", false
	}
	return id, true
}

// writeOptions sends WebAuthn options as the verifier encoded them.
func writeOptions(w http.ResponseWriter, options []byte) {
	platform.WriteJSON(w, http.StatusOK, json.RawMessage(options))
}

func (h *Handler) startPasskeyLogin(w http.ResponseWriter, r *http.Request) {
	options, err := h.UseCases.StartPasskey.Execute(r.Context(), principalFrom(r), h.client(r))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeOptions(w, options)
}

func (h *Handler) loginWithPasskey(w http.ResponseWriter, r *http.Request) {
	var body passkeyAssertionRequest
	if err := platform.DecodeJSON(w, r, &body); err != nil {
		writeBadRequest(w)
		return
	}
	if !decodeCredential(w, body.Credential) || !h.allowPrincipal(w, r) {
		return
	}
	principal := principalFrom(r)
	session, err := h.UseCases.LoginPasskey.Execute(r.Context(), principal, body.Credential, h.client(r))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.setSession(w, session)
	platform.WriteJSON(w, http.StatusOK, toUserResponse(principal.User))
}

func (h *Handler) startPasskeyRegistration(w http.ResponseWriter, r *http.Request) {
	proof, ok := decodeProof(w, r)
	if !ok || !h.allowPrincipal(w, r) {
		return
	}
	options, err := h.UseCases.StartRegister.Execute(r.Context(), principalFrom(r), proof, h.client(r))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeOptions(w, options)
}

func (h *Handler) finishPasskeyRegistration(w http.ResponseWriter, r *http.Request) {
	var body passkeyRegistrationRequest
	if err := platform.DecodeJSON(w, r, &body); err != nil {
		writeBadRequest(w)
		return
	}
	name, ok := decodePasskeyName(w, body.Name)
	if !ok || !decodeCredential(w, body.Credential) {
		return
	}
	passkey, err := h.UseCases.FinishRegister.Execute(r.Context(), principalFrom(r), name, body.Credential, h.client(r))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	platform.WriteJSON(w, http.StatusCreated, toPasskeyResponse(passkey))
}

func (h *Handler) listPasskeys(w http.ResponseWriter, r *http.Request) {
	passkeys, err := h.UseCases.ListPasskeys.Execute(r.Context(), principalFrom(r).User.ID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	items := make([]passkeyResponse, 0, len(passkeys))
	for _, passkey := range passkeys {
		items = append(items, toPasskeyResponse(passkey))
	}
	platform.WriteJSON(w, http.StatusOK, passkeyListResponse{Items: items})
}

func (h *Handler) renamePasskey(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body passkeyRenameRequest
	if err := platform.DecodeJSON(w, r, &body); err != nil {
		writeBadRequest(w)
		return
	}
	name, ok := decodePasskeyName(w, body.Name)
	if !ok {
		return
	}
	passkey, err := h.UseCases.RenamePasskey.Execute(r.Context(), principalFrom(r).User.ID, id, name)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, toPasskeyResponse(passkey))
}

func (h *Handler) removePasskey(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	proof, ok := decodeProof(w, r)
	if !ok || !h.allowPrincipal(w, r) {
		return
	}
	err := h.UseCases.RemovePasskey.Execute(r.Context(), principalFrom(r), id, proof, h.client(r))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type sessionResponse struct {
	ID         string    `json:"id"`
	UserAgent  *string   `json:"userAgent"`
	IP         *string   `json:"ip"`
	CreatedAt  time.Time `json:"createdAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	Current    bool      `json:"current"`
	MFAPending bool      `json:"mfaPending"`
}

type sessionListResponse struct {
	Items []sessionResponse `json:"items"`
}

func toSessionResponse(session domain.ActiveSession, currentID string) sessionResponse {
	response := sessionResponse{
		ID:         session.ID,
		CreatedAt:  session.CreatedAt,
		LastSeenAt: session.LastSeenAt,
		Current:    session.ID == currentID,
		MFAPending: session.Pending,
	}
	if session.UserAgent != "" {
		response.UserAgent = &session.UserAgent
	}
	if session.IP.IsValid() {
		ip := session.IP.String()
		response.IP = &ip
	}
	return response
}

func (h *Handler) listSessions(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	sessions, err := h.UseCases.ListSessions.Execute(r.Context(), principal.User.ID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	items := make([]sessionResponse, 0, len(sessions))
	for _, session := range sessions {
		items = append(items, toSessionResponse(session, principal.Session.ID))
	}
	platform.WriteJSON(w, http.StatusOK, sessionListResponse{Items: items})
}

func (h *Handler) revokeSession(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	principal := principalFrom(r)
	if err := h.UseCases.RevokeSession.Execute(r.Context(), principal, id, h.client(r)); err != nil {
		h.writeError(w, r, err)
		return
	}
	if id == principal.Session.ID {
		h.clearSession(w)
	}
	w.WriteHeader(http.StatusNoContent)
}

var _ = errors.New
