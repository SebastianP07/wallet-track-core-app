package domain

import "net/netip"

type EventType string

const (
	EventPasswordAccepted          EventType = "login_password_accepted"
	EventLoginFailed               EventType = "login_failed"
	EventLoginLocked               EventType = "login_locked"
	EventLoginSucceededTOTP        EventType = "login_succeeded_totp"
	EventLoginSucceededRecovery    EventType = "login_succeeded_recovery_code"
	EventLoginSucceededPasskey     EventType = "login_succeeded_passkey"
	EventTOTPFailed                EventType = "mfa_totp_failed"
	EventTOTPReplayed              EventType = "mfa_totp_replayed"
	EventRecoveryCodeFailed        EventType = "mfa_recovery_code_failed"
	EventPasskeyFailed             EventType = "mfa_passkey_failed"               //nolint:gosec // An audit label, not a credential.
	EventPasskeySignCountRegressed EventType = "mfa_passkey_sign_count_regressed" //nolint:gosec // An audit label, not a credential.
	EventPasskeyRegistered         EventType = "passkey_registered"
	EventPasskeyRegistrationFailed EventType = "passkey_registration_failed"
	EventPasskeyRemoved            EventType = "passkey_removed"
	EventRecoveryCodesRegenerated  EventType = "mfa_recovery_codes_regenerated"
	EventEnrollmentStarted         EventType = "mfa_enrollment_started"
	EventEnrollmentRejected        EventType = "mfa_enrollment_rejected"
	EventEnrollmentConfirmed       EventType = "mfa_enrollment_confirmed"
	EventSessionRevoked            EventType = "session_revoked"
	EventAllSessionsRevoked        EventType = "all_sessions_revoked"
	EventOwnerCreated              EventType = "owner_created"
	EventPasswordReset             EventType = "password_reset"
	EventTOTPKeyRotated            EventType = "totp_key_rotated"
)

// ClientInfo identifies where a request came from, for sessions and the audit log.
type ClientInfo struct {
	IP        netip.Addr
	UserAgent string
}

// Event is one audit log entry. It never carries a credential. UserID is empty when
// a failed login names an unknown email.
type Event struct {
	UserID string
	Type   EventType
	Client ClientInfo
}
