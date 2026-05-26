package sysinfo

import (
	"context"
	"fmt"

	"github.com/shirou/gopsutil/v4/mem"
)

func (c *Collector) collectMemory(ctx context.Context) (MemoryInfo, error) {
	out := MemoryInfo{}

	vm, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return out, fmt.Errorf("virtual memory: %w", err)
	}
	out.Virtual = VirtualMemory{
		TotalBytes:     vm.Total,
		AvailableBytes: vm.Available,
		UsedBytes:      vm.Used,
		FreeBytes:      vm.Free,
		CachedBytes:    vm.Cached,
		BuffersBytes:   vm.Buffers,
		SharedBytes:    vm.Shared,
		SlabBytes:      vm.Slab,
		UsedPercent:    vm.UsedPercent,
	}

	// Swap может отсутствовать на некоторых хостах / контейнерах — это не ошибка.
	if sw, err := mem.SwapMemoryWithContext(ctx); err == nil && sw != nil {
		out.Swap = SwapMemory{
			TotalBytes:  sw.Total,
			UsedBytes:   sw.Used,
			FreeBytes:   sw.Free,
			UsedPercent: sw.UsedPercent,
		}
	}

	return out, nil
}
