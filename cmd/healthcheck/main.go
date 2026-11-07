// Command healthcheck probes the API's /healthz from inside its container, whose distroless image has
// no shell, curl or wget. It exits 0 only on a 200, so a container healthcheck can run it directly.
// The URL may be given as the only argument, but only on loopback; otherwise it is derived from HTTP_ADDR.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/SebastianP07/wallet-track-core-app/internal/shared/platform"
)

const probeTimeout = 3 * time.Second

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		os.Exit(1)
	}
}

func run() error {
	target, err := targetURL(os.Args[1:])
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	// targetURL only lets through plain HTTP to a loopback address, the container's own server.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, http.NoBody) //nolint:gosec // Loopback only, checked above.
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req) //nolint:gosec // Loopback only, checked above.
	if err != nil {
		return err
	}
	// Only the status matters; the process exits right after.
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

func targetURL(args []string) (string, error) {
	switch len(args) {
	case 0:
		cfg, err := platform.LoadConfig(os.Getenv)
		if err != nil {
			return "", err
		}
		return healthzURL(cfg.HTTPAddr)
	case 1:
		return requireLoopback(args[0])
	default:
		return "", errors.New("usage: healthcheck [url]")
	}
}

func requireLoopback(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("not a valid URL")
	}
	ip := net.ParseIP(parsed.Hostname())
	if parsed.Scheme != "http" || ip == nil || !ip.IsLoopback() {
		return "", errors.New("only http:// to a loopback address is probed")
	}
	return parsed.String(), nil
}

// healthzURL reaches the server on loopback when HTTP_ADDR listens on every interface, as ":8080" does.
func healthzURL(httpAddr string) (string, error) {
	host, port, err := net.SplitHostPort(httpAddr)
	if err != nil {
		return "", fmt.Errorf("HTTP_ADDR: %w", err)
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/healthz", nil
}
