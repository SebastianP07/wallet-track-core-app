// Package passkey adapts the WebAuthn library to the auth module's PasskeyVerifier port. No other
// package imports the library.
package passkey

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/SebastianP07/wallet-track-core-app/internal/auth/application"
	"github.com/SebastianP07/wallet-track-core-app/internal/auth/domain"
)

var _ application.PasskeyVerifier = (*Verifier)(nil)

const relyingPartyName = "Wallet Track"

var knownTransports = []protocol.AuthenticatorTransport{
	protocol.USB, protocol.NFC, protocol.BLE, protocol.SmartCard, protocol.Hybrid, protocol.Internal,
}

type Verifier struct {
	relyingParty *webauthn.WebAuthn
	logger       *slog.Logger
}

// NewVerifier requires user verification on every ceremony and asks for no attestation: a passkey
// here is a second factor the owner chose, so nothing depends on which model of device holds it.
func NewVerifier(rpID string, rpOrigins []string, logger *slog.Logger) (*Verifier, error) {
	timeout := webauthn.TimeoutConfig{
		Enforce:    true,
		Timeout:    domain.PasskeyChallengeLifetime,
		TimeoutUVD: domain.PasskeyChallengeLifetime,
	}
	relyingParty, err := webauthn.New(&webauthn.Config{
		RPID:                  rpID,
		RPDisplayName:         relyingPartyName,
		RPOrigins:             rpOrigins,
		AttestationPreference: protocol.PreferNoAttestation,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementPreferred,
			UserVerification: protocol.VerificationRequired,
		},
		Timeouts: webauthn.TimeoutsConfig{Login: timeout, Registration: timeout},
	})
	if err != nil {
		return nil, fmt.Errorf("configure WebAuthn: %w", err)
	}
	return &Verifier{relyingParty: relyingParty, logger: logger}, nil
}

func (v *Verifier) BeginLogin(user domain.User, passkeys []domain.Passkey) (application.PasskeyCeremony, error) {
	assertion, session, err := v.relyingParty.BeginLogin(newOwner(user, passkeys), webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return application.PasskeyCeremony{}, err
	}
	return newCeremony(assertion, session)
}

func (v *Verifier) FinishLogin(user domain.User, passkeys []domain.Passkey, state, response []byte) (application.PasskeyAssertion, error) {
	session, err := decodeState(state)
	if err != nil {
		return application.PasskeyAssertion{}, err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(response)
	if err != nil {
		return application.PasskeyAssertion{}, v.rejected("login", err)
	}
	credential, err := v.relyingParty.ValidateLogin(newOwner(user, passkeys), session, parsed)
	if err != nil {
		return application.PasskeyAssertion{}, v.rejected("login", err)
	}
	index := slices.IndexFunc(passkeys, func(passkey domain.Passkey) bool {
		return bytes.Equal(passkey.CredentialID, credential.ID)
	})
	if index < 0 {
		return application.PasskeyAssertion{}, errors.New("validated a credential that is not among the owner's")
	}
	return application.PasskeyAssertion{
		PasskeyID:          passkeys[index].ID,
		SignCount:          credential.Authenticator.SignCount,
		BackupState:        credential.Flags.BackupState,
		SignCountRegressed: credential.Authenticator.CloneWarning,
	}, nil
}

func (v *Verifier) BeginRegistration(user domain.User, passkeys []domain.Passkey) (application.PasskeyCeremony, error) {
	owner := newOwner(user, passkeys)
	exclusions := make([]protocol.CredentialDescriptor, 0, len(owner.credentials))
	for _, credential := range owner.credentials {
		exclusions = append(exclusions, credential.Descriptor())
	}
	creation, session, err := v.relyingParty.BeginRegistration(owner,
		webauthn.WithExclusions(exclusions),
		webauthn.WithCredentialParameters(webauthn.CredentialParametersRecommendedL3()),
	)
	if err != nil {
		return application.PasskeyCeremony{}, err
	}
	return newCeremony(creation, session)
}

func (v *Verifier) FinishRegistration(user domain.User, state, response []byte) (application.NewPasskeyCredential, error) {
	session, err := decodeState(state)
	if err != nil {
		return application.NewPasskeyCredential{}, err
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(response)
	if err != nil {
		return application.NewPasskeyCredential{}, v.rejected("registration", err)
	}
	credential, err := v.relyingParty.CreateCredential(newOwner(user, nil), session, parsed)
	if err != nil {
		return application.NewPasskeyCredential{}, v.rejected("registration", err)
	}
	return application.NewPasskeyCredential{
		CredentialID:   credential.ID,
		PublicKey:      credential.PublicKey,
		Transports:     transportNames(credential.Transport),
		SignCount:      credential.Authenticator.SignCount,
		BackupEligible: credential.Flags.BackupEligible,
		BackupState:    credential.Flags.BackupState,
	}, nil
}

// rejected logs why, in the library's fixed wording only: its debug detail repeats what the client
// sent, and a credential ID has no place in a log.
func (v *Verifier) rejected(ceremony string, err error) error {
	reason := "unreadable response"
	var protocolError *protocol.Error
	if errors.As(err, &protocolError) {
		reason = protocolError.Type + ": " + protocolError.Details
	}
	v.logger.LogAttrs(context.Background(), slog.LevelWarn, "passkey rejected",
		slog.String("ceremony", ceremony),
		slog.String("reason", reason),
	)
	return domain.ErrInvalidCredentials
}

func newCeremony(options any, session *webauthn.SessionData) (application.PasskeyCeremony, error) {
	encodedOptions, err := json.Marshal(options)
	if err != nil {
		return application.PasskeyCeremony{}, fmt.Errorf("encode ceremony options: %w", err)
	}
	state, err := json.Marshal(session)
	if err != nil {
		return application.PasskeyCeremony{}, fmt.Errorf("encode ceremony state: %w", err)
	}
	return application.PasskeyCeremony{Options: encodedOptions, State: state}, nil
}

func decodeState(state []byte) (webauthn.SessionData, error) {
	var session webauthn.SessionData
	if err := json.Unmarshal(state, &session); err != nil {
		return webauthn.SessionData{}, fmt.Errorf("decode ceremony state: %w", err)
	}
	return session, nil
}

func transportNames(transports []protocol.AuthenticatorTransport) []string {
	names := make([]string, 0, len(transports))
	for _, transport := range transports {
		if slices.Contains(knownTransports, transport) {
			names = append(names, string(transport))
		}
	}
	return names
}

// owner is the library's view of the user. The handle is the user's random UUID, which holds nothing
// personal and never changes.
type owner struct {
	user        domain.User
	credentials []webauthn.Credential
}

func newOwner(user domain.User, passkeys []domain.Passkey) owner {
	credentials := make([]webauthn.Credential, 0, len(passkeys))
	for _, passkey := range passkeys {
		transports := make([]protocol.AuthenticatorTransport, 0, len(passkey.Transports))
		for _, transport := range passkey.Transports {
			transports = append(transports, protocol.AuthenticatorTransport(transport))
		}
		credentials = append(credentials, webauthn.Credential{
			ID:        passkey.CredentialID,
			PublicKey: passkey.PublicKey,
			Transport: transports,
			Flags: webauthn.CredentialFlags{
				UserVerified:   true,
				BackupEligible: passkey.BackupEligible,
				BackupState:    passkey.BackupState,
			},
			Authenticator: webauthn.Authenticator{SignCount: passkey.SignCount},
		})
	}
	return owner{user: user, credentials: credentials}
}

func (o owner) WebAuthnID() []byte {
	return []byte(o.user.ID)
}

func (o owner) WebAuthnName() string {
	return o.user.Email
}

func (o owner) WebAuthnDisplayName() string {
	return o.user.Email
}

func (o owner) WebAuthnCredentials() []webauthn.Credential {
	return o.credentials
}
