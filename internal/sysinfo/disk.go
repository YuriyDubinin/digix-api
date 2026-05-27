package sysinfo

import (
	"context"
	"fmt"
	"strings"

	"github.com/shirou/gopsutil/v4/disk"
)

// pseudoFilesystems — ФС, которые НЕ являются физическим диском: виртуальные,
// служебные и RAM-backed (tmpfs/ramfs). В подсчёт физического места не идут.
var pseudoFilesystems = map[string]struct{}{
	"tmpfs": {}, "devtmpfs": {}, "devfs": {}, "ramfs": {},
	"proc": {}, "sysfs": {}, "cgroup": {}, "cgroup2": {}, "devpts": {},
	"mqueue": {}, "securityfs": {}, "debugfs": {}, "tracefs": {}, "fusectl": {},
	"configfs": {}, "binfmt_misc": {}, "hugetlbfs": {}, "pstore": {}, "bpf": {},
	"autofs": {}, "nsfs": {}, "rpc_pipefs": {}, "selinuxfs": {}, "efivarfs": {},
	"fuse.lxcfs": {}, "overlayfs": {},
}

func isPseudoFS(fstype string) bool {
	_, ok := pseudoFilesystems[strings.ToLower(fstype)]
	return ok
}

func (c *Collector) collectDisks(ctx context.Context) (DisksInfo, []SectionError) {
	var sectionErrs []SectionError
	out := DisksInfo{}

	// 1) Сводка по физическому диску сервера — корневая ФС "/".
	// Работает и на голом железе, и в контейнере (overlay поверх диска хоста).
	if u, err := disk.UsageWithContext(ctx, "/"); err == nil && u != nil {
		out.Usage = DiskUsageSummary{
			Path:        u.Path,
			Fstype:      u.Fstype,
			TotalBytes:  u.Total,
			UsedBytes:   u.Used,
			FreeBytes:   u.Free,
			UsedPercent: u.UsedPercent,
			InodesTotal: u.InodesTotal,
			InodesUsed:  u.InodesUsed,
			InodesFree:  u.InodesFree,
		}
	} else if err != nil {
		sectionErrs = append(sectionErrs, SectionError{
			Section: "disks.usage",
			Message: err.Error(),
		})
	}

	// 2) Список реальных партиций. all=true + фильтр псевдо-ФС: иначе внутри
	// контейнера all=false отсекает overlay-корень и список выходит пустым
	// (тот самый "// 0 partitions").
	partitions, err := disk.PartitionsWithContext(ctx, true)
	if err != nil {
		sectionErrs = append(sectionErrs, SectionError{
			Section: "disks.partitions",
			Message: fmt.Sprintf("partitions: %v", err),
		})
		return out, sectionErrs
	}

	seenDevice := make(map[string]struct{})
	for _, p := range partitions {
		if isPseudoFS(p.Fstype) {
			continue // не физический диск (часто RAM)
		}
		if _, dup := seenDevice[p.Device]; dup {
			continue // один физический девайс — одна строка (bind/overlay-дубли)
		}
		usage, uerr := disk.UsageWithContext(ctx, p.Mountpoint)
		if uerr != nil || usage == nil || usage.Total == 0 {
			continue // нулевые/недоступные ФС пропускаем
		}
		seenDevice[p.Device] = struct{}{}
		out.Partitions = append(out.Partitions, DiskPartition{
			Device:      p.Device,
			Mountpoint:  p.Mountpoint,
			Fstype:      p.Fstype,
			Opts:        joinOpts(p.Opts),
			TotalBytes:  usage.Total,
			UsedBytes:   usage.Used,
			FreeBytes:   usage.Free,
			UsedPercent: usage.UsedPercent,
			InodesTotal: usage.InodesTotal,
			InodesUsed:  usage.InodesUsed,
			InodesFree:  usage.InodesFree,
		})
	}

	// 3) IO-counters — без изменений (1:1 с прежней логикой).
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
