package platform_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SebastianP07/wallet-track-core-app/internal/shared/platform"
)

func TestHealthz(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		wantStatus int
		wantBody   string
	}{
		{name: "GET reports ok", method: http.MethodGet, wantStatus: http.StatusOK, wantBody: `{"status":"ok"}`},
		{name: "POST is not allowed", method: http.MethodPost, wantStatus: http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			platform.NewRouter().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), tt.method, "/healthz", nil))

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantBody == "" {
				return
			}
			if got := rec.Body.String(); got != tt.wantBody {
				t.Errorf("body = %q, want %q", got, tt.wantBody)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", got)
			}
		})
	}
}

// startServing runs platform.Serve on a free port with a handler that blocks
// until release is closed, and returns once a request is in flight. The status
// channel yields the response status, or 0 if the request failed.
func startServing(t *testing.T, shutdownTimeout time.Duration, release <-chan struct{}) (cancel context.CancelFunc, served <-chan error, status <-chan int) {
	t.Helper()

	listener, err := new(net.ListenConfig).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+listener.Addr().String(), nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	inFlight := make(chan struct{})
	server := platform.NewHTTPServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(inFlight)
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	serveErr := make(chan error, 1)
	go func() { serveErr <- platform.Serve(ctx, server, listener, shutdownTimeout) }()

	statuses := make(chan int, 1)
	go func() {
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			statuses <- 0
			return
		}
		if err := resp.Body.Close(); err != nil {
			statuses <- 0
			return
		}
		statuses <- resp.StatusCode
	}()

	<-inFlight
	return cancel, serveErr, statuses
}

func TestServeDrainsInFlightRequestsOnCancel(t *testing.T) {
	release := make(chan struct{})
	cancel, served, status := startServing(t, 5*time.Second, release)

	cancel()

	select {
	case err := <-served:
		t.Fatalf("Serve returned before the in-flight request finished: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(release)

	if got := <-status; got != http.StatusNoContent {
		t.Errorf("status = %d, want %d", got, http.StatusNoContent)
	}
	if err := <-served; err != nil {
		t.Errorf("Serve() error = %v, want nil after a clean drain", err)
	}
}

func TestServeReportsShutdownTimeout(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	cancel, served, _ := startServing(t, 10*time.Millisecond, release)

	cancel()

	if err := <-served; !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Serve() error = %v, want context.DeadlineExceeded", err)
	}
}

func TestServeReportsListenerFailure(t *testing.T) {
	listener, err := new(net.ListenConfig).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	server := platform.NewHTTPServer(platform.NewRouter())
	if err := platform.Serve(context.Background(), server, listener, time.Second); err == nil {
		t.Error("Serve() error = nil, want the listener failure")
	}
}
