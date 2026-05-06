package runner

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"trafficloadsim/internal/config"
	"trafficloadsim/internal/report"
	"trafficloadsim/internal/stats"
	"trafficloadsim/internal/traffic"
)

// Run executes the load test described by cfg.
func Run(cfg *config.Config) error {
	printBanner(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Duration)
	defer cancel()

	// Honour Ctrl-C.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-sigCh:
			fmt.Fprintln(os.Stderr, "\r\033[K  interrupted — waiting for in-flight requests…")
			cancel()
		case <-ctx.Done():
		}
	}()

	client := buildClient(cfg)
	collector := stats.NewCollector()

	// requestCh: rate controller → workers. Buffer absorbs small bursts.
	requestCh := make(chan struct{}, cfg.Concurrency)
	// resultCh: workers → collector. Buffer prevents workers blocking on sends.
	resultCh := make(chan stats.Result, cfg.Concurrency*2)

	start := time.Now()

	// Launch worker pool.
	var workerWg sync.WaitGroup
	for i := 0; i < cfg.Concurrency; i++ {
		workerWg.Add(1)
		go func() {
			defer workerWg.Done()
			worker(cfg, client, requestCh, resultCh)
		}()
	}

	// Single collector goroutine — no locks needed in hot path.
	var collectorWg sync.WaitGroup
	collectorWg.Add(1)
	go func() {
		defer collectorWg.Done()
		for r := range resultCh {
			collector.Record(r)
		}
	}()

	// Live display ticker (writes to stderr so JSON stdout stays clean).
	displayDone := make(chan struct{})
	go func() {
		defer close(displayDone)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				elapsed := time.Since(start)
				printLive(elapsed, cfg.Duration, traffic.RateAt(cfg, elapsed), collector.Live())
			}
		}
	}()

	// Rate controller — blocks until ctx expires or is cancelled.
	rateController(ctx, cfg, start, requestCh)

	// Orderly shutdown.
	close(requestCh)  // unblock workers waiting for work
	workerWg.Wait()   // wait for all in-flight requests to complete
	close(resultCh)   // tell collector no more results are coming
	collectorWg.Wait() // drain remaining results
	<-displayDone      // let the display goroutine exit cleanly

	fmt.Fprint(os.Stderr, "\r\033[K") // erase the live line

	elapsed := time.Since(start)
	return report.Print(cfg, collector.Finalize(), elapsed)
}

// rateController sends tokens to requestCh at the rate dictated by the traffic
// profile. It checks every 10ms and emits the number of tokens needed to stay
// on target, skipping tokens if workers are already saturated.
func rateController(ctx context.Context, cfg *config.Config, start time.Time, requestCh chan<- struct{}) {
	var sent int64
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			elapsed := now.Sub(start)
			expected := int64(traffic.CumulativeAt(cfg, elapsed))
			toSend := expected - sent

			for i := int64(0); i < toSend; i++ {
				select {
				case requestCh <- struct{}{}:
					sent++
				case <-ctx.Done():
					return
				default:
					// Workers saturated — skip token rather than blocking.
				}
			}
		}
	}
}

// worker reads from requestCh and executes HTTP requests until the channel is
// closed, sending each result to resultCh.
func worker(cfg *config.Config, client *http.Client, requestCh <-chan struct{}, resultCh chan<- stats.Result) {
	for range requestCh {
		resultCh <- doRequest(cfg, client)
	}
}

func doRequest(cfg *config.Config, client *http.Client) stats.Result {
	t0 := time.Now()

	var body io.Reader
	if cfg.Body != "" {
		body = strings.NewReader(cfg.Body)
	}

	req, err := http.NewRequest(cfg.Method, cfg.URL, body)
	if err != nil {
		return stats.Result{Duration: time.Since(t0), Err: err}
	}

	for _, h := range cfg.Headers {
		parts := strings.SplitN(h, ":", 2)
		if len(parts) == 2 {
			req.Header.Set(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return stats.Result{Duration: time.Since(t0), Err: err}
	}
	defer resp.Body.Close()

	n, _ := io.Copy(io.Discard, resp.Body)
	return stats.Result{
		Duration:   time.Since(t0),
		StatusCode: resp.StatusCode,
		Bytes:      n,
	}
}

func buildClient(cfg *config.Config) *http.Client {
	transport := &http.Transport{
		MaxIdleConns:        cfg.Concurrency + 10,
		MaxIdleConnsPerHost: cfg.Concurrency + 10,
		IdleConnTimeout:     90 * time.Second,
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: cfg.Insecure}, //nolint:gosec
	}

	client := &http.Client{
		Timeout:   cfg.Timeout,
		Transport: transport,
	}
	if !cfg.FollowRedirects {
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	return client
}

func printBanner(cfg *config.Config) {
	fmt.Fprintf(os.Stderr, "\n  trafficloadsim — %s %s\n", cfg.Method, cfg.URL)
	fmt.Fprintf(os.Stderr, "  profile: %-10s  rps: %.0f→%.0f  concurrency: %d  duration: %s\n\n",
		cfg.Profile, cfg.MinRPS, cfg.MaxRPS, cfg.Concurrency, cfg.Duration)
}

func printLive(elapsed, total time.Duration, targetRPS float64, live stats.LiveStats) {
	errPct := 0.0
	if live.Total > 0 {
		errPct = float64(live.Errors) / float64(live.Total) * 100
	}
	pct := int(elapsed.Seconds() / total.Seconds() * 20)
	if pct > 20 {
		pct = 20
	}
	bar := "[" + strings.Repeat("█", pct) + strings.Repeat("░", 20-pct) + "]"

	fmt.Fprintf(os.Stderr,
		"\r\033[K  %s %4.0fs/%-4.0fs  target %6.0f rps  actual %6.1f rps  total %-8s  err %.1f%%  mean %6.1fms",
		bar,
		elapsed.Seconds(),
		total.Seconds(),
		targetRPS,
		live.CurrentRPS,
		commify(live.Total),
		errPct,
		live.WindowMean,
	)
}

func commify(n int64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, byte(c))
	}
	return string(out)
}
