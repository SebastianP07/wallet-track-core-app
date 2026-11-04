// Command admin is the operator's CLI. Passwords come from a no-echo prompt or stdin, never a
// flag, because flags end up in shell history and process listings.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/SebastianP07/wallet-track-core-app/internal/auth/adapters/crypto"
	"github.com/SebastianP07/wallet-track-core-app/internal/auth/adapters/postgres"
	"github.com/SebastianP07/wallet-track-core-app/internal/auth/application"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/platform"
)

const usage = `usage: admin <command>

  create-owner --email <email>    Create the owner and print a one-time MFA enrollment secret
  reset-password --email <email>  Set a new password, clear any lockout, revoke every session
  rotate-totp-key                 Re-encrypt TOTP secrets under TOTP_ENC_KEY

Passwords are read from a no-echo prompt, or from the first line of stdin when it is not a terminal.
To rotate, set the new key in TOTP_ENC_KEY and the old one in TOTP_ENC_KEY_PREVIOUS.`

const commandTimeout = 2 * time.Minute

func main() {
	if len(os.Args) < 2 || !knownCommand(os.Args[1]) {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "admin:", err)
		os.Exit(1)
	}
}

func knownCommand(command string) bool {
	switch command {
	case "create-owner", "reset-password", "rotate-totp-key":
		return true
	default:
		return false
	}
}

func run(command string, rest []string) error {
	cfg, err := platform.LoadAppConfig(os.Getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	pool, err := platform.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	keyring, err := crypto.NewKeyring(cfg.TOTPKey, cfg.PreviousTOTPKey)
	if err != nil {
		return fmt.Errorf("load TOTP keys: %w", err)
	}
	deps := application.Deps{
		Repo:   postgres.NewRepository(pool),
		Tx:     postgres.NewTxManager(pool),
		Clock:  crypto.Clock{},
		Hasher: crypto.NewHasher(cfg.PasswordPepper),
		TOTP:   crypto.TOTP{},
		Tokens: crypto.Tokens{},
		Cipher: keyring,
	}

	switch command {
	case "create-owner":
		return createOwner(ctx, deps, rest)
	case "reset-password":
		return resetPassword(ctx, deps, rest)
	default:
		return rotateTOTPKey(ctx, deps)
	}
}

func createOwner(ctx context.Context, deps application.Deps, args []string) error {
	email, err := parseEmail("create-owner", args)
	if err != nil {
		return err
	}
	password, err := readNewPassword()
	if err != nil {
		return err
	}
	grant, err := application.NewCreateOwner(deps).Execute(ctx, email, password)
	if err != nil {
		return err
	}
	fmt.Println("Owner created.")
	printEnrollmentGrant(grant)
	return nil
}

func resetPassword(ctx context.Context, deps application.Deps, args []string) error {
	email, err := parseEmail("reset-password", args)
	if err != nil {
		return err
	}
	password, err := readNewPassword()
	if err != nil {
		return err
	}
	grant, err := application.NewResetPassword(deps).Execute(ctx, email, password)
	if err != nil {
		return err
	}
	fmt.Println("Password changed. Every session was revoked.")
	if grant != nil {
		printEnrollmentGrant(*grant)
	}
	return nil
}

func rotateTOTPKey(ctx context.Context, deps application.Deps) error {
	rotated, err := application.NewRotateTOTPKey(deps).Execute(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("Re-encrypted %d TOTP secret(s). TOTP_ENC_KEY_PREVIOUS can be removed.\n", rotated)
	return nil
}

func printEnrollmentGrant(grant application.EnrollmentGrant) {
	fmt.Printf("MFA enrollment secret, shown only now, valid until %s:\n%s\n",
		grant.ExpiresAt.UTC().Format(time.RFC3339), grant.Secret)
}

func parseEmail(command string, args []string) (string, error) {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	email := flags.String("email", "", "owner email")
	if err := flags.Parse(args); err != nil {
		return "", err
	}
	if *email == "" {
		return "", errors.New("--email is required")
	}
	return *email, nil
}

func readNewPassword() (string, error) {
	fd := int(os.Stdin.Fd()) //nolint:gosec // a file descriptor always fits an int.
	if !term.IsTerminal(fd) {
		return readPasswordLine(os.Stdin)
	}
	first, err := promptPassword(fd, "New password: ")
	if err != nil {
		return "", err
	}
	second, err := promptPassword(fd, "Repeat password: ")
	if err != nil {
		return "", err
	}
	if first != second {
		return "", errors.New("passwords do not match")
	}
	return first, nil
}

func promptPassword(fd int, prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	password, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	return string(password), nil
}

func readPasswordLine(reader io.Reader) (string, error) {
	line, err := bufio.NewReader(reader).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read password: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}
