package sysinfo

import (
	"context"
	"fmt"

	"github.com/shirou/gopsutil/v4/disk"
)

func (c *Collector) collectDisks(ctx context.Context) (DisksInfo, []SectionError) {
	var sectionErrs []SectionError
	out := DisksInfo{}

	partitions, err := disk.PartitionsWithContext(ctx, false) // all=false → только реальные ФС
	if err != nil {
		sectionErrs = append(sectionErrs, SectionError{
			Section: "disks.partitions",
			Message: fmt.Sprintf("partitions: %v", err),
		})
		return out, sectionErrs
	}

	for _, p := range partitions {
		row := DiskPartition{
			Device:     p.Device,
			Mountpoint: p.Mountpoint,
			Fstype:     p.Fstype,
			Opts:       joinOpts(p.Opts),
		}
		usage, err := disk.UsageWithContext(ctx, p.Mountpoint)
		if err == nil && usage != nil {
			row.TotalBytes = usage.Total
			row.UsedBytes = usage.Used
			row.FreeBytes = usage.Free
			row.UsedPercent = usage.UsedPercent
			row.InodesTotal = usage.InodesTotal
			row.InodesUsed = usage.InodesUsed
			row.InodesFree = usage.InodesFree
		}
		// usage может упасть на спецФС (squashfs, ramfs и т.п.) — не валим всю секцию.
		out.Partitions = append(out.Partitions, row)
	}

	// IO-counters требуют /proc/diskstats или эквивалента — могут быть недоступны
	// в minimal-контейнерах. Best-effort.
	if ioc, err := disk.IOCountersWithContext(ctx); err == nil {
		out.IOCounters = make(map[string]DiskIOCounters, len(ioc))
		for name, v := range ioc {
			out.IOCounters[name] = DiskIOCounters{
				ReadCount:  v.ReadCount,
				WriteCount: v.WriteCount,
				ReadBytes:  v.ReadBytes,
				WriteBytes: v.WriteBytes,
				ReadTime:   v.ReadTime,
				WriteTime:  v.WriteTime,
				IoTime:     v.IoTime,
			}
		}
	} else {
		sectionErrs = append(sectionErrs, SectionError{
			Section: "disks.io_counters",
			Message: err.Error(),
		})
	}

	return out, sectionErrs
}

func joinOpts(opts []string) string {
	if len(opts) == 0 {
		return ""
	}
	s := ""
	for i, o := range opts {
		if i > 0 {
			s += ","
		}
		s += o
	}
	return s
}
