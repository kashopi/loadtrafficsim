package config

import (
	"strings"
	"testing"
	"time"
)

// ── validate ──────────────────────────────────────────────────────────────────

func validConfig() *Config {
	return &Config{
		URL:             "http://example.com",
		Method:          "GET",
		Duration:        10 * time.Second,
		MinRPS:          1,
		MaxRPS:          100,
		Concurrency:     10,
		Profile:         "linear",
		Steps:           5,
		Timeout:         5 * time.Second,
		FollowRedirects: true,
		Output:          "text",
	}
}

func TestValidate_Valid(t *testing.T) {
	if err := validConfig().validate(); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}
}

func TestValidate_MissingURL(t *testing.T) {
	c := validConfig()
	c.URL = ""
	assertError(t, c, "-url is required")
}

func TestValidate_ZeroMaxRPS(t *testing.T) {
	c := validConfig()
	c.MaxRPS = 0
	assertError(t, c, "-max-rps must be positive")
}

func TestValidate_NegativeMaxRPS(t *testing.T) {
	c := validConfig()
	c.MaxRPS = -1
	assertError(t, c, "-max-rps must be positive")
}

func TestValidate_NegativeMinRPS(t *testing.T) {
	c := validConfig()
	c.MinRPS = -1
	assertError(t, c, "-min-rps must be non-negative")
}

func TestValidate_MinRPSExceedsMaxRPS(t *testing.T) {
	c := validConfig()
	c.MinRPS = 200
	c.MaxRPS = 100
	assertError(t, c, "-min-rps must not exceed -max-rps")
}

func TestValidate_MinRPSEqualToMaxRPS(t *testing.T) {
	// Equal min and max is valid (constant-rate equivalent).
	c := validConfig()
	c.MinRPS = 50
	c.MaxRPS = 50
	if err := c.validate(); err != nil {
		t.Errorf("min==max should be valid, got: %v", err)
	}
}

func TestValidate_ZeroConcurrency(t *testing.T) {
	c := validConfig()
	c.Concurrency = 0
	assertError(t, c, "-concurrency must be positive")
}

func TestValidate_NegativeConcurrency(t *testing.T) {
	c := validConfig()
	c.Concurrency = -5
	assertError(t, c, "-concurrency must be positive")
}

func TestValidate_ZeroDuration(t *testing.T) {
	c := validConfig()
	c.Duration = 0
	assertError(t, c, "-duration must be positive")
}

func TestValidate_NegativeDuration(t *testing.T) {
	c := validConfig()
	c.Duration = -time.Second
	assertError(t, c, "-duration must be positive")
}

func TestValidate_InvalidProfile(t *testing.T) {
	c := validConfig()
	c.Profile = "ramp"
	assertError(t, c, "-profile")
}

func TestValidate_AllValidProfiles(t *testing.T) {
	for _, p := range []string{"constant", "linear", "step", "wave", "spike"} {
		c := validConfig()
		c.Profile = p
		if err := c.validate(); err != nil {
			t.Errorf("profile %q should be valid, got: %v", p, err)
		}
	}
}

func TestValidate_ZeroSteps(t *testing.T) {
	c := validConfig()
	c.Steps = 0
	assertError(t, c, "-steps must be at least 1")
}

func TestValidate_MultipleErrors(t *testing.T) {
	// All errors should be reported at once, not just the first.
	c := validConfig()
	c.URL = ""
	c.MaxRPS = -1
	c.Concurrency = 0
	err := c.validate()
	if err == nil {
		t.Fatal("expected error for multiple invalid fields")
	}
	for _, fragment := range []string{"-url", "-max-rps", "-concurrency"} {
		if !strings.Contains(err.Error(), fragment) {
			t.Errorf("error %q does not contain %q", err.Error(), fragment)
		}
	}
}

// ── headerFlags ───────────────────────────────────────────────────────────────

func TestHeaderFlags_SetValid(t *testing.T) {
	var h headerFlags
	if err := h.Set("Authorization: Bearer token"); err != nil {
		t.Errorf("valid header rejected: %v", err)
	}
	if err := h.Set("X-Custom: value with spaces"); err != nil {
		t.Errorf("header with spaces rejected: %v", err)
	}
	if len(h) != 2 {
		t.Errorf("len(h) = %d, want 2", len(h))
	}
}

func TestHeaderFlags_SetInvalidMissingColon(t *testing.T) {
	var h headerFlags
	if err := h.Set("Authorization"); err == nil {
		t.Error("expected error for header without colon")
	}
}

func TestHeaderFlags_SetMultiple(t *testing.T) {
	var h headerFlags
	_ = h.Set("A: 1")
	_ = h.Set("B: 2")
	_ = h.Set("C: 3")
	if h.String() != "A: 1, B: 2, C: 3" {
		t.Errorf("String() = %q", h.String())
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func assertError(t *testing.T, c *Config, fragment string) {
	t.Helper()
	err := c.validate()
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", fragment)
	}
	if !strings.Contains(err.Error(), fragment) {
		t.Errorf("error %q does not contain %q", err.Error(), fragment)
	}
}
