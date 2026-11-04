package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/SebastianP07/wallet-track-core-app/internal/auth/domain"
)

// lastSeenResolution keeps ordinary requests from writing to the database every time.
const lastSeenResolution = time.Minute

type AuthenticateSession struct {
	deps Deps
}

func NewAuthenticateSession(deps Deps) *AuthenticateSession {
	return &AuthenticateSession{deps: deps}
}

func (uc *AuthenticateSession) Execute(ctx context.Context, token string) (Principal, error) {
	user, session, err := uc.deps.Repo.FindSessionByTokenHash(ctx, domain.HashToken(token))
	if errors.Is(err, domain.ErrNotFound) {
		return Principal{}, domain.ErrInvalidCredentials
	}
	if err != nil {
		return Principal{}, fmt.Errorf("find session: %w", err)
	}
	now := uc.deps.Clock.Now()
	if !session.ValidAt(now) {
		return Principal{}, domain.ErrInvalidCredentials
	}
	if now.Sub(session.LastSeenAt) >= lastSeenResolution {
		if err := uc.deps.Repo.TouchSession(ctx, session.ID, now); err != nil {
			return Principal{}, fmt.Errorf("touch session: %w", err)
		}
	}
	return Principal{User: user, Session: session}, nil
}

type Logout struct {
	deps Deps
}

func NewLogout(deps Deps) *Logout {
	return &Logout{deps: deps}
}

func (uc *Logout) Execute(ctx context.Context, principal Principal, client domain.ClientInfo) error {
	err := uc.deps.Tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		if err := repo.RevokeSession(ctx, principal.Session.ID, uc.deps.Clock.Now()); err != nil {
			return err
		}
		return repo.RecordEvent(ctx, domain.Event{UserID: principal.User.ID, Type: domain.EventSessionRevoked, Client: client})
	})
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

type LogoutAll struct {
	deps Deps
}

func NewLogoutAll(deps Deps) *LogoutAll {
	return &LogoutAll{deps: deps}
}

func (uc *LogoutAll) Execute(ctx context.Context, principal Principal, client domain.ClientInfo) error {
	err := uc.deps.Tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		if err := repo.RevokeUserSessions(ctx, principal.User.ID, uc.deps.Clock.Now()); err != nil {
			return err
		}
		return repo.RecordEvent(ctx, domain.Event{UserID: principal.User.ID, Type: domain.EventAllSessionsRevoked, Client: client})
	})
	if err != nil {
		return fmt.Errorf("revoke sessions: %w", err)
	}
	return nil
}
