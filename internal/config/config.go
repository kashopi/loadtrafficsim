package config

import (
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"
)

type headerFlags []string

func (h *headerFlags) String() string  { return strings.Join(*h, ", ") }
func (h *headerFlags) Set(v string) error {
	if !strings.Contains(v, ":") {
		return fmt.Errorf("header must be in 'Key: Value' format")
	}
	*h = append(*h, v)
	return nil
}

type Config struct {
	URL             string
	Method          string
	Headers         []string
	Body            string
	Duration        time.Duration
	MinRPS          float64
	MaxRPS          float64
	Concurrency     int
	Profile         string
	Steps           int
	Timeout         time.Duration
	Insecure        bool
	FollowRedirects bool
	Output          string // "text" or "json"
}

var validProfiles = map[string]bool{
	"constant": true,
	"linear":   true,
	"step":     true,
	"wave":     true,
	"spike":    true,
}

func Parse() (*Config, error) {
	cfg := &Config{}
	var headers headerFlags

	flag.StringVar(&cfg.URL, "url", "", "Target URL (required)")
	flag.StringVar(&cfg.Method, "method", "GET", "HTTP method (GET, POST, PUT, DELETE, ...)")
	flag.Var(&headers, "header", "HTTP header 'Key: Value' (repeatable)")
	flag.StringVar(&cfg.Body, "body", "", "Request body")
	flag.DurationVar(&cfg.Duration, "duration", 30*time.Second, "Test duration")
	flag.Float64Var(&cfg.MinRPS, "min-rps", 1, "Starting requests per second")
	flag.Float64Var(&cfg.MaxRPS, "max-rps", 100, "Peak requests per second")
	flag.IntVar(&cfg.Concurrency, "concurrency", 50, "Max concurrent requests in flight")
	flag.StringVar(&cfg.Profile, "profile", "linear",
		"Traffic profile: constant, linear, step, wave, spike\n"+
			"  constant  fixed at max-rps throughout\n"+
			"  linear    ramp from min-rps to max-rps over duration\n"+
			"  step      staircase increases (use --steps to control count)\n"+
			"  wave      sinusoidal oscillation between min-rps and max-rps\n"+
			"  spike     low traffic with a burst spike at the midpoint",
	)
	flag.IntVar(&cfg.Steps, "steps", 5, "Number of steps for the 'step' profile")
	flag.DurationVar(&cfg.Timeout, "timeout", 10*time.Second, "Per-request timeout")
	flag.BoolVar(&cfg.Insecure, "insecure", false, "Skip TLS certificate verification")
	flag.BoolVar(&cfg.FollowRedirects, "follow-redirects", true, "Follow HTTP redirects")
	flag.StringVar(&cfg.Output, "output", "text", "Output format: text or json")

	flag.Parse()

	cfg.Headers = headers
	return cfg, cfg.validate()
}

func (c *Config) validate() error {
	var errs []string

	if c.URL == "" {
		errs = append(errs, "-url is required")
	}
	if c.MaxRPS <= 0 {
		errs = append(errs, "-max-rps must be positive")
	}
	if c.MinRPS < 0 {
		errs = append(errs, "-min-rps must be non-negative")
	}
	if c.MinRPS > c.MaxRPS {
		errs = append(errs, "-min-rps must not exceed -max-rps")
	}
	if c.Concurrency <= 0 {
		errs = append(errs, "-concurrency must be positive")
	}
	if c.Duration <= 0 {
		errs = append(errs, "-duration must be positive")
	}
	if !validProfiles[c.Profile] {
		errs = append(errs, fmt.Sprintf("-profile %q is invalid; valid: constant, linear, step, wave, spike", c.Profile))
	}
	if c.Steps < 1 {
		errs = append(errs, "-steps must be at least 1")
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "\n"))
	}
	return nil
}
