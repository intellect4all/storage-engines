package testutil

import (
	"sync/atomic"

	"github.com/intellect4all/storage-engines/common"
)

type ResourceLimiter struct {
	maxDiskBytes   int64
	maxMemoryBytes int64
	diskUsed       atomic.Int64
	memUsed        atomic.Int64
}

func NewResourceLimiter(maxDisk, maxMemory int64) *ResourceLimiter {
	return &ResourceLimiter{
		maxDiskBytes:   maxDisk,
		maxMemoryBytes: maxMemory,
	}
}

func (r *ResourceLimiter) AllocDisk(n int64) error {
	newUsed := r.diskUsed.Add(n)
	if newUsed > r.maxDiskBytes {
		r.diskUsed.Add(-n)
		return common.ErrDiskFull
	}
	return nil
}

func (r *ResourceLimiter) FreeDisk(n int64) {
	r.diskUsed.Add(-n)
}

func (r *ResourceLimiter) DiskUsed() int64 {
	return r.diskUsed.Load()
}

func (r *ResourceLimiter) AllocMemory(n int64) error {
	newUsed := r.memUsed.Add(n)
	if newUsed > r.maxMemoryBytes {
		r.memUsed.Add(-n)
		return common.ErrDiskFull // reuse error
	}
	return nil
}

func (r *ResourceLimiter) FreeMemory(n int64) {
	r.memUsed.Add(-n)
}
