package stats

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func success(code int, latency time.Duration, bytes int64) Result {
	return Result{StatusCode: code, Duration: latency, Bytes: bytes}
}

func failure(err error, latency time.Duration) Result {
	return Result{Err: err, Duration: latency}
}

// ── Record / Finalize ─────────────────────────────────────────────────────────

func TestCollector_Empty(t *testing.T) {
	c := NewCollector()
	r := c.Finalize()
	if r.Total != 0 || r.Success != 0 || r.Errors != 0 {
		t.Errorf("empty collector: got total=%d success=%d errors=%d, want all 0",
			r.Total, r.Success, r.Errors)
	}
	if r.MinMS != 0 || r.MaxMS != 0 || r.MeanMS != 0 {
		t.Errorf("empty collector: non-zero latency stats")
	}
}

func TestCollector_2xxIncrementsSuccess(t *testing.T) {
	c := NewCollector()
	for _, code := range []int{200, 201, 204} {
		c.Record(success(code, time.Millisecond, 0))
	}
	r := c.Finalize()
	if r.Total != 3 {
		t.Errorf("total = %d, want 3", r.Total)
	}
	if r.Success != 3 {
		t.Errorf("success = %d, want 3", r.Success)
	}
	if r.Errors != 0 {
		t.Errorf("errors = %d, want 0", r.Errors)
	}
}

func TestCollector_Non2xxNotSuccess(t *testing.T) {
	c := NewCollector()
	for _, code := range []int{400, 404, 500, 503} {
		c.Record(success(code, time.Millisecond, 0))
	}
	r := c.Finalize()
	if r.Total != 4 {
		t.Errorf("total = %d, want 4", r.Total)
	}
	if r.Success != 0 {
		t.Errorf("success = %d, want 0", r.Success)
	}
	if r.Errors != 0 {
		t.Errorf("errors = %d, want 0 (non-2xx is not an Err)", r.Errors)
	}
}

func TestCollector_ErrFieldIncrementsErrors(t *testing.T) {
	c := NewCollector()
	c.Record(failure(errors.New("connection refused"), time.Millisecond))
	c.Record(failure(errors.New("timeout"), time.Millisecond))
	r := c.Finalize()
	if r.Total != 2 {
		t.Errorf("total = %d, want 2", r.Total)
	}
	if r.Success != 0 {
		t.Errorf("success = %d, want 0", r.Success)
	}
	if r.Errors != 2 {
		t.Errorf("errors = %d, want 2", r.Errors)
	}
}

func TestCollector_BytesRecvAccumulates(t *testing.T) {
	c := NewCollector()
	c.Record(success(200, time.Millisecond, 100))
	c.Record(success(200, time.Millisecond, 250))
	c.Record(failure(errors.New("err"), time.Millisecond)) // errors add 0 bytes
	r := c.Finalize()
	if r.BytesRecv != 350 {
		t.Errorf("BytesRecv = %d, want 350", r.BytesRecv)
	}
}

func TestCollector_StatusCodesTracked(t *testing.T) {
	c := NewCollector()
	c.Record(success(200, time.Millisecond, 0))
	c.Record(success(200, time.Millisecond, 0))
	c.Record(success(404, time.Millisecond, 0))
	c.Record(success(500, time.Millisecond, 0))
	r := c.Finalize()
	if r.StatusCodes[200] != 2 {
		t.Errorf("200 count = %d, want 2", r.StatusCodes[200])
	}
	if r.StatusCodes[404] != 1 {
		t.Errorf("404 count = %d, want 1", r.StatusCodes[404])
	}
	if r.StatusCodes[500] != 1 {
		t.Errorf("500 count = %d, want 1", r.StatusCodes[500])
	}
}

func TestCollector_ErrorGroupsTracked(t *testing.T) {
	c := NewCollector()
	c.Record(failure(errors.New("connection refused: addr"), time.Millisecond))
	c.Record(failure(errors.New("connection refused: addr"), time.Millisecond))
	c.Record(failure(errors.New("context deadline exceeded (timeout)"), time.Millisecond))
	r := c.Finalize()
	if r.ErrorGroups["connection refused"] != 2 {
		t.Errorf("'connection refused' count = %d, want 2", r.ErrorGroups["connection refused"])
	}
	if r.ErrorGroups["timeout"] != 1 {
		t.Errorf("'timeout' count = %d, want 1", r.ErrorGroups["timeout"])
	}
}

// ── Percentiles ───────────────────────────────────────────────────────────────

// TestCollector_Percentiles uses 100 requests with latencies 1ms…100ms.
// With the formula idx = int((n-1)·p/100):
//   P50 → idx=49 → sorted[49] = 50ms
//   P90 → idx=89 → sorted[89] = 90ms
//   P99 → idx=98 → sorted[98] = 99ms
func TestCollector_Percentiles(t *testing.T) {
	c := NewCollector()
	for i := 1; i <= 100; i++ {
		c.Record(success(200, time.Duration(i)*time.Millisecond, 0))
	}
	r := c.Finalize()

	if r.MinMS != 1.0 {
		t.Errorf("Min = %.2fms, want 1.00ms", r.MinMS)
	}
	if r.MaxMS != 100.0 {
		t.Errorf("Max = %.2fms, want 100.00ms", r.MaxMS)
	}
	// Mean = (1+2+…+100)/100 = 5050/100 = 50.5
	if r.MeanMS < 50.4 || r.MeanMS > 50.6 {
		t.Errorf("Mean = %.2fms, want ~50.50ms", r.MeanMS)
	}
	if r.P50MS != 50.0 {
		t.Errorf("P50 = %.2fms, want 50.00ms", r.P50MS)
	}
	if r.P90MS != 90.0 {
		t.Errorf("P90 = %.2fms, want 90.00ms", r.P90MS)
	}
	if r.P99MS != 99.0 {
		t.Errorf("P99 = %.2fms, want 99.00ms", r.P99MS)
	}
}

func TestCollector_SingleSample(t *testing.T) {
	c := NewCollector()
	c.Record(success(200, 42*time.Millisecond, 0))
	r := c.Finalize()
	if r.MinMS != 42.0 || r.MaxMS != 42.0 || r.MeanMS != 42.0 {
		t.Errorf("single sample: min=%.2f max=%.2f mean=%.2f, all want 42.00",
			r.MinMS, r.MaxMS, r.MeanMS)
	}
}

func TestCollector_FinalizeReturnsCopies(t *testing.T) {
	c := NewCollector()
	c.Record(success(200, time.Millisecond, 0))
	r1 := c.Finalize()
	c.Record(success(404, time.Millisecond, 0))
	r2 := c.Finalize()

	// r1 must not be mutated by subsequent Records.
	if _, ok := r1.StatusCodes[404]; ok {
		t.Error("r1.StatusCodes was mutated after a subsequent Record")
	}
	if r2.StatusCodes[404] != 1 {
		t.Errorf("r2 should see 404; got %d", r2.StatusCodes[404])
	}
}

// ── classifyError ─────────────────────────────────────────────────────────────

func TestClassifyError_KnownKeywords(t *testing.T) {
	cases := []struct {
		msg  string
		want string
	}{
		{"dial tcp: connection refused", "connection refused"},
		{"read tcp: connection reset by peer", "connection reset"},
		{"dial tcp: no such host", "no such host"},
		{"context deadline exceeded (Client.Timeout exceeded)", "timeout"},
		{"tls handshake timeout", "tls handshake"},
		{"unexpected EOF", "eof"},
		{"write tcp: broken pipe", "broken pipe"},
	}
	for _, tc := range cases {
		got := classifyError(errors.New(tc.msg))
		if got != tc.want {
			t.Errorf("classifyError(%q) = %q, want %q", tc.msg, got, tc.want)
		}
	}
}

func TestClassifyError_Unknown(t *testing.T) {
	msg := "some completely unknown error XYZ"
	got := classifyError(errors.New(msg))
	if got != msg {
		t.Errorf("classifyError unknown: got %q, want original message", got)
	}
}

// ── Concurrency ───────────────────────────────────────────────────────────────

func TestCollector_ConcurrentRecord(t *testing.T) {
	// Run with `go test -race` to detect data races.
	c := NewCollector()
	const goroutines = 50
	const perGoroutine = 100

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				if i%10 == 0 {
					c.Record(failure(errors.New("connection refused"), time.Millisecond))
				} else {
					c.Record(success(200, time.Duration(i)*time.Millisecond, int64(i*10)))
				}
			}
		}(g)
	}
	wg.Wait()

	r := c.Finalize()
	total := int64(goroutines * perGoroutine)
	if r.Total != total {
		t.Errorf("total = %d, want %d", r.Total, total)
	}
	errors_ := int64(goroutines * (perGoroutine / 10))
	success_ := total - errors_
	if r.Success != success_ {
		t.Errorf("success = %d, want %d", r.Success, success_)
	}
	if r.Errors != errors_ {
		t.Errorf("errors = %d, want %d", r.Errors, errors_)
	}
}

// ── Live ──────────────────────────────────────────────────────────────────────

func TestCollector_LiveCounters(t *testing.T) {
	c := NewCollector()
	c.Record(success(200, 10*time.Millisecond, 100))
	c.Record(failure(errors.New("eof"), 5*time.Millisecond))

	live := c.Live()
	if live.Total != 2 {
		t.Errorf("Live.Total = %d, want 2", live.Total)
	}
	if live.Success != 1 {
		t.Errorf("Live.Success = %d, want 1", live.Success)
	}
	if live.Errors != 1 {
		t.Errorf("Live.Errors = %d, want 1", live.Errors)
	}
	if live.BytesRecv != 100 {
		t.Errorf("Live.BytesRecv = %d, want 100", live.BytesRecv)
	}
}
