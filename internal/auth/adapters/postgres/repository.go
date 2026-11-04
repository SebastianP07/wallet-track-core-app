// Package postgres implements the auth repository and transactions on PostgreSQL.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SebastianP07/wallet-track-core-app/internal/auth/adapters/postgres/sqlcgen"
	"github.com/SebastianP07/wallet-track-core-app/internal/auth/application"
	"github.com/SebastianP07/wallet-track-core-app/internal/auth/domain"
)

var (
	_ application.Repository = (*Repository)(nil)
	_ application.TxManager  = (*TxManager)(nil)
)

const userAgentMaxBytes = 512

type TxManager struct {
	pool *pgxpool.Pool
}

func NewTxManager(pool *pgxpool.Pool) *TxManager {
	return &TxManager{pool: pool}
}

func (m *TxManager) WithinTx(ctx context.Context, fn func(ctx context.Context, repo application.Repository) error) error {
	return pgx.BeginFunc(ctx, m.pool, func(tx pgx.Tx) error {
		return fn(ctx, &Repository{queries: sqlcgen.New(tx)})
	})
}

type Repository struct {
	queries *sqlcgen.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{queries: sqlcgen.New(pool)}
}

func (r *Repository) CountUsers(ctx context.Context) (int64, error) {
	return r.queries.CountUsers(ctx)
}

func (r *Repository) CreateUser(ctx context.Context, user application.NewUser) (domain.User, error) {
	row, err := r.queries.InsertUser(ctx, sqlcgen.InsertUserParams{
		Email:                       user.Email,
		PasswordHash:                user.PasswordHash,
		MfaEnrollmentTokenHash:      user.EnrollmentSecretHash,
		MfaEnrollmentTokenExpiresAt: timestamp(user.EnrollmentSecretExpiresAt),
	})
	if err != nil {
		return domain.User{}, err
	}
	return toUser(row), nil
}

func (r *Repository) FindUserByEmail(ctx context.Context, email string) (domain.User, error) {
	row, err := r.queries.GetUserByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.User{}, err
	}
	return toUser(row), nil
}

func (r *Repository) IncrementFailedAttempts(ctx context.Context, userID string) (int, error) {
	id, err := parseID(userID)
	if err != nil {
		return 0, err
	}
	attempts, err := r.queries.IncrementFailedAttempts(ctx, id)
	return int(attempts), err
}

func (r *Repository) SetLockedUntil(ctx context.Context, userID string, until time.Time) error {
	id, err := parseID(userID)
	if err != nil {
		return err
	}
	return r.queries.SetLockedUntil(ctx, sqlcgen.SetLockedUntilParams{ID: id, LockedUntil: timestamp(until)})
}

func (r *Repository) ResetFailedAttempts(ctx context.Context, userID string) error {
	id, err := parseID(userID)
	if err != nil {
		return err
	}
	return r.queries.ResetFailedAttempts(ctx, id)
}

func (r *Repository) UpdatePassword(ctx context.Context, userID, passwordHash string) error {
	id, err := parseID(userID)
	if err != nil {
		return err
	}
	return r.queries.UpdatePassword(ctx, sqlcgen.UpdatePasswordParams{ID: id, PasswordHash: passwordHash})
}

func (r *Repository) SetEnrollmentSecret(ctx context.Context, userID string, secretHash []byte, expiresAt time.Time) error {
	id, err := parseID(userID)
	if err != nil {
		return err
	}
	return r.queries.SetEnrollmentSecret(ctx, sqlcgen.SetEnrollmentSecretParams{
		ID:                          id,
		MfaEnrollmentTokenHash:      secretHash,
		MfaEnrollmentTokenExpiresAt: timestamp(expiresAt),
	})
}

func (r *Repository) SetUnconfirmedTOTPSecret(ctx context.Context, userID string, secret application.EncryptedSecret) (bool, error) {
	id, err := parseID(userID)
	if err != nil {
		return false, err
	}
	rows, err := r.queries.SetUnconfirmedTOTPSecret(ctx, sqlcgen.SetUnconfirmedTOTPSecretParams{
		ID:            id,
		TotpSecretEnc: secret.Ciphertext,
		TotpKeyID:     text(secret.KeyID),
	})
	return rows == 1, err
}

func (r *Repository) ActivateMFA(ctx context.Context, userID string, step int64, now time.Time) (bool, error) {
	id, err := parseID(userID)
	if err != nil {
		return false, err
	}
	rows, err := r.queries.ActivateMFA(ctx, sqlcgen.ActivateMFAParams{
		ID:   id,
		Step: pgtype.Int8{Int64: step, Valid: true},
		Now:  timestamp(now),
	})
	return rows == 1, err
}

func (r *Repository) ConsumeTOTPStep(ctx context.Context, userID string, step int64) (bool, error) {
	id, err := parseID(userID)
	if err != nil {
		return false, err
	}
	rows, err := r.queries.ConsumeTOTPStep(ctx, sqlcgen.ConsumeTOTPStepParams{
		ID:   id,
		Step: pgtype.Int8{Int64: step, Valid: true},
	})
	return rows == 1, err
}

func (r *Repository) ListTOTPSecretsForUpdate(ctx context.Context) ([]application.UserSecret, error) {
	rows, err := r.queries.ListUsersWithTOTPSecret(ctx)
	if err != nil {
		return nil, err
	}
	secrets := make([]application.UserSecret, 0, len(rows))
	for _, row := range rows {
		secrets = append(secrets, application.UserSecret{
			UserID: row.ID.String(),
			Secret: application.EncryptedSecret{Ciphertext: row.TotpSecretEnc, KeyID: row.TotpKeyID.String},
		})
	}
	return secrets, nil
}

func (r *Repository) ReplaceTOTPSecret(ctx context.Context, userID string, secret application.EncryptedSecret) error {
	id, err := parseID(userID)
	if err != nil {
		return err
	}
	return r.queries.ReplaceTOTPSecret(ctx, sqlcgen.ReplaceTOTPSecretParams{
		ID:            id,
		TotpSecretEnc: secret.Ciphertext,
		TotpKeyID:     text(secret.KeyID),
	})
}

func (r *Repository) CreateSession(ctx context.Context, session application.NewSession) error {
	id, err := parseID(session.UserID)
	if err != nil {
		return err
	}
	_, err = r.queries.InsertSession(ctx, sqlcgen.InsertSessionParams{
		UserID:     id,
		TokenHash:  session.TokenHash,
		UserAgent:  userAgent(session.Client.UserAgent),
		Ip:         address(session.Client.IP),
		LastSeenAt: timestamp(session.Now),
		ExpiresAt:  timestamp(session.ExpiresAt),
	})
	return err
}

func (r *Repository) FindSessionByTokenHash(ctx context.Context, tokenHash []byte) (domain.User, domain.Session, error) {
	row, err := r.queries.GetSessionAndUserByTokenHash(ctx, tokenHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.Session{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.User{}, domain.Session{}, err
	}
	return toUser(row.User), domain.Session{
		ID:            row.Session.ID.String(),
		UserID:        row.Session.UserID.String(),
		MFAVerifiedAt: row.Session.MfaVerifiedAt.Time,
		LastSeenAt:    row.Session.LastSeenAt.Time,
		ExpiresAt:     row.Session.ExpiresAt.Time,
		RevokedAt:     row.Session.RevokedAt.Time,
	}, nil
}

func (r *Repository) PromoteSession(ctx context.Context, promotion application.SessionPromotion) (bool, error) {
	id, err := parseID(promotion.SessionID)
	if err != nil {
		return false, err
	}
	rows, err := r.queries.PromoteSession(ctx, sqlcgen.PromoteSessionParams{
		ID:        id,
		TokenHash: promotion.TokenHash,
		Now:       timestamp(promotion.Now),
		ExpiresAt: timestamp(promotion.ExpiresAt),
	})
	return rows == 1, err
}

func (r *Repository) TouchSession(ctx context.Context, sessionID string, now time.Time) error {
	id, err := parseID(sessionID)
	if err != nil {
		return err
	}
	return r.queries.TouchSession(ctx, sqlcgen.TouchSessionParams{ID: id, LastSeenAt: timestamp(now)})
}

func (r *Repository) RevokeSession(ctx context.Context, sessionID string, now time.Time) error {
	id, err := parseID(sessionID)
	if err != nil {
		return err
	}
	return r.queries.RevokeSession(ctx, sqlcgen.RevokeSessionParams{ID: id, RevokedAt: timestamp(now)})
}

func (r *Repository) RevokeUserSessions(ctx context.Context, userID string, now time.Time) error {
	id, err := parseID(userID)
	if err != nil {
		return err
	}
	return r.queries.RevokeUserSessions(ctx, sqlcgen.RevokeUserSessionsParams{UserID: id, RevokedAt: timestamp(now)})
}

func (r *Repository) ReplaceRecoveryCodes(ctx context.Context, userID string, codeHashes []string) error {
	id, err := parseID(userID)
	if err != nil {
		return err
	}
	if err := r.queries.DeleteRecoveryCodes(ctx, id); err != nil {
		return err
	}
	for _, codeHash := range codeHashes {
		if err := r.queries.InsertRecoveryCode(ctx, sqlcgen.InsertRecoveryCodeParams{UserID: id, CodeHash: codeHash}); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) ConsumeRecoveryCode(ctx context.Context, userID, codeHash string, now time.Time) (bool, error) {
	id, err := parseID(userID)
	if err != nil {
		return false, err
	}
	rows, err := r.queries.ConsumeRecoveryCode(ctx, sqlcgen.ConsumeRecoveryCodeParams{
		UserID:   id,
		CodeHash: codeHash,
		UsedAt:   timestamp(now),
	})
	return rows == 1, err
}

func (r *Repository) CountUnusedRecoveryCodes(ctx context.Context, userID string) (int64, error) {
	id, err := parseID(userID)
	if err != nil {
		return 0, err
	}
	return r.queries.CountUnusedRecoveryCodes(ctx, id)
}

func (r *Repository) RecordEvent(ctx context.Context, event domain.Event) error {
	var userID pgtype.UUID
	if event.UserID != "" {
		id, err := parseID(event.UserID)
		if err != nil {
			return err
		}
		userID = id
	}
	return r.queries.InsertAuthEvent(ctx, sqlcgen.InsertAuthEventParams{
		UserID:    userID,
		EventType: string(event.Type),
		UserAgent: userAgent(event.Client.UserAgent),
		Ip:        address(event.Client.IP),
	})
}

func toUser(row sqlcgen.User) domain.User {
	return domain.User{
		ID:                        row.ID.String(),
		Email:                     row.Email,
		PasswordHash:              row.PasswordHash,
		TOTPSecretEnc:             row.TotpSecretEnc,
		TOTPKeyID:                 row.TotpKeyID.String,
		MFAEnabled:                row.MfaEnabled,
		EnrollmentSecretHash:      row.MfaEnrollmentTokenHash,
		EnrollmentSecretExpiresAt: row.MfaEnrollmentTokenExpiresAt.Time,
		LockedUntil:               row.LockedUntil.Time,
	}
}

func parseID(raw string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(raw); err != nil {
		return pgtype.UUID{}, fmt.Errorf("parse id: %w", err)
	}
	return id, nil
}

func timestamp(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: !t.IsZero()}
}

func text(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: s != ""}
}

func userAgent(raw string) pgtype.Text {
	if len(raw) > userAgentMaxBytes {
		raw = raw[:userAgentMaxBytes]
	}
	// PostgreSQL rejects invalid UTF-8, and this value comes straight from the client.
	return text(strings.ToValidUTF8(raw, "\uFFFD"))
}

func address(ip netip.Addr) *netip.Addr {
	if !ip.IsValid() {
		return nil
	}
	return &ip
}
