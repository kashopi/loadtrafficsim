package traffic

import (
	"fmt"
	"math"
	"time"

	"trafficloadsim/internal/config"
)

// RateAt returns the target RPS at elapsed time t during the test.
func RateAt(cfg *config.Config, t time.Duration) float64 {
	if t <= 0 {
		return rateAtRatio(cfg, 0)
	}
	ratio := t.Seconds() / cfg.Duration.Seconds()
	if ratio > 1 {
		ratio = 1
	}
	return rateAtRatio(cfg, ratio)
}

// rateAtRatio maps a [0,1] progress ratio to a target RPS.
func rateAtRatio(cfg *config.Config, ratio float64) float64 {
	min, max := cfg.MinRPS, cfg.MaxRPS

	switch cfg.Profile {
	case "constant":
		return max

	case "linear":
		return min + (max-min)*ratio

	case "step":
		steps := float64(cfg.Steps)
		// Floor to the nearest completed step boundary.
		step := math.Floor(ratio*steps) / steps
		return min + (max-min)*step

	case "wave":
		// One full sine cycle: starts at min, peaks at max at 25%, back to min at 50%,
		// troughs at 75%, returns to min at 100%.
		mid := (max + min) / 2
		amplitude := (max - min) / 2
		// Shift by -π/2 so the wave starts at min (sin(-π/2) = -1).
		return mid + amplitude*math.Sin(2*math.Pi*ratio-math.Pi/2)

	case "spike":
		// Constant low traffic, with a spike during the middle 20% of duration.
		if ratio >= 0.40 && ratio <= 0.60 {
			return max
		}
		return min
	}

	return max
}

// CumulativeAt returns the total expected request count by elapsed time t,
// i.e. ∫₀ᵗ RateAt(τ) dτ. This is what the rate controller uses to stay
// accurate — using instantaneous_rate × t would double-count for ramp profiles.
func CumulativeAt(cfg *config.Config, t time.Duration) float64 {
	if t <= 0 {
		return 0
	}
	T := cfg.Duration.Seconds()
	s := t.Seconds()
	if s > T {
		s = T
	}
	min, max := cfg.MinRPS, cfg.MaxRPS

	switch cfg.Profile {
	case "constant":
		return max * s

	case "linear":
		// ∫₀ˢ (min + (max-min)·τ/T) dτ = min·s + (max-min)·s²/(2T)
		return min*s + (max-min)*s*s/(2*T)

	case "step":
		// Each step i spans [i·T/N, (i+1)·T/N) at rate min+(max-min)·i/N.
		steps := float64(cfg.Steps)
		stepDur := T / steps
		currentStep := math.Floor(s / stepDur)
		var total float64
		for i := float64(0); i < currentStep; i++ {
			total += (min + (max-min)*i/steps) * stepDur
		}
		partialInStep := s - currentStep*stepDur
		total += (min + (max-min)*currentStep/steps) * partialInStep
		return total

	case "wave":
		// rate(τ) = mid + amplitude·sin(2π·τ/T − π/2)
		// ∫₀ˢ = mid·s − amplitude·T/(2π)·cos(2π·s/T − π/2)
		// (cos(−π/2) = 0, so the lower-bound term vanishes)
		mid := (max + min) / 2
		amplitude := (max - min) / 2
		return mid*s - amplitude*T/(2*math.Pi)*math.Cos(2*math.Pi*s/T-math.Pi/2)

	case "spike":
		spikeStart := 0.4 * T
		spikeEnd := 0.6 * T
		switch {
		case s <= spikeStart:
			return min * s
		case s <= spikeEnd:
			return min*spikeStart + max*(s-spikeStart)
		default:
			return min*spikeStart + max*(spikeEnd-spikeStart) + min*(s-spikeEnd)
		}
	}

	return max * s
}

// Description returns a human-readable description of the traffic profile.
func Description(cfg *config.Config) string {
	switch cfg.Profile {
	case "constant":
		return fmt.Sprintf("constant %.0f rps", cfg.MaxRPS)
	case "linear":
		return fmt.Sprintf("linear %.0f → %.0f rps over %s", cfg.MinRPS, cfg.MaxRPS, cfg.Duration)
	case "step":
		return fmt.Sprintf("step %.0f → %.0f rps in %d steps", cfg.MinRPS, cfg.MaxRPS, cfg.Steps)
	case "wave":
		return fmt.Sprintf("wave %.0f ↔ %.0f rps", cfg.MinRPS, cfg.MaxRPS)
	case "spike":
		return fmt.Sprintf("spike %.0f rps (burst to %.0f at midpoint)", cfg.MinRPS, cfg.MaxRPS)
	}
	return cfg.Profile
}
