package traffic

import (
	"math"
	"testing"
	"time"

	"trafficloadsim/internal/config"
)

// cfg is a test helper that builds a minimal Config for traffic tests.
func cfg(profile string, min, max float64, dur time.Duration, steps int) *config.Config {
	return &config.Config{
		Profile:  profile,
		MinRPS:   min,
		MaxRPS:   max,
		Duration: dur,
		Steps:    steps,
	}
}

const (
	eps  = 0.001 // absolute tolerance for float comparisons
	dur  = 10 * time.Second
	min  = 10.0
	max  = 100.0
)

func approxEqual(a, b float64) bool { return math.Abs(a-b) <= eps }

// ── RateAt ───────────────────────────────────────────────────────────────────

func TestRateAt_Constant(t *testing.T) {
	c := cfg("constant", min, max, dur, 1)
	for _, at := range []time.Duration{0, dur / 2, dur} {
		if got := RateAt(c, at); !approxEqual(got, max) {
			t.Errorf("constant RateAt(%v) = %.4f, want %.4f", at, got, max)
		}
	}
}

func TestRateAt_Linear(t *testing.T) {
	c := cfg("linear", min, max, dur, 1)
	cases := []struct {
		at   time.Duration
		want float64
	}{
		{0, min},                    // start = min
		{dur / 2, (min + max) / 2},  // midpoint
		{dur, max},                  // end = max
	}
	for _, tc := range cases {
		if got := RateAt(c, tc.at); !approxEqual(got, tc.want) {
			t.Errorf("linear RateAt(%v) = %.4f, want %.4f", tc.at, got, tc.want)
		}
	}
}

func TestRateAt_Step(t *testing.T) {
	// 5-step staircase: steps at 0%, 20%, 40%, 60%, 80% of the range (0→90 gap).
	// step i covers [i/5, (i+1)/5) of duration; rate = min + (max-min)·i/5.
	c := cfg("step", min, max, dur, 5)
	cases := []struct {
		at   time.Duration
		want float64
	}{
		{0, min},                              // step 0 → rate = min
		{dur / 10, min},                       // still step 0 (10% < 20%)
		{dur*2/10 + time.Millisecond, min + (max-min)*1/5}, // step 1
		{dur * 8 / 10, min + (max-min)*4/5},   // step 4
		{dur, max},                            // ratio=1 → floor(5)/5=1 → max
	}
	for _, tc := range cases {
		if got := RateAt(c, tc.at); !approxEqual(got, tc.want) {
			t.Errorf("step RateAt(%v) = %.4f, want %.4f", tc.at, got, tc.want)
		}
	}
}

func TestRateAt_Wave(t *testing.T) {
	// Wave: rate(ratio) = mid + amplitude·sin(2π·ratio − π/2)
	// sin(−π/2)=−1 → min at ratio=0
	// sin(π/2)=+1  → max at ratio=0.5
	// sin(3π/2)=−1 → min at ratio=1
	c := cfg("wave", min, max, dur, 1)
	cases := []struct {
		at   time.Duration
		want float64
	}{
		{0, min},                         // starts at min
		{dur / 2, max},                   // peaks at max (ratio=0.5 → sin(π−π/2)=sin(π/2)=1)
		{dur, min},                       // returns to min
	}
	for _, tc := range cases {
		if got := RateAt(c, tc.at); !approxEqual(got, tc.want) {
			t.Errorf("wave RateAt(%v) = %.4f, want %.4f", tc.at, got, tc.want)
		}
	}
}

func TestRateAt_Spike(t *testing.T) {
	c := cfg("spike", min, max, dur, 1)
	cases := []struct {
		at   time.Duration
		want float64
	}{
		{0, min},                          // before spike
		{dur * 39 / 100, min},             // just before spike (39%)
		{dur * 40 / 100, max},             // spike start boundary
		{dur / 2, max},                    // middle of spike
		{dur * 60 / 100, max},             // spike end boundary
		{dur * 61 / 100, min},             // just after spike (61%)
		{dur, min},                        // end
	}
	for _, tc := range cases {
		if got := RateAt(c, tc.at); !approxEqual(got, tc.want) {
			t.Errorf("spike RateAt(%v) = %.4f, want %.4f", tc.at, got, tc.want)
		}
	}
}

func TestRateAt_NegativeTime(t *testing.T) {
	// t ≤ 0 should return the rate at ratio=0.
	for _, profile := range []string{"constant", "linear", "step", "wave", "spike"} {
		c := cfg(profile, min, max, dur, 3)
		want := RateAt(c, 0)
		if got := RateAt(c, -time.Second); !approxEqual(got, want) {
			t.Errorf("%s: RateAt(-1s) = %.4f, want %.4f (same as t=0)", profile, got, want)
		}
	}
}

func TestRateAt_BeyondDuration(t *testing.T) {
	// t > Duration should behave the same as t = Duration (clamped).
	for _, profile := range []string{"constant", "linear", "step", "wave", "spike"} {
		c := cfg(profile, min, max, dur, 3)
		atEnd := RateAt(c, dur)
		if got := RateAt(c, dur*2); !approxEqual(got, atEnd) {
			t.Errorf("%s: RateAt(2T) = %.4f, want %.4f (same as t=T)", profile, got, atEnd)
		}
	}
}

// ── CumulativeAt ─────────────────────────────────────────────────────────────

func TestCumulativeAt_ZeroOrNegative(t *testing.T) {
	for _, profile := range []string{"constant", "linear", "step", "wave", "spike"} {
		c := cfg(profile, min, max, dur, 3)
		for _, at := range []time.Duration{0, -time.Second} {
			if got := CumulativeAt(c, at); got != 0 {
				t.Errorf("%s: CumulativeAt(%v) = %.4f, want 0", profile, at, got)
			}
		}
	}
}

func TestCumulativeAt_Constant(t *testing.T) {
	// ∫₀ˢ max·dτ = max·s
	c := cfg("constant", min, max, dur, 1)
	cases := []struct{ s, want float64 }{
		{1, max * 1},
		{5, max * 5},
		{10, max * 10},
	}
	for _, tc := range cases {
		at := time.Duration(tc.s * float64(time.Second))
		if got := CumulativeAt(c, at); !approxEqual(got, tc.want) {
			t.Errorf("constant CumulativeAt(%.0fs) = %.4f, want %.4f", tc.s, got, tc.want)
		}
	}
}

func TestCumulativeAt_Linear(t *testing.T) {
	// ∫₀ˢ (min + (max-min)·τ/T) dτ = min·s + (max-min)·s²/(2T)
	// Full duration (T=10): 10·10 + 90·100/20 = 100 + 450 = 550
	// Average rate = (min+max)/2 = 55; total = 55·10 = 550.
	c := cfg("linear", min, max, dur, 1)
	T := dur.Seconds()
	cases := []float64{1, 3, 5, 7, 10}
	for _, s := range cases {
		want := min*s + (max-min)*s*s/(2*T)
		at := time.Duration(s * float64(time.Second))
		if got := CumulativeAt(c, at); !approxEqual(got, want) {
			t.Errorf("linear CumulativeAt(%.0fs) = %.6f, want %.6f", s, got, want)
		}
	}
}

func TestCumulativeAt_Linear_FullDuration(t *testing.T) {
	// The total for a linear ramp equals average_rate × duration.
	c := cfg("linear", min, max, dur, 1)
	avgRate := (min + max) / 2
	want := avgRate * dur.Seconds()
	if got := CumulativeAt(c, dur); !approxEqual(got, want) {
		t.Errorf("linear full-duration: %.4f, want %.4f", got, want)
	}
}

func TestCumulativeAt_Step(t *testing.T) {
	// 4 steps over 8s, min=0, max=40:
	// step 0: rate=0, dur=2s → 0
	// step 1: rate=10, dur=2s → 20
	// step 2: rate=20, dur=2s → 40
	// step 3: rate=30, dur=2s → 60
	// total = 120; (at t=T, step=4 → rate=40, but that's floor(4)/4=1 → rate=max=40)
	c := cfg("step", 0, 40, 8*time.Second, 4)
	cases := []struct{ s, want float64 }{
		{0, 0},
		{1, 0 * 1},                    // still in step 0 (rate=0)
		{2, 0 * 2},                    // end of step 0
		{3, 0*2 + 10*1},               // 1s into step 1
		{4, 0*2 + 10*2},               // end of step 1
		{6, 0*2 + 10*2 + 20*2},        // end of step 2
		{8, 0*2 + 10*2 + 20*2 + 30*2}, // total
	}
	for _, tc := range cases {
		at := time.Duration(tc.s * float64(time.Second))
		if got := CumulativeAt(c, at); !approxEqual(got, tc.want) {
			t.Errorf("step CumulativeAt(%.0fs) = %.4f, want %.4f", tc.s, got, tc.want)
		}
	}
}

func TestCumulativeAt_Wave_FullCycle(t *testing.T) {
	// Over a full cycle the integral = mid·T (positive and negative sine halves cancel).
	c := cfg("wave", min, max, dur, 1)
	mid := (min + max) / 2
	want := mid * dur.Seconds()
	if got := CumulativeAt(c, dur); !approxEqual(got, want) {
		t.Errorf("wave full-cycle: %.6f, want %.6f", got, want)
	}
}

func TestCumulativeAt_Spike(t *testing.T) {
	// min=5, max=50, T=10s: spike from 4s to 6s.
	// [0,4): rate=5 → 5*4=20
	// [4,6): rate=50 → 50*2=100
	// [6,10]: rate=5 → 5*4=20
	// total = 140
	c := cfg("spike", 5, 50, 10*time.Second, 1)
	cases := []struct{ s, want float64 }{
		{0, 0},
		{2, 5 * 2},
		{4, 5 * 4},                    // end of pre-spike
		{5, 5*4 + 50*1},               // midpoint of spike
		{6, 5*4 + 50*2},               // end of spike
		{8, 5*4 + 50*2 + 5*2},         // 2s into post-spike
		{10, 5*4 + 50*2 + 5*4},        // full duration
	}
	for _, tc := range cases {
		at := time.Duration(tc.s * float64(time.Second))
		if got := CumulativeAt(c, at); !approxEqual(got, tc.want) {
			t.Errorf("spike CumulativeAt(%.0fs) = %.4f, want %.4f", tc.s, got, tc.want)
		}
	}
}

func TestCumulativeAt_Monotonic(t *testing.T) {
	// CumulativeAt must be non-decreasing for all profiles (rate is always ≥ 0).
	profiles := []struct {
		name  string
		steps int
	}{
		{"constant", 1}, {"linear", 1}, {"step", 5}, {"wave", 1}, {"spike", 1},
	}
	for _, p := range profiles {
		t.Run(p.name, func(t *testing.T) {
			c := cfg(p.name, min, max, dur, p.steps)
			prev := CumulativeAt(c, 0)
			for i := 1; i <= 1000; i++ {
				at := time.Duration(float64(dur) * float64(i) / 1000)
				curr := CumulativeAt(c, at)
				if curr < prev-eps {
					t.Errorf("at step %d: CumulativeAt decreased from %.6f to %.6f", i, prev, curr)
				}
				prev = curr
			}
		})
	}
}

func TestCumulativeAt_NumericalCrossCheck(t *testing.T) {
	// Cross-check analytic integral against trapezoidal numerical integration.
	profiles := []struct {
		name  string
		steps int
	}{
		{"constant", 1}, {"linear", 1}, {"step", 4}, {"wave", 1}, {"spike", 1},
	}
	const n = 10_000
	for _, p := range profiles {
		t.Run(p.name, func(t *testing.T) {
			c := cfg(p.name, min, max, dur, p.steps)
			dt := dur.Seconds() / n
			var numerical float64
			for i := 0; i < n; i++ {
				at := time.Duration(float64(dur) * float64(i) / n)
				numerical += RateAt(c, at) * dt
			}
			analytic := CumulativeAt(c, dur)
			// 0.5% tolerance — trapezoidal has O(dt) error for smooth functions.
			tolerance := analytic * 0.005
			if math.Abs(analytic-numerical) > tolerance+eps {
				t.Errorf("%s: analytic=%.4f numerical=%.4f diff=%.4f",
					p.name, analytic, numerical, math.Abs(analytic-numerical))
			}
		})
	}
}

func TestCumulativeAt_BeyondDuration(t *testing.T) {
	// t > Duration should clamp to the Duration total.
	for _, profile := range []string{"constant", "linear", "step", "wave", "spike"} {
		c := cfg(profile, min, max, dur, 3)
		atT := CumulativeAt(c, dur)
		if got := CumulativeAt(c, dur*2); !approxEqual(got, atT) {
			t.Errorf("%s: CumulativeAt(2T) = %.4f, want %.4f", profile, got, atT)
		}
	}
}

// ── Description ──────────────────────────────────────────────────────────────

func TestDescription(t *testing.T) {
	cases := []struct {
		profile string
		want    string
	}{
		{"constant", "constant"},
		{"linear", "linear"},
		{"step", "step"},
		{"wave", "wave"},
		{"spike", "spike"},
	}
	for _, tc := range cases {
		c := cfg(tc.profile, min, max, dur, 5)
		d := Description(c)
		if d == "" {
			t.Errorf("%s: Description returned empty string", tc.profile)
		}
		if !containsFold(d, tc.want) {
			t.Errorf("%s: Description %q does not contain %q", tc.profile, d, tc.want)
		}
	}
}

func containsFold(s, sub string) bool {
	return len(s) >= len(sub) &&
		func() bool {
			for i := range s {
				if i+len(sub) > len(s) {
					break
				}
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}()
}
