package http

import (
	"net/http"
	"unicode/utf8"

	"github.com/SebastianP07/wallet-track-core-app/internal/auth/application"
	"github.com/SebastianP07/wallet-track-core-app/internal/auth/domain"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/platform"
)

const (
	passwordMaxChars     = 1024
	recoveryCodeMaxChars = 64
)

type passwordLoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginStepResponse struct {
	NextStep string `json:"nextStep"`
}

type totpCodeRequest struct {
	Code string `json:"code"`
}

type recoveryCodeRequest struct {
	Code string `json:"code"`
}

type userResponse struct {
	ID         string `json:"id"`
	Email      string `json:"email"`
	MFAEnabled bool   `json:"mfaEnabled"`
}

type enrollmentStartRequest struct {
	EnrollmentSecret string `json:"enrollmentSecret"`
}

type enrollmentResponse struct {
	Secret     string `json:"secret"`
	OTPAuthURI string `json:"otpauthUri"`
}

type enrollmentResultResponse struct {
	User          userResponse `json:"user"`
	RecoveryCodes []string     `json:"recoveryCodes"`
}

type recoveryCodeListResponse struct {
	RecoveryCodes []string `json:"recoveryCodes"`
}

type recoveryCodeStatusResponse struct {
	Remaining int64 `json:"remaining"`
}

func toUserResponse(user domain.User) userResponse {
	return userResponse{ID: user.ID, Email: user.Email, MFAEnabled: user.MFAEnabled}
}

func (h *Handler) loginWithPassword(w http.ResponseWriter, r *http.Request) {
	var body passwordLoginRequest
	if err := decodeBody(w, r, &body); err != nil {
		writeBadRequest(w)
		return
	}
	email, err := domain.NormalizeEmail(body.Email)
	if err != nil {
		writeBadRequest(w, platform.FieldError{Field: "email", Message: "must be a valid email address"})
		return
	}
	if length := utf8.RuneCountInString(body.Password); length < 1 || length > passwordMaxChars {
		writeBadRequest(w, platform.FieldError{Field: "password", Message: "must have between 1 and 1024 characters"})
		return
	}

	result, err := h.UseCases.LoginPassword.Execute(r.Context(), email, body.Password, h.client(r))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.setSession(w, result.Session)
	platform.WriteJSON(w, http.StatusOK, loginStepResponse{NextStep: string(result.NextStep)})
}

// decodeTOTPCode answers the 400 itself and reports whether the handler may go on.
func decodeTOTPCode(w http.ResponseWriter, r *http.Request) (string, bool) {
	var body totpCodeRequest
	if err := decodeBody(w, r, &body); err != nil {
		writeBadRequest(w)
		return "", false
	}
	if !totpCodePattern.MatchString(body.Code) {
		writeBadRequest(w, platform.FieldError{Field: "code", Message: "must be 6 digits"})
		return "", false
	}
	return body.Code, true
}

func (h *Handler) loginWithTOTP(w http.ResponseWriter, r *http.Request) {
	code, ok := decodeTOTPCode(w, r)
	if !ok || !h.allowPrincipal(w, r) {
		return
	}
	principal := principalFrom(r)
	session, err := h.UseCases.LoginTOTP.Execute(r.Context(), principal, code, h.client(r))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.setSession(w, session)
	platform.WriteJSON(w, http.StatusOK, toUserResponse(principal.User))
}

func (h *Handler) loginWithRecoveryCode(w http.ResponseWriter, r *http.Request) {
	var body recoveryCodeRequest
	if err := decodeBody(w, r, &body); err != nil {
		writeBadRequest(w)
		return
	}
	if length := utf8.RuneCountInString(body.Code); length < 1 || length > recoveryCodeMaxChars {
		writeBadRequest(w, platform.FieldError{Field: "code", Message: "must have between 1 and 64 characters"})
		return
	}
	if !h.allowPrincipal(w, r) {
		return
	}
	principal := principalFrom(r)
	session, err := h.UseCases.LoginRecovery.Execute(r.Context(), principal, body.Code, h.client(r))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.setSession(w, session)
	platform.WriteJSON(w, http.StatusOK, toUserResponse(principal.User))
}

func (h *Handler) startMFAEnrollment(w http.ResponseWriter, r *http.Request) {
	// The contract answers a malformed or missing secret like a wrong one, never with a 400.
	var body enrollmentStartRequest
	if err := decodeBody(w, r, &body); err != nil {
		body = enrollmentStartRequest{}
	}
	if !h.allowPrincipal(w, r) {
		return
	}
	enrollment, err := h.UseCases.StartEnrollment.Execute(r.Context(), principalFrom(r), body.EnrollmentSecret, h.client(r))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, enrollmentResponse{Secret: enrollment.Secret, OTPAuthURI: enrollment.OTPAuthURI})
}

func (h *Handler) confirmMFAEnrollment(w http.ResponseWriter, r *http.Request) {
	code, ok := decodeTOTPCode(w, r)
	if !ok || !h.allowPrincipal(w, r) {
		return
	}
	result, err := h.UseCases.ConfirmEnroll.Execute(r.Context(), principalFrom(r), code, h.client(r))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.setSession(w, result.Session)
	platform.WriteJSON(w, http.StatusOK, enrollmentResultResponse{
		User:          toUserResponse(result.User),
		RecoveryCodes: result.RecoveryCodes,
	})
}

func (h *Handler) getRecoveryCodeStatus(w http.ResponseWriter, r *http.Request) {
	remaining, err := h.UseCases.RecoveryStatus.Execute(r.Context(), principalFrom(r).User.ID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, recoveryCodeStatusResponse{Remaining: remaining})
}

func (h *Handler) regenerateRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	code, ok := decodeTOTPCode(w, r)
	if !ok || !h.allowPrincipal(w, r) {
		return
	}
	codes, err := h.UseCases.RegenerateCodes.Execute(r.Context(), principalFrom(r), code, h.client(r))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, recoveryCodeListResponse{RecoveryCodes: codes})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if err := h.UseCases.Logout.Execute(r.Context(), principalFrom(r), h.client(r)); err != nil {
		h.writeError(w, r, err)
		return
	}
	h.clearSession(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) logoutAll(w http.ResponseWriter, r *http.Request) {
	if err := h.UseCases.LogoutAll.Execute(r.Context(), principalFrom(r), h.client(r)); err != nil {
		h.writeError(w, r, err)
		return
	}
	h.clearSession(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) getMe(w http.ResponseWriter, r *http.Request) {
	platform.WriteJSON(w, http.StatusOK, toUserResponse(principalFrom(r).User))
}

var _ = application.NextStepTOTP
