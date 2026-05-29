package remoteinfo

import (
	"context"
	"strings"

	"golang.org/x/crypto/ssh"
)

// collectDisks собирает df-статистику + /proc/mounts + /proc/diskstats.
// Возвращает usage по корневой ФС (для UI «всего/занято на сервере»), список
// разделов и IO-счётчики по дисковым устройствам.
func (c *Collector) collectDisks(ctx context.Context, client *ssh.Client) (DisksInfo, error) {
	// printf '\n===<NAME>===\n' с ведущим \n — единый защитный стиль (см. host.go).
	const script = `printf '===DF===\n'
df -B1 -PT 2>/dev/null
printf '\n===DF_INODES===\n'
df -PTi 2>/dev/null
printf '\n===MOUNTS===\n'
cat /proc/mounts 2>/dev/null
printf '\n===DISKSTATS===\n'
cat /proc/diskstats 2>/dev/null
printf '\n===END===\n'
`
	out, err := runOutput(ctx, client, script)
	if err != nil && out == "" {
		return DisksInfo{}, err
	}
	blocks := splitByMarkers(out, []string{
		"===DF===", "===DF_INODES===", "===MOUNTS===", "===DISKSTATS===", "===END===",
	})

	partitions := parseDF(blocks["===DF==="], blocks["===DF_INODES==="], blocks["===MOUNTS==="])

	// usage — сводка по корню (приоритет mountpoint="/", иначе первая запись).
	usage := DiskUsageSummary{}
	for _, p := range partitions {
		if p.Mountpoint == "/" {
			usage = DiskUsageSummary{
				Path:        p.Mountpoint,
				Fstype:      p.Fstype,
				TotalBytes:  p.TotalBytes,
				UsedBytes:   p.UsedBytes,
				FreeBytes:   p.FreeBytes,
				UsedPercent: p.UsedPercent,
				InodesTotal: p.InodesTotal,
				InodesUsed:  p.InodesUsed,
				InodesFree:  p.InodesFree,
			}
			break
		}
	}
	if usage.Path == "" && len(partitions) > 0 {
		p := partitions[0]
		usage = DiskUsageSummary{
			Path: p.Mountpoint, Fstype: p.Fstype,
			TotalBytes: p.TotalBytes, UsedBytes: p.UsedBytes, FreeBytes: p.FreeBytes,
			UsedPercent: p.UsedPercent,
			InodesTotal: p.InodesTotal, InodesUsed: p.InodesUsed, InodesFree: p.InodesFree,
		}
	}

	io := parseDiskstats(blocks["===DISKSTATS==="])

	return DisksInfo{Usage: usage, Partitions: partitions, IOCounters: io}, nil
}

// parseDF объединяет вывод `df -B1 -PT` (размеры в байтах + fstype), `df -PTi`
// (inode-статистика) и /proc/mounts (опции монтирования) в единый список.
//
// Намеренно отфильтровываем псевдо-ФС (proc/sysfs/tmpfs/cgroup и т.п.) — они
// бесполезны для UI «сколько места осталось».
func parseDF(dfBytes, dfInodes, mountsText string) []DiskPartition {
	dfLines := strings.Split(dfBytes, "\n")
	if len(dfLines) <= 1 {
		return nil
	}

	// Опции монтирования из /proc/mounts: ключ — mountpoint.
	mounts := make(map[string]string, 32)
	for _, line := range strings.Split(mountsText, "\n") {
		f := strings.Fields(line)
		if len(f) >= 4 {
			mounts[f[1]] = f[3]
		}
	}

	// Inode-данные: ключ — mountpoint.
	type inodeRec struct{ total, used, free uint64 }
	inodes := make(map[string]inodeRec, 16)
	for _, line := range strings.Split(dfInodes, "\n")[1:] {
		f := strings.Fields(line)
		if len(f) < 7 {
			continue
		}
		mp := f[len(f)-1]
		inodes[mp] = inodeRec{
			total: atou64Safe(f[2]),
			used:  atou64Safe(f[3]),
			free:  atou64Safe(f[4]),
		}
	}

	partitions := make([]DiskPartition, 0, 8)
	for _, line := range dfLines[1:] {
		f := strings.Fields(line)
		// `df -PT` колонки: Filesystem, Type, 1B-blocks, Used, Available, Capacity, Mounted on
		if len(f) < 7 {
			continue
		}
		fstype := f[1]
		if isPseudoFS(fstype) {
			continue
		}
		mp := strings.Join(f[6:], " ") // mountpoint может содержать пробелы
		total := atou64Safe(f[2])
		used := atou64Safe(f[3])
		free := atou64Safe(f[4])
		usedPct := 0.0
		if total > 0 {
			usedPct = float64(used) / float64(total) * 100.0
		}
		ino := inodes[mp]
		partitions = append(partitions, DiskPartition{
			Device:      f[0],
			Mountpoint:  mp,
			Fstype:      fstype,
			Opts:        mounts[mp],
			TotalBytes:  total,
			UsedBytes:   used,
			FreeBytes:   free,
			UsedPercent: usedPct,
			InodesTotal: ino.total,
			InodesUsed:  ino.used,
			InodesFree:  ino.free,
		})
	}
	return partitions
}

// isPseudoFS — фильтр служебных ФС, которые бесполезны для дисковой панели.
func isPseudoFS(fs string) bool {
	switch fs {
	case "tmpfs", "devtmpfs", "proc", "sysfs", "cgroup", "cgroup2",
		"devpts", "mqueue", "pstore", "bpf", "tracefs", "debugfs",
		"securityfs", "configfs", "fusectl", "ramfs", "rpc_pipefs",
		"hugetlbfs", "autofs", "binfmt_misc", "nsfs", "overlay",
		"squashfs", "fuse.gvfsd-fuse", "fuse.portal":
		return true
	}
	return false
}

// parseDiskstats читает /proc/diskstats. Формат: major minor name reads
// reads_merged sectors_read read_ms writes ... Возвращает map по имени
// устройства (sda, nvme0n1, ...). Sector = 512 байт.
//
// Поля /proc/diskstats (на 2.6.27+ — 11 полей; 4.18+ — 18; берём первые 11).
func parseDiskstats(text string) map[string]DiskIOCounters {
	out := make(map[string]DiskIOCounters)
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 11 {
			continue
		}
		name := f[2]
		// Отфильтровываем разделы — оставляем родительские устройства
		// (sda, nvme0n1, vda). Для разделов это дублирование данных
		// и кратное увеличение JSON-ответа.
		if isPartition(name) {
			continue
		}
		out[name] = DiskIOCounters{
			ReadCount:  atou64Safe(f[3]),
			ReadBytes:  atou64Safe(f[5]) * 512,
			ReadTime:   atou64Safe(f[6]),
			WriteCount: atou64Safe(f[7]),
			WriteBytes: atou64Safe(f[9]) * 512,
			WriteTime:  atou64Safe(f[10]),
		}
		if len(f) > 12 {
			out[name] = withIoTime(out[name], atou64Safe(f[12]))
		}
	}
	return out
}

func withIoTime(c DiskIOCounters, ioTime uint64) DiskIOCounters {
	c.IoTime = ioTime
	return c
}

// isPartition — простая эвристика: оканчивается цифрой ИЛИ соответствует
// nvme/mmcblk-pattern. Достаточно для подавляющего большинства Linux-серверов.
func isPartition(name string) bool {
	if name == "" {
		return false
	}
	last := name[len(name)-1]
	if last < '0' || last > '9' {
		return false
	}
	// nvme0n1 — это устройство (n + цифра в середине, без 'p' в конце);
	// nvme0n1p1 — раздел; mmcblk0 — устройство, mmcblk0p1 — раздел.
	if strings.Contains(name, "nvme") || strings.Contains(name, "mmcblk") {
		return strings.Contains(name, "p")
	}
	return true
}
