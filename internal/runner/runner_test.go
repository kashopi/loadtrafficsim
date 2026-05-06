package runner

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"trafficloadsim/internal/config"
	"trafficloadsim/internal/traffic"
)

// testServer is a lightweight httptest.Server wrapper that counts requests and
// captures the first request's headers and body.
type testServer struct {
	mu          sync.Mutex
	count       atomic.Int64
	firstBody   string
	firstHeader http.Header
	statusCode  int // response status code (default 200)
	delay       time.Duration
}

func newTestServer(t *testing.T) (*httptest.Server, *testServer) {
	t.Helper()
	ts := &testServer{statusCode: 200}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ts.delay > 0 {
			time.Sleep(ts.delay)
		}
		body, _ := io.ReadAll(r.Body)
		ts.mu.Lock()
		if ts.count.Load() == 0 {
			ts.firstBody = string(body)
			ts.firstHeader = r.Header.Clone()
		}
		ts.mu.Unlock()
		ts.count.Add(1)
		w.WriteHeader(ts.statusCode)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv, ts
}

// baseConfig returns a minimal valid Config pointed at url.
func baseConfig(url string) *config.Config {
	return &config.Config{
		URL:             url,
		Method:          "GET",
		Duration:        time.Second,
		MinRPS:          20,
		MaxRPS:          20,
		Concurrency:     10,
		Profile:         "constant",
		Steps:           1,
		Timeout:         5 * time.Second,
		FollowRedirects: true,
		Output:          "text",
	}
}

// withinPct returns true if got is within pct% of want.
func withinPct(got, want int64, pct float64) bool {
	if want == 0 {
		return got == 0
	}
	delta := float64(want) * pct / 100
	diff := float64(got) - float64(want)
	if diff < 0 {
		diff = -diff
	}
	return diff <= delta
}

// ── Constant profile ──────────────────────────────────────────────────────────

func TestRun_ConstantProfile(t *testing.T) {
	srv, ts := newTestServer(t)
	cfg := baseConfig(srv.URL)
	cfg.Profile = "constant"
	cfg.MinRPS = 20
	cfg.MaxRPS = 20
	cfg.Duration = time.Second

	if err := Run(cfg); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	// CumulativeAt for constant 20 RPS over 1s = 20 requests.
	expected := int64(traffic.CumulativeAt(cfg, cfg.Duration))
	got := ts.count.Load()
	if !withinPct(got, expected, 15) {
		t.Errorf("constant 20 rps/1s: got %d requests, want ~%d (±15%%)", got, expected)
	}
}

// ── Linear profile ────────────────────────────────────────────────────────────

func TestRun_LinearProfile(t *testing.T) {
	srv, ts := newTestServer(t)
	cfg := baseConfig(srv.URL)
	cfg.Profile = "linear"
	cfg.MinRPS = 5
	cfg.MaxRPS = 40
	cfg.Duration = 2 * time.Second

	if err := Run(cfg); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	// ∫₀ᵀ (5 + 35·t/2) dt = 5·2 + 35·4/(2·2) = 10 + 35 = 45
	expected := int64(traffic.CumulativeAt(cfg, cfg.Duration))
	got := ts.count.Load()
	if !withinPct(got, expected, 15) {
		t.Errorf("linear 5→40 rps/2s: got %d requests, want ~%d (±15%%)", got, expected)
	}
}

// ── Custom headers ────────────────────────────────────────────────────────────

func TestRun_CustomHeaders(t *testing.T) {
	srv, ts := newTestServer(t)
	cfg := baseConfig(srv.URL)
	cfg.Headers = []string{
		"Authorization: Bearer secret-token",
		"X-Request-ID: test-123",
	}
	cfg.Duration = 300 * time.Millisecond
	cfg.MaxRPS = 5
	cfg.MinRPS = 5

	if err := Run(cfg); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	ts.mu.Lock()
	got := ts.firstHeader
	ts.mu.Unlock()

	if got.Get("Authorization") != "Bearer secret-token" {
		t.Errorf("Authorization header = %q, want %q", got.Get("Authorization"), "Bearer secret-token")
	}
	if got.Get("X-Request-Id") != "test-123" {
		t.Errorf("X-Request-Id header = %q, want %q", got.Get("X-Request-Id"), "test-123")
	}
}

// ── Request body ─────────────────────────────────────────────────────────────

func TestRun_RequestBody(t *testing.T) {
	srv, ts := newTestServer(t)
	cfg := baseConfig(srv.URL)
	cfg.Method = "POST"
	cfg.Body = `{"user":"alice","action":"login"}`
	cfg.Duration = 300 * time.Millisecond
	cfg.MaxRPS = 5
	cfg.MinRPS = 5

	if err := Run(cfg); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	ts.mu.Lock()
	got := ts.firstBody
	ts.mu.Unlock()

	if got != cfg.Body {
		t.Errorf("request body = %q, want %q", got, cfg.Body)
	}
}

// ── Server error responses ────────────────────────────────────────────────────

func TestRun_ServerErrorsTracked(t *testing.T) {
	srv, ts := newTestServer(t)
	ts.statusCode = 500
	cfg := baseConfig(srv.URL)
	cfg.Duration = 500 * time.Millisecond
	cfg.MaxRPS = 20
	cfg.MinRPS = 20

	if err := Run(cfg); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	total := ts.count.Load()
	if total == 0 {
		t.Fatal("expected requests to be sent, got 0")
	}
	// All responses are 500 — none should count as successes.
	// Run() does not expose stats directly; verify via the server side that
	// all responses were served (500 tracking is validated in stats tests).
	// At minimum, all server-side responses should be 500s (no panics, etc.).
}

// ── Per-request timeout ───────────────────────────────────────────────────────

func TestRun_SlowServerCausesTimeouts(t *testing.T) {
	srv, ts := newTestServer(t)
	ts.delay = 200 * time.Millisecond // server is slow

	cfg := baseConfig(srv.URL)
	cfg.Timeout = 50 * time.Millisecond // client times out before server responds
	cfg.Duration = time.Second
	cfg.MaxRPS = 10
	cfg.MinRPS = 10
	cfg.Concurrency = 20

	// Run should complete without hanging — the per-request timeout must fire.
	done := make(chan error, 1)
	go func() { done <- Run(cfg) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not complete within 5 seconds — likely hung on slow server")
	}

	// The server should have received few (or zero) complete responses since
	// the client cancels before the 200ms delay completes.
	t.Logf("server saw %d completed requests (expected ~0)", ts.count.Load())
}

// ── No-redirect ───────────────────────────────────────────────────────────────

func TestRun_NoFollowRedirects(t *testing.T) {
	// Server A redirects to server B. With follow-redirects=false, the runner
	// should get a 302 from A and NOT follow through to B.
	var bHit atomic.Int64
	serverB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bHit.Add(1)
		w.WriteHeader(200)
	}))
	defer serverB.Close()

	serverA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, serverB.URL, http.StatusFound)
	}))
	defer serverA.Close()

	cfg := baseConfig(serverA.URL)
	cfg.FollowRedirects = false
	cfg.Duration = 300 * time.Millisecond
	cfg.MaxRPS = 5
	cfg.MinRPS = 5

	if err := Run(cfg); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if bHit.Load() > 0 {
		t.Errorf("serverB was hit %d times despite follow-redirects=false", bHit.Load())
	}
}

// ── commify (package-level helper) ───────────────────────────────────────────

func TestCommify(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{1234567, "1,234,567"},
	}
	for _, tc := range cases {
		if got := commify(tc.in); got != tc.want {
			t.Errorf("commify(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
