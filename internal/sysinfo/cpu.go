package sysinfo

import (
	"context"
	"fmt"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/load"
)

// cpuSampleInterval — длительность семплирования %CPU. Меньше — больше шума,
// больше — больше задержка ответа эндпоинта. 300мс — разумный компромисс.
const cpuSampleInterval = 300 * time.Millisecond

func (c *Collector) collectCPU(ctx context.Context) (CPUInfo, error) {
	infos, err := cpu.InfoWithContext(ctx)
	if err != nil {
		return CPUInfo{}, fmt.Errorf("cpu info: %w", err)
	}

	out := CPUInfo{}
	if len(infos) > 0 {
		ci := infos[0]
		out.ModelName = ci.ModelName
		out.Vendor = ci.VendorID
		out.Family = ci.Family
		out.Model = ci.Model
		out.Stepping = ci.Stepping
		out.MHz = ci.Mhz
		out.CacheSizeKB = ci.CacheSize
		out.Flags = ci.Flags
	}

	if n, err := cpu.CountsWithContext(ctx, false); err == nil {
		out.PhysicalCores = n
	}
	if n, err := cpu.CountsWithContext(ctx, true); err == nil {
		out.LogicalCores = n
	}

	// Семплируем CPU% с задержкой — иначе значения будут нулевыми при первом вызове.
	if total, err := cpu.PercentWithContext(ctx, cpuSampleInterval, false); err == nil && len(total) > 0 {
		out.UsagePercent = total[0]
	}
	// Per-core — отдельный вызов с нулевым интервалом, чтобы использовать
	// внутренний snapshot, оставшийся от предыдущего сэмпла.
	if perCore, err := cpu.PercentWithContext(ctx, 0, true); err == nil {
		out.PerCoreUsagePercent = perCore
	}

	if l, err := load.AvgWithContext(ctx); err == nil && l != nil {
		out.LoadAvg1 = l.Load1
		out.LoadAvg5 = l.Load5
		out.LoadAvg15 = l.Load15
	}

	return out, nil
}
