package loadtest

import (
	"sort"
	"sync"
	"time"
)

type OpType string

const (
	OpValidate  OpType = "validate"
	OpActivate  OpType = "activate"
	OpUsage     OpType = "usage"
)

type ErrorClass string

const (
	ErrNone          ErrorClass = ""
	ErrRateLimited   ErrorClass = "rate_limited"
	ErrInvalid       ErrorClass = "invalid"
	ErrNotFound      ErrorClass = "not_found"
	ErrExpired       ErrorClass = "expired"
	ErrRevoked       ErrorClass = "revoked"
	ErrTimeout       ErrorClass = "timeout"
	ErrInternal      ErrorClass = "internal"
)

type Metrics struct {
	mu        sync.Mutex
	latencies []time.Duration
	total     int64
	success   int64
	fail      int64
	byError   map[ErrorClass]int64
	byOp      map[OpType]int64
}

func NewMetrics() *Metrics {
	return &Metrics{
		byError: make(map[ErrorClass]int64),
		byOp:    make(map[OpType]int64),
	}
}

func (m *Metrics) Record(op OpType, ok bool, errClass ErrorClass, latency time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.total++
	if ok {
		m.success++
	} else {
		m.fail++
	}
	m.byError[errClass]++
	m.byOp[op]++
	m.latencies = append(m.latencies, latency)
}

type Summary struct {
	Requests   int64
	Success    int64
	Failures   int64
	Avg        time.Duration
	P95        time.Duration
	P99        time.Duration
	Throughput float64
	Errors     map[ErrorClass]int64
	ByOp       map[OpType]int64
}

func (m *Metrics) Summarize(elapsed time.Duration) Summary {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := Summary{
		Requests: m.total,
		Success:  m.success,
		Failures: m.fail,
		Errors:   make(map[ErrorClass]int64, len(m.byError)),
		ByOp:     make(map[OpType]int64, len(m.byOp)),
	}
	for k, v := range m.byError {
		out.Errors[k] = v
	}
	for k, v := range m.byOp {
		out.ByOp[k] = v
	}
	if len(m.latencies) > 0 {
		cp := make([]time.Duration, len(m.latencies))
		copy(cp, m.latencies)
		sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
		var sum time.Duration
		for _, d := range cp {
			sum += d
		}
		out.Avg = sum / time.Duration(len(cp))
		out.P95 = percentile(cp, 95)
		out.P99 = percentile(cp, 99)
	}
	if elapsed > 0 {
		out.Throughput = float64(out.Requests) / elapsed.Seconds()
	}
	return out
}

func percentile(sorted []time.Duration, p int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[len(sorted)-1]
	}
	// nearest-rank
	rank := (p*len(sorted) + 99) / 100
	if rank <= 0 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}

