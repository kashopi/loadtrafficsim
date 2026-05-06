package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"trafficloadsim/internal/config"
	"trafficloadsim/internal/stats"
)

// ── commify ───────────────────────────────────────────────────────────────────

func TestCommify(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{9, "9"},
		{999, "999"},
		{1000, "1,000"},
		{1001, "1,001"},
		{9999, "9,999"},
		{10000, "10,000"},
		{999999, "999,999"},
		{1000000, "1,000,000"},
		{1234567, "1,234,567"},
	}
	for _, tc := range cases {
		if got := commify(tc.in); got != tc.want {
			t.Errorf("commify(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ── fmtMS ─────────────────────────────────────────────────────────────────────

func TestFmtMS(t *testing.T) {
	cases := []struct {
		ms   float64
		want string
	}{
		{0, "0.00ms"},
		{0.5, "0.50ms"},
		{1, "1.00ms"},
		{12.34, "12.34ms"},
		{999.99, "999.99ms"},
		{1000, "1.00s"},
		{1500, "1.50s"},
		{60000, "60.00s"},
	}
	for _, tc := range cases {
		if got := fmtMS(tc.ms); got != tc.want {
			t.Errorf("fmtMS(%.2f) = %q, want %q", tc.ms, got, tc.want)
		}
	}
}

// ── formatBytes ───────────────────────────────────────────────────────────────

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
	}
	for _, tc := range cases {
		if got := formatBytes(tc.in); got != tc.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ── round2 ────────────────────────────────────────────────────────────────────

func TestRound2(t *testing.T) {
	cases := []struct{ in, want float64 }{
		{1.0, 1.0},
		{1.234, 1.23},
		{1.235, 1.24},
		{99.999, 100.0},
		{0.001, 0.0},
	}
	for _, tc := range cases {
		if got := round2(tc.in); got != tc.want {
			t.Errorf("round2(%.4f) = %.4f, want %.4f", tc.in, got, tc.want)
		}
	}
}

// ── sortedIntKeys ─────────────────────────────────────────────────────────────

func TestSortedIntKeys(t *testing.T) {
	m := map[int]int64{500: 1, 200: 10, 404: 2}
	keys := sortedIntKeys(m)
	want := []int{200, 404, 500}
	for i, k := range keys {
		if k != want[i] {
			t.Errorf("sortedIntKeys[%d] = %d, want %d", i, k, want[i])
		}
	}
}

// ── Render JSON ───────────────────────────────────────────────────────────────

func TestRenderJSON_Structure(t *testing.T) {
	cfg := &config.Config{
		URL:         "http://example.com/api",
		Method:      "POST",
		Profile:     "linear",
		MinRPS:      10,
		MaxRPS:      100,
		Concurrency: 20,
		Duration:    30 * time.Second,
		Output:      "json",
		Steps:       5,
	}
	report := stats.Report{
		Total:   100,
		Success: 95,
		Errors:  5,
		StatusCodes: map[int]int64{
			200: 95,
			500: 5,
		},
		ErrorGroups: map[string]int64{"timeout": 5},
		MeanMS:      42.5,
		MinMS:       10.0,
		MaxMS:       200.0,
		P50MS:       38.0,
		P90MS:       90.0,
		P95MS:       120.0,
		P99MS:       180.0,
		BytesRecv:   1024,
	}
	elapsed := 30 * time.Second

	var buf bytes.Buffer
	if err := Render(&buf, cfg, report, elapsed); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}

	// Top-level keys.
	for _, key := range []string{"config", "results", "latency", "status_codes", "errors"} {
		if _, ok := out[key]; !ok {
			t.Errorf("JSON missing top-level key %q", key)
		}
	}

	// Config section.
	outCfg := out["config"].(map[string]any)
	if outCfg["url"] != cfg.URL {
		t.Errorf("config.url = %v, want %v", outCfg["url"], cfg.URL)
	}
	if outCfg["method"] != cfg.Method {
		t.Errorf("config.method = %v, want %v", outCfg["method"], cfg.Method)
	}

	// Results section.
	outRes := out["results"].(map[string]any)
	if int64(outRes["total"].(float64)) != 100 {
		t.Errorf("results.total = %v, want 100", outRes["total"])
	}
	if int64(outRes["success"].(float64)) != 95 {
		t.Errorf("results.success = %v, want 95", outRes["success"])
	}
	successPct := outRes["success_pct"].(float64)
	if successPct < 94.9 || successPct > 95.1 {
		t.Errorf("results.success_pct = %.2f, want ~95.00", successPct)
	}

	// Status codes.
	codes := out["status_codes"].(map[string]any)
	if int64(codes["200"].(float64)) != 95 {
		t.Errorf("status_codes.200 = %v, want 95", codes["200"])
	}
}

func TestRenderJSON_EmptyReport(t *testing.T) {
	cfg := &config.Config{
		URL: "http://example.com", Method: "GET",
		Profile: "constant", MaxRPS: 10, MinRPS: 1,
		Duration: time.Second, Concurrency: 1, Steps: 1,
		Output: "json",
	}
	var buf bytes.Buffer
	err := Render(&buf, cfg, stats.Report{StatusCodes: map[int]int64{}, ErrorGroups: map[string]int64{}}, time.Second)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
}

// ── Render text ───────────────────────────────────────────────────────────────

func TestRenderText_ContainsKeyFields(t *testing.T) {
	cfg := &config.Config{
		URL: "http://example.com", Method: "GET",
		Profile: "linear", MaxRPS: 100, MinRPS: 10,
		Duration: 30 * time.Second, Concurrency: 50, Steps: 5,
		Output: "text",
	}
	report := stats.Report{
		Total:       1000,
		Success:     990,
		Errors:      10,
		StatusCodes: map[int]int64{200: 990, 500: 10},
		ErrorGroups: map[string]int64{"timeout": 10},
		MeanMS:      25.0,
		P50MS:       20.0,
		P99MS:       100.0,
		MinMS:       5.0,
		MaxMS:       200.0,
		BytesRecv:   512 * 1024,
	}
	elapsed := 30 * time.Second

	var buf bytes.Buffer
	if err := Render(&buf, cfg, report, elapsed); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	out := buf.String()

	for _, fragment := range []string{
		"http://example.com",
		"1,000",    // commified total
		"512.0 KB", // formatted bytes (512*1024 bytes)
		"Load Test Results",
		"Latency",
		"Status Codes",
		"Errors",
	} {
		if !strings.Contains(out, fragment) {
			t.Errorf("text output missing %q", fragment)
		}
	}
}

func TestRenderText_NoLatencyWhenEmpty(t *testing.T) {
	cfg := &config.Config{
		URL: "http://example.com", Method: "GET",
		Profile: "constant", MaxRPS: 10, MinRPS: 1,
		Duration: time.Second, Concurrency: 1, Steps: 1,
		Output: "text",
	}
	var buf bytes.Buffer
	_ = Render(&buf, cfg, stats.Report{
		StatusCodes: map[int]int64{},
		ErrorGroups: map[string]int64{},
	}, time.Second)
	out := buf.String()
	if strings.Contains(out, "Latency") {
		t.Error("text output should not contain Latency section when Total=0")
	}
}
