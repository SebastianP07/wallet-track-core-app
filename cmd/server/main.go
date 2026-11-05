// Command server is the composition root of the Wallet Track API.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	accountshttp "github.com/SebastianP07/wallet-track-core-app/internal/accounts/adapters/http"
	accountspostgres "github.com/SebastianP07/wallet-track-core-app/internal/accounts/adapters/postgres"
	accountsapplication "github.com/SebastianP07/wallet-track-core-app/internal/accounts/application"
	"github.com/SebastianP07/wallet-track-core-app/internal/auth/adapters/crypto"
	authhttp "github.com/SebastianP07/wallet-track-core-app/internal/auth/adapters/http"
	"github.com/SebastianP07/wallet-track-core-app/internal/auth/adapters/memory"
	"github.com/SebastianP07/wallet-track-core-app/internal/auth/adapters/postgres"
	"github.com/SebastianP07/wallet-track-core-app/internal/auth/application"
	cardshttp "github.com/SebastianP07/wallet-track-core-app/internal/cards/adapters/http"
	cardspostgres "github.com/SebastianP07/wallet-track-core-app/internal/cards/adapters/postgres"
	cardsapplication "github.com/SebastianP07/wallet-track-core-app/internal/cards/application"
	expenseshttp "github.com/SebastianP07/wallet-track-core-app/internal/expenses/adapters/http"
	expensespostgres "github.com/SebastianP07/wallet-track-core-app/internal/expenses/adapters/postgres"
	expensesapplication "github.com/SebastianP07/wallet-track-core-app/internal/expenses/application"
	periodshttp "github.com/SebastianP07/wallet-track-core-app/internal/periods/adapters/http"
	periodspostgres "github.com/SebastianP07/wallet-track-core-app/internal/periods/adapters/postgres"
	periodsapplication "github.com/SebastianP07/wallet-track-core-app/internal/periods/application"
	periodsdomain "github.com/SebastianP07/wallet-track-core-app/internal/periods/domain"
	platformshttp "github.com/SebastianP07/wallet-track-core-app/internal/platforms/adapters/http"
	platformspostgres "github.com/SebastianP07/wallet-track-core-app/internal/platforms/adapters/postgres"
	platformsapplication "github.com/SebastianP07/wallet-track-core-app/internal/platforms/application"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/platform"
	tagshttp "github.com/SebastianP07/wallet-track-core-app/internal/tags/adapters/http"
	tagspostgres "github.com/SebastianP07/wallet-track-core-app/internal/tags/adapters/postgres"
	tagsapplication "github.com/SebastianP07/wallet-track-core-app/internal/tags/application"
)

const (
	credentialsBurstPerIP      = 20
	credentialsRefillPerIP     = 6 * time.Second
	csrfBurstPerIP             = 60
	csrfRefillPerIP            = time.Second
	credentialsBurstPerAccount = 10
	credentialsRefillPerAcct   = 30 * time.Second
)

func main() {
	if err := run(); err != nil {
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("server stopped", "error", err.Error())
		os.Exit(1)
	}
}

func run() error {
	cfg, err := platform.LoadConfig(os.Getenv)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	appCfg, err := platform.LoadAppConfig(os.Getenv)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	edgeCfg, err := platform.LoadEdgeConfig(os.Getenv)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger := platform.NewLogger(os.Stdout, cfg.LogLevel)
	if edgeCfg.InsecureDevCookies {
		if err := requireLocalOrigins(edgeCfg.AllowedOrigins); err != nil {
			return err
		}
		logger.Warn("INSECURE_DEV_COOKIES is on: cookies lose Secure and the __Host- prefix; never use this outside local development")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := platform.NewPool(ctx, appCfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	authHandler, err := newAuthHandler(pool, appCfg, edgeCfg, logger)
	if err != nil {
		return err
	}

	listener, err := new(net.ListenConfig).Listen(ctx, "tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.HTTPAddr, err)
	}

	router := platform.Chain(
		platform.NewRouter(
			authHandler.Register,
			newAccountsHandler(pool, authHandler, logger).Register,
			newPeriodsHandler(pool, authHandler, logger).Register,
			newCardsHandler(pool, authHandler, logger).Register,
			newExpensesHandler(pool, authHandler, logger).Register,
			newTagsHandler(pool, authHandler, logger).Register,
			newPlatformsHandler(pool, authHandler, logger).Register,
			newDashboardHandler(pool, authHandler, logger).Register,
		),
		platform.RequestID,
		platform.LogRequests(logger),
		platform.SecurityHeaders,
	)
	server := platform.NewHTTPServer(router)
	logger.Info("server started", "addr", listener.Addr().String())
	if err := platform.Serve(ctx, server, listener, cfg.ShutdownTimeout); err != nil {
		return err
	}
	logger.Info("server stopped")
	return nil
}

// requireLocalOrigins stops a deployed server from running with dev cookies by accident: a public
// origin in the list is the sign that this is not a local machine.
func requireLocalOrigins(origins []string) error {
	for _, origin := range origins {
		parsed, err := url.Parse(origin)
		if err != nil || !isLocalHost(parsed.Hostname()) {
			return fmt.Errorf("INSECURE_DEV_COOKIES=true is for local development only, but ALLOWED_ORIGINS has the non-local origin %q", origin)
		}
	}
	return nil
}

func isLocalHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func newAuthHandler(pool *pgxpool.Pool, appCfg platform.AppConfig, edgeCfg platform.EdgeConfig, logger *slog.Logger) (*authhttp.Handler, error) {
	keyring, err := crypto.NewKeyring(appCfg.TOTPKey, appCfg.PreviousTOTPKey)
	if err != nil {
		return nil, fmt.Errorf("load TOTP keys: %w", err)
	}
	deps := application.Deps{
		Repo:     postgres.NewRepository(pool),
		Tx:       postgres.NewTxManager(pool),
		Clock:    crypto.Clock{},
		Hasher:   crypto.NewHasher(appCfg.PasswordPepper),
		TOTP:     crypto.TOTP{},
		Tokens:   crypto.Tokens{},
		Cipher:   keyring,
		Attempts: memory.NewPasswordAttempts(),
	}
	loginPassword, err := application.NewLoginWithPassword(deps)
	if err != nil {
		return nil, err
	}

	cookies := platform.NewCookiePolicy(edgeCfg.InsecureDevCookies)
	return authhttp.NewHandler(authhttp.Config{
		UseCases: authhttp.UseCases{
			Authenticate:    application.NewAuthenticateSession(deps),
			LoginPassword:   loginPassword,
			LoginTOTP:       application.NewLoginWithTOTP(deps),
			LoginRecovery:   application.NewLoginWithRecoveryCode(deps),
			StartEnrollment: application.NewStartMFAEnrollment(deps),
			ConfirmEnroll:   application.NewConfirmMFAEnrollment(deps),
			RecoveryStatus:  application.NewGetRecoveryCodeStatus(deps),
			RegenerateCodes: application.NewRegenerateRecoveryCodes(deps),
			Logout:          application.NewLogout(deps),
			LogoutAll:       application.NewLogoutAll(deps),
		},
		Limits: authhttp.Limits{
			CredentialsByIP: platform.NewTokenBucket(credentialsBurstPerIP, credentialsRefillPerIP),
			CSRFByIP:        platform.NewTokenBucket(csrfBurstPerIP, csrfRefillPerIP),
			ByAccount:       platform.NewTokenBucket(credentialsBurstPerAccount, credentialsRefillPerAcct),
		},
		Cookies:  cookies,
		CSRF:     platform.NewCSRF(cookies, edgeCfg.AllowedOrigins),
		ClientIP: platform.NewClientIPResolver(edgeCfg.TrustedProxies),
		Logger:   logger,
	}), nil
}

func newAccountsHandler(pool *pgxpool.Pool, auth *authhttp.Handler, logger *slog.Logger) *accountshttp.Handler {
	repo := accountspostgres.NewRepository(pool)
	tx := accountspostgres.NewTxManager(pool)
	return accountshttp.NewHandler(accountshttp.Config{
		UseCases: accountshttp.UseCases{
			List:   accountsapplication.NewListAccounts(repo),
			Get:    accountsapplication.NewGetAccount(repo),
			Create: accountsapplication.NewCreateAccount(tx),
			Update: accountsapplication.NewUpdateAccount(tx),
			Delete: accountsapplication.NewDeleteAccount(tx),
		},
		RequireSession: auth.RequireFullSession,
		RequireCSRF:    auth.CSRF.Protect,
		Logger:         logger,
	})
}

func newPeriodsHandler(pool *pgxpool.Pool, auth *authhttp.Handler, logger *slog.Logger) *periodshttp.Handler {
	repo := periodspostgres.NewRepository(pool)
	tx := periodspostgres.NewTxManager(pool, memberIncomeInTx)
	return periodshttp.NewHandler(periodshttp.Config{
		UseCases: periodshttp.UseCases{
			ListPeriods:    periodsapplication.NewListPeriods(repo),
			GetPeriod:      periodsapplication.NewGetPeriod(repo),
			OpenPeriod:     periodsapplication.NewOpenPeriod(tx),
			ClosePeriod:    periodsapplication.NewClosePeriod(tx),
			ListTemplates:  periodsapplication.NewListTemplates(repo),
			GetTemplate:    periodsapplication.NewGetTemplate(repo),
			CreateTemplate: periodsapplication.NewCreateTemplate(repo),
			UpdateTemplate: periodsapplication.NewUpdateTemplate(tx),
			DeleteTemplate: periodsapplication.NewDeleteTemplate(repo),
		},
		RequireSession: auth.RequireFullSession,
		RequireCSRF:    auth.CSRF.Protect,
		Logger:         logger,
	})
}

func newCardsHandler(pool *pgxpool.Pool, auth *authhttp.Handler, logger *slog.Logger) *cardshttp.Handler {
	repo := cardspostgres.NewRepository(pool)
	return cardshttp.NewHandler(cardshttp.Config{
		UseCases: cardshttp.UseCases{
			List:   cardsapplication.NewListCards(repo),
			Get:    cardsapplication.NewGetCard(repo),
			Create: cardsapplication.NewCreateCard(repo),
			Update: cardsapplication.NewUpdateCard(repo),
			Delete: cardsapplication.NewDeleteCard(repo),
			Totals: cardsapplication.NewGetCardTotals(repo),
		},
		RequireSession: auth.RequireFullSession,
		RequireCSRF:    auth.CSRF.Protect,
		Logger:         logger,
	})
}

func newExpensesHandler(pool *pgxpool.Pool, auth *authhttp.Handler, logger *slog.Logger) *expenseshttp.Handler {
	repo := expensespostgres.NewRepository(pool)
	tx := expensespostgres.NewTxManager(pool, memberPaymentsInTx)
	clock := platform.SystemClock{}
	return expenseshttp.NewHandler(expenseshttp.Config{
		UseCases: expenseshttp.UseCases{
			List:    expensesapplication.NewListItems(repo, clock),
			Get:     expensesapplication.NewGetItem(repo, clock),
			Create:  expensesapplication.NewCreateItem(tx, clock),
			Update:  expensesapplication.NewUpdateItem(tx, clock),
			Delete:  expensesapplication.NewDeleteItem(tx),
			Done:    expensesapplication.NewMarkItemDone(tx, clock),
			Undone:  expensesapplication.NewUnmarkItemDone(tx, clock),
			Overdue: expensesapplication.NewListOverdueItems(repo, clock),
		},
		RequireSession: auth.RequireFullSession,
		RequireCSRF:    auth.CSRF.Protect,
		Logger:         logger,
	})
}

func newTagsHandler(pool *pgxpool.Pool, auth *authhttp.Handler, logger *slog.Logger) *tagshttp.Handler {
	repo := tagspostgres.NewRepository(pool)
	return tagshttp.NewHandler(tagshttp.Config{
		UseCases: tagshttp.UseCases{
			List:   tagsapplication.NewListTags(repo),
			Create: tagsapplication.NewCreateTag(repo),
			Update: tagsapplication.NewUpdateTag(repo),
			Delete: tagsapplication.NewDeleteTag(repo),
		},
		RequireSession: auth.RequireFullSession,
		RequireCSRF:    auth.CSRF.Protect,
		Logger:         logger,
	})
}

func newPlatformsHandler(pool *pgxpool.Pool, auth *authhttp.Handler, logger *slog.Logger) *platformshttp.Handler {
	repo := platformspostgres.NewRepository(pool)
	tx := platformspostgres.NewTxManager(pool, incomeItemsInTx)
	return platformshttp.NewHandler(platformshttp.Config{
		UseCases: platformshttp.UseCases{
			ListPeople:     platformsapplication.NewListPeople(repo),
			GetPerson:      platformsapplication.NewGetPerson(repo),
			CreatePerson:   platformsapplication.NewCreatePerson(tx),
			UpdatePerson:   platformsapplication.NewUpdatePerson(tx),
			DeletePerson:   platformsapplication.NewDeletePerson(tx),
			ListPayments:   platformsapplication.NewListPersonPayments(repo),
			RecordPayment:  platformsapplication.NewRecordPayment(tx, platform.SystemClock{}),
			DeletePayment:  platformsapplication.NewDeletePayment(tx),
			ListPlatforms:  platformsapplication.NewListPlatforms(repo),
			GetPlatform:    platformsapplication.NewGetPlatform(repo),
			CreatePlatform: platformsapplication.NewCreatePlatform(tx),
			UpdatePlatform: platformsapplication.NewUpdatePlatform(tx),
			DeletePlatform: platformsapplication.NewDeletePlatform(tx),
			AddMember:      platformsapplication.NewAddMember(tx),
			RemoveMember:   platformsapplication.NewRemoveMember(tx),
		},
		RequireSession: auth.RequireFullSession,
		RequireCSRF:    auth.CSRF.Protect,
		Logger:         logger,
	})
}

// The ports between modules, each built on the caller's transaction, so that what crosses a module
// boundary commits or rolls back with the rest. None calls back across a boundary.

func memberIncomeInTx(tx pgx.Tx) periodsapplication.MemberIncome {
	return memberIncome{platformsapplication.NewMemberIncome(platformspostgres.NewRepositoryInTx(tx))}
}

func memberPaymentsInTx(tx pgx.Tx) expensesapplication.MemberPayments {
	return platformsapplication.NewItemPayments(platformspostgres.NewRepositoryInTx(tx), platform.SystemClock{})
}

func incomeItemsInTx(tx pgx.Tx) platformsapplication.IncomeItems {
	return expensesapplication.NewMemberItems(expensespostgres.NewRepositoryInTx(tx))
}

// memberIncome carries the answer of one module in the types of the other, which it cannot import.
type memberIncome struct {
	source *platformsapplication.MemberIncome
}

func (m memberIncome) Owing(ctx context.Context, month kernel.YearMonth) ([]periodsdomain.OwingMember, error) {
	owing, err := m.source.Owing(ctx, month)
	if err != nil {
		return nil, err
	}
	members := make([]periodsdomain.OwingMember, 0, len(owing))
	for _, member := range owing {
		members = append(members, periodsdomain.OwingMember(member))
	}
	return members, nil
}

var _ http.Handler = http.NewServeMux()
