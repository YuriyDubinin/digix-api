package remoteinfo

import (
	"context"
	"strings"

	"golang.org/x/crypto/ssh"
)

// collectMemory читает /proc/meminfo. Значения в файле — в КБ; конвертим в байты.
func (c *Collector) collectMemory(ctx context.Context, client *ssh.Client) (MemoryInfo, error) {
	const cmd = `cat /proc/meminfo 2>/dev/null`
	out, err := runMustOutput(ctx, client, cmd)
	if err != nil {
		return MemoryInfo{}, err
	}
	kv := parseMeminfo(out)

	mem := MemoryInfo{
		Virtual: VirtualMemory{
			TotalBytes:     kv["MemTotal"],
			AvailableBytes: kv["MemAvailable"],
			FreeBytes:      kv["MemFree"],
			CachedBytes:    kv["Cached"],
			BuffersBytes:   kv["Buffers"],
			SharedBytes:    kv["Shmem"],
			SlabBytes:      kv["Slab"],
		},
		Swap: SwapMemory{
			TotalBytes: kv["SwapTotal"],
			FreeBytes:  kv["SwapFree"],
		},
	}

	// UsedBytes считаем как Total - Available (как делает `free`):
	// это «настоящая» занятая память, без cache/buffers, которые легко
	// освобождаются под нагрузку.
	if mem.Virtual.TotalBytes > 0 && mem.Virtual.AvailableBytes <= mem.Virtual.TotalBytes {
		mem.Virtual.UsedBytes = mem.Virtual.TotalBytes - mem.Virtual.AvailableBytes
		mem.Virtual.UsedPercent = float64(mem.Virtual.UsedBytes) / float64(mem.Virtual.TotalBytes) * 100.0
	}
	if mem.Swap.TotalBytes > 0 && mem.Swap.FreeBytes <= mem.Swap.TotalBytes {
		mem.Swap.UsedBytes = mem.Swap.TotalBytes - mem.Swap.FreeBytes
		mem.Swap.UsedPercent = float64(mem.Swap.UsedBytes) / float64(mem.Swap.TotalBytes) * 100.0
	}

	return mem, nil
}

// parseMeminfo разбирает /proc/meminfo. Значения возвращаются в байтах
// (исходно — КБ, "1234 kB"). Неизвестные/мусорные строки пропускаются.
func parseMeminfo(text string) map[string]uint64 {
	out := make(map[string]uint64)
	for _, line := range strings.Split(text, "\n") {
		i := strings.IndexByte(line, ':')
		if i <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:i])
		rest := strings.TrimSpace(line[i+1:])
		f := strings.Fields(rest)
		if len(f) == 0 {
			continue
		}
		n := atou64Safe(f[0])
		if len(f) > 1 && strings.EqualFold(f[1], "kB") {
			n *= 1024
		}
		out[key] = n
	}
	return out
}
