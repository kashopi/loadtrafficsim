package report

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"trafficloadsim/internal/config"
	"trafficloadsim/internal/stats"
	"trafficloadsim/internal/traffic"
)

// Print writes the final test report to os.Stdout.
func Print(cfg *config.Config, r stats.Report, elapsed time.Duration) error {
	return Render(os.Stdout, cfg, r, elapsed)
}

// Render writes the final test report to w in the format specified by cfg.Output.
func Render(w io.Writer, cfg *config.Config, r stats.Report, elapsed time.Duration) error {
	switch cfg.Output {
	case "json":
		return renderJSON(w, cfg, r, elapsed)
	default:
		renderText(w, cfg, r, elapsed)
		return nil
	}
}

func renderText(w io.Writer, cfg *config.Config, r stats.Report, elapsed time.Duration) {
	bar := strings.Repeat("━", 60)
	fmt.Fprintln(w)
	fmt.Fprintln(w, bar)
	fmt.Fprintln(w, "  Load Test Results")
	fmt.Fprintln(w, bar)

	successPct := 0.0
	errorPct := 0.0
	if r.Total > 0 {
		successPct = float64(r.Success) / float64(r.Total) * 100
		errorPct = float64(r.Errors) / float64(r.Total) * 100
	}
	avgRPS := 0.0
	if elapsed.Seconds() > 0 {
		avgRPS = float64(r.Total) / elapsed.Seconds()
	}

	fmt.Fprintf(w, "\n  Target       %s %s\n", cfg.Method, cfg.URL)
	fmt.Fprintf(w, "  Profile      %s\n", traffic.Description(cfg))
	fmt.Fprintf(w, "  Duration     %s (actual %.1fs)\n", cfg.Duration, elapsed.Seconds())
	fmt.Fprintf(w, "  Concurrency  %d\n", cfg.Concurrency)

	fmt.Fprintln(w)
	fmt.Fprintln(w, "  Requests")
	fmt.Fprintf(w, "    Total       %s\n", commify(r.Total))
	fmt.Fprintf(w, "    Success     %s (%.1f%%)\n", commify(r.Success), successPct)
	fmt.Fprintf(w, "    Errors      %s (%.1f%%)\n", commify(r.Errors), errorPct)
	fmt.Fprintf(w, "    Throughput  %.1f req/s avg\n", avgRPS)
	fmt.Fprintf(w, "    Received    %s\n", formatBytes(r.BytesRecv))

	if r.Total > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "  Latency")
		fmt.Fprintf(w, "    Min    %s\n", fmtMS(r.MinMS))
		fmt.Fprintf(w, "    Mean   %s\n", fmtMS(r.MeanMS))
		fmt.Fprintf(w, "    P50    %s\n", fmtMS(r.P50MS))
		fmt.Fprintf(w, "    P90    %s\n", fmtMS(r.P90MS))
		fmt.Fprintf(w, "    P95    %s\n", fmtMS(r.P95MS))
		fmt.Fprintf(w, "    P99    %s\n", fmtMS(r.P99MS))
		fmt.Fprintf(w, "    Max    %s\n", fmtMS(r.MaxMS))
	}

	if len(r.StatusCodes) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "  Status Codes")
		for _, code := range sortedIntKeys(r.StatusCodes) {
			fmt.Fprintf(w, "    %d    %s\n", code, commify(r.StatusCodes[code]))
		}
	}

	if len(r.ErrorGroups) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "  Errors")
		for group, count := range r.ErrorGroups {
			fmt.Fprintf(w, "    %-35s %s\n", group, commify(count))
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, bar)
}

type jsonReport struct {
	Config struct {
		URL         string  `json:"url"`
		Method      string  `json:"method"`
		Profile     string  `json:"profile"`
		MinRPS      float64 `json:"min_rps"`
		MaxRPS      float64 `json:"max_rps"`
		Concurrency int     `json:"concurrency"`
		DurationS   float64 `json:"duration_seconds"`
	} `json:"config"`
	Results struct {
		ActualDurationS float64 `json:"actual_duration_seconds"`
		Total           int64   `json:"total"`
		Success         int64   `json:"success"`
		Errors          int64   `json:"errors"`
		SuccessPct      float64 `json:"success_pct"`
		ErrorPct        float64 `json:"error_pct"`
		AvgRPS          float64 `json:"avg_rps"`
		BytesReceived   int64   `json:"bytes_received"`
	} `json:"results"`
	Latency struct {
		MinMS  float64 `json:"min_ms"`
		MeanMS float64 `json:"mean_ms"`
		P50MS  float64 `json:"p50_ms"`
		P90MS  float64 `json:"p90_ms"`
		P95MS  float64 `json:"p95_ms"`
		P99MS  float64 `json:"p99_ms"`
		MaxMS  float64 `json:"max_ms"`
	} `json:"latency"`
	StatusCodes map[string]int64 `json:"status_codes"`
	ErrorGroups map[string]int64 `json:"errors"`
}

func renderJSON(w io.Writer, cfg *config.Config, r stats.Report, elapsed time.Duration) error {
	var out jsonReport
	out.Config.URL = cfg.URL
	out.Config.Method = cfg.Method
	out.Config.Profile = cfg.Profile
	out.Config.MinRPS = cfg.MinRPS
	out.Config.MaxRPS = cfg.MaxRPS
	out.Config.Concurrency = cfg.Concurrency
	out.Config.DurationS = cfg.Duration.Seconds()

	out.Results.ActualDurationS = elapsed.Seconds()
	out.Results.Total = r.Total
	out.Results.Success = r.Success
	out.Results.Errors = r.Errors
	out.Results.BytesReceived = r.BytesRecv
	if r.Total > 0 {
		out.Results.SuccessPct = round2(float64(r.Success) / float64(r.Total) * 100)
		out.Results.ErrorPct = round2(float64(r.Errors) / float64(r.Total) * 100)
	}
	if elapsed.Seconds() > 0 {
		out.Results.AvgRPS = round2(float64(r.Total) / elapsed.Seconds())
	}

	out.Latency.MinMS = round2(r.MinMS)
	out.Latency.MeanMS = round2(r.MeanMS)
	out.Latency.P50MS = round2(r.P50MS)
	out.Latency.P90MS = round2(r.P90MS)
	out.Latency.P95MS = round2(r.P95MS)
	out.Latency.P99MS = round2(r.P99MS)
	out.Latency.MaxMS = round2(r.MaxMS)

	out.StatusCodes = make(map[string]int64, len(r.StatusCodes))
	for code, count := range r.StatusCodes {
		out.StatusCodes[fmt.Sprintf("%d", code)] = count
	}
	out.ErrorGroups = r.ErrorGroups

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func fmtMS(ms float64) string {
	if ms >= 1000 {
		return fmt.Sprintf("%.2fs", ms/1000)
	}
	return fmt.Sprintf("%.2fms", ms)
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
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

func round2(f float64) float64 {
	return math.Round(f*100) / 100
}

func sortedIntKeys(m map[int]int64) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}
