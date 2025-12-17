package benchmark

import (
	"sort"
	"sync"
	"time"
)

type LatencyHistogram struct {
	mu      sync.RWMutex
	samples []time.Duration
}

type LatencyStats struct {
	Min  time.Duration
	Max  time.Duration
	Mean time.Duration
	P50  time.Duration
	P95  time.Duration
	P99  time.Duration
	P999 time.Duration
}

func NewLatencyHistogram() *LatencyHistogram {
	return &LatencyHistogram{
		samples: make([]time.Duration, 0, 1000000),
	}
}

func (h *LatencyHistogram) Record(d time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.samples = append(h.samples, d)
}

func (h *LatencyHistogram) Stats() LatencyStats {
	var copied []time.Duration
	func() {
		h.mu.RLock()
		defer h.mu.RUnlock()
		copied = make([]time.Duration, len(h.samples))
		copy(copied, h.samples)
	}()

	if len(copied) == 0 {
		return LatencyStats{}
	}

	sort.Slice(copied, func(i, j int) bool {
		return copied[i] < copied[j]
	})

	sum := time.Duration(0)
	for _, d := range copied {
		sum += d
	}

	return LatencyStats{
		Min:  copied[0],
		Max:  copied[len(copied)-1],
		Mean: sum / time.Duration(len(copied)),
		P50:  copied[len(copied)*50/100],
		P95:  copied[len(copied)*95/100],
		P99:  copied[len(copied)*99/100],
		P999: copied[len(copied)*999/1000],
	}
}
