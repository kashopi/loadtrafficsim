package stats

import (
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Result holds the outcome of a single HTTP request.
type Result struct {
	Duration   time.Duration
	StatusCode int
	Bytes      int64
	Err        error
}

// LiveStats is a cheap snapshot for the real-time display ticker.
type LiveStats struct {
	Total     int64
	Success   int64
	Errors    int64
	BytesRecv int64
	CurrentRPS float64
	WindowMean float64 // mean latency in ms for the current 1s window
}

// Report is the full end-of-test summary (percentiles computed once).
type Report struct {
	Total       int64
	Success     int64
	Errors      int64
	BytesRecv   int64
	MeanMS      float64
	MinMS       float64
	MaxMS       float64
	P50MS       float64
	P90MS       float64
	P95MS       float64
	P99MS       float64
	StatusCodes map[int]int64
	ErrorGroups map[string]int64
}

// Collector gathers request results concurrently.
type Collector struct {
	mu          sync.Mutex
	latencies   []float64     // all latency samples, ms
	latSum      float64       // running sum for cheap mean
	statusCodes map[int]int64
	errorGroups map[string]int64

	total     atomic.Int64
	success   atomic.Int64
	errCount  atomic.Int64
	bytesRecv atomic.Int64

	// 1-second sliding window for live RPS and mean latency
	windowStart  time.Time
	windowCount  int64
	windowLatSum float64
	currentRPS   float64
	windowMean   float64
}

// NewCollector returns an initialised Collector.
func NewCollector() *Collector {
	return &Collector{
		statusCodes: make(map[int]int64),
		errorGroups: make(map[string]int64),
		windowStart: time.Now(),
	}
}

// Record registers a single request result. Safe for concurrent use.
func (c *Collector) Record(r Result) {
	latMS := float64(r.Duration.Microseconds()) / 1000.0

	c.total.Add(1)
	c.bytesRecv.Add(r.Bytes)

	c.mu.Lock()
	c.latencies = append(c.latencies, latMS)
	c.latSum += latMS

	if r.Err != nil {
		c.errCount.Add(1)
		c.errorGroups[classifyError(r.Err)]++
	} else {
		c.statusCodes[r.StatusCode]++
		if r.StatusCode >= 200 && r.StatusCode < 300 {
			c.success.Add(1)
		}
	}

	c.windowCount++
	c.windowLatSum += latMS
	elapsed := time.Since(c.windowStart)
	if elapsed >= time.Second {
		c.currentRPS = float64(c.windowCount) / elapsed.Seconds()
		c.windowMean = c.windowLatSum / float64(c.windowCount)
		c.windowStart = time.Now()
		c.windowCount = 0
		c.windowLatSum = 0
	}
	c.mu.Unlock()
}

// Live returns a cheap snapshot for the periodic display — no sorting.
func (c *Collector) Live() LiveStats {
	c.mu.Lock()
	rps := c.currentRPS
	wm := c.windowMean
	c.mu.Unlock()
	return LiveStats{
		Total:      c.total.Load(),
		Success:    c.success.Load(),
		Errors:     c.errCount.Load(),
		BytesRecv:  c.bytesRecv.Load(),
		CurrentRPS: rps,
		WindowMean: wm,
	}
}

// Finalize produces the full end-of-test report. Call once after all results are in.
func (c *Collector) Finalize() Report {
	c.mu.Lock()
	defer c.mu.Unlock()

	r := Report{
		Total:       c.total.Load(),
		Success:     c.success.Load(),
		Errors:      c.errCount.Load(),
		BytesRecv:   c.bytesRecv.Load(),
		StatusCodes: copyIntMap(c.statusCodes),
		ErrorGroups: copyStrMap(c.errorGroups),
	}

	if n := len(c.latencies); n > 0 {
		sorted := make([]float64, n)
		copy(sorted, c.latencies)
		sort.Float64s(sorted)

		r.MinMS = sorted[0]
		r.MaxMS = sorted[n-1]
		r.MeanMS = c.latSum / float64(n)
		r.P50MS = pct(sorted, 50)
		r.P90MS = pct(sorted, 90)
		r.P95MS = pct(sorted, 95)
		r.P99MS = pct(sorted, 99)
	}

	return r
}

func pct(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p / 100.0)
	return sorted[idx]
}

func copyIntMap(m map[int]int64) map[int]int64 {
	out := make(map[int]int64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func copyStrMap(m map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// Ordered from most-specific to least-specific so longer keywords win.
var errorKeywords = []string{
	"connection refused",
	"connection reset",
	"no such host",
	"tls handshake",
	"timeout",
	"eof",
	"broken pipe",
}

func classifyError(err error) string {
	lower := strings.ToLower(err.Error())
	for _, kw := range errorKeywords {
		if strings.Contains(lower, kw) {
			return kw
		}
	}
	return err.Error()
}
